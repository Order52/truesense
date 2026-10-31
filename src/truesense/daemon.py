# SPDX-License-Identifier: MIT
"""truesense daemon (root): smart fan curve, thermal governor, CPU power limits, automatic power profile, and the control socket.

Safety: fans go back to firmware Auto and clocks are uncapped on ANY exit (SIGTERM/SIGINT/exception/stop); if the loop breaks while hot the fans go to 100 %;
a missing/failing device never kills the daemon (it keeps waiting and retrying). Measured fan response (manual %, both fans):
20 % ~1700 rpm, 30 2160, 40 2850, 50 3460, 60 4070, 70 4600, 80 5100, 100 6050. Firmware Auto is the only way to reach 0 rpm, so when calm control goes back to it.
"""
import glob, os, pwd, signal, socket, subprocess, threading, time
from . import backend as hw, config, cpu, curves, gamerules, gpuundervolt, powermgr, ipc, looks, smartprofiles, undervolt, osd, state as statemod, touchpad

RUN = os.environ.get("TRUESENSE_RUN", "/run/truesense")
FAN_OFF_START_CPU, FAN_OFF_START_GPU = 60, 55        # fans may only be turned off below these temperatures ...
FAN_OFF_MAX_CPU, FAN_OFF_MAX_GPU = 70, 65              # ... and the watchdog gives them back to the firmware at these
GPU_UV_PENDING = os.environ.get("TRUESENSE_GPU_UV_PENDING", "/var/lib/truesense/gpu-uv-pending")
UV_PENDING = os.environ.get("TRUESENSE_UV_PENDING", "/var/lib/truesense/uv-pending")      # exists while an undervolt applied at boot has not yet proven stable
RAPL = "/sys/class/powercap/intel-rapl:0/"


def rd(path, default=None):
    try:
        with open(path) as f: return f.read().strip()
    except OSError: return default

def wr(path, value):
    try:
        with open(path, "w") as f: f.write(str(value))
        return True
    except OSError: return False

def num(path):
    try: return float(rd(path))
    except (TypeError, ValueError): return None

def ema(prev, v, a): return v if prev is None else prev + a * (v - prev)

def sd_notify(msg):
    path = os.environ.get("NOTIFY_SOCKET")
    if not path: return
    try:
        s = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM); s.connect("\0" + path[1:] if path[0] == "@" else path); s.send(msg.encode()); s.close()
    except OSError: pass


TMP_STATE = "/tmp/truesense-state"                  # world-readable copy for status bars whose sandbox cannot see /run


class Brain:
    def __init__(self, cfg, st):
        self.c, self.st = cfg, st; self.f, self.p = cfg["fan"], cfg["cpu"]
        try: self.pw = pwd.getpwnam(cfg["user"])
        except KeyError: self.pw = None
        self.home = self.pw.pw_dir if self.pw else "/root"
        self.coretemp = None
        self.prev_total = self.prev_idle = None
        self.cpu_load = self.gpu_util = self.gpu_power = 0.0
        self.gpu_temp = None; self.gpu_seen = 0.0
        self.tcpu = self.tgpu = self.tchassis = None; self.rpm = (0, 0)
        self.hist_c, self.hist_g = [], []
        self.tick = self.calm = self.busy_hold = self.last_try = 0
        self.lvl = [0, 0]; self.changed = [0, 0]; self.fan_off = False; self.heavy_for = self.idle_for = 0; self.pl_orig = None; self.pl_touched = False; self._smart = None; self.uv_since = None; self.gpu_uv_since = None; self.hold_ctx = None; self.reason = ""; self.backoff_until = 0; self.mode_changed = -999; self.follower = None
        self.written = None                      # (cpu, gpu) last written, (0, 0) = firmware Auto
        self.activity = "idle"; self.last_target = None; self.last_ac = None; self.cur_mode = None
        self.cap = self.p["cap_max_khz"]; self.cap_changed = 0
        self.cpus = sorted(glob.glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_max_freq"))
        self.hw_ok = True; self._last_err = 0.0; self.live = {}; self.lock = threading.RLock(); self.last_clock = None
        self.find_hwmon()
        self.gpu_started = False; self.interval = 1.0                    # the GPU sampler (nvidia-smi) only starts when a smart feature needs it

    # ---- helpers -------------------------------------------------------------------------------------------------------------------
    def log(self, msg, every=60):
        now = time.time()
        if now - self._last_err >= every: self._last_err = now; print(msg, flush=True)

    def say(self, text, icon="power"):
        if self.c["profile"]["osd"]: osd.show(text, icon, self.c, user=self.c["user"])

    def legacy(self, name):
        return self.c["compat"]["predator_widgets"] and os.path.exists(f"{self.home}/.cache/{name}")

    # ---- sensors -------------------------------------------------------------------------------------------------------------------
    def find_hwmon(self):
        for base in glob.glob("/sys/class/hwmon/hwmon*"):
            if rd(base + "/name") == "coretemp": self.coretemp = base

    def gpu_stream(self):
        cmd = ["nvidia-smi", "--query-gpu=temperature.gpu,utilization.gpu,power.draw", "--format=csv,noheader,nounits", "-l", "2"]
        while True:
            try:
                p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
                for line in p.stdout:
                    try:
                        t, u, w = [x.strip() for x in line.split(",")]
                        self.gpu_temp = float(t); self.gpu_util = ema(self.gpu_util, float(u), 0.3); self.gpu_power = ema(self.gpu_power, float(w), 0.3); self.gpu_seen = time.time()
                    except ValueError: pass
                p.wait()
            except Exception: pass
            time.sleep(5)

    def read_cpu_load(self):
        vals = [int(x) for x in rd("/proc/stat", "").split("\n", 1)[0].split()[1:9]]
        total, idle = sum(vals), vals[3] + vals[4]
        if self.prev_total is not None and total > self.prev_total:
            self.cpu_load = ema(self.cpu_load, 100 * (1 - (idle - self.prev_idle) / (total - self.prev_total)), 0.25)
        self.prev_total, self.prev_idle = total, idle

    # ---- fan -----------------------------------------------------------------------------------------------------------------------
    def write_fan(self, c, g):
        """Write only on change; (0, 0) = firmware Auto. A single write holds (verified), so no periodic rewrite."""
        if (c, g) == self.written: return
        try:
            hw.fan_auto() if (c, g) == (0, 0) else hw.fan_manual(max(20, c), max(20, g))
            self.written = (c, g)
        except hw.HwError as e:
            self.log(f"fan write failed: {e}", 10); self.written = None

    def verify_fan(self):
        """Every 15 s: if the firmware/driver reset the fans behind our back (plug/unplug, resume), re-assert."""
        if self.written is None or not self.hw_ok: return
        try: manual = hw.fan_state()["mode"] != "auto"
        except hw.HwError: return
        if manual != (self.written != (0, 0)): self.written = None

    @staticmethod
    def slope(hist, v):
        hist.append(v)
        if len(hist) > 5: hist.pop(0)
        return (hist[-1] - hist[0]) / 4 if len(hist) == 5 else 0

    def classify(self):
        m = 5 if getattr(self, "_hot", False) else 0                                  # once hot, stay hot until 5 degrees below the line (no flapping at the threshold)
        self._hot = self.tcpu >= self.f["hot_cpu"] - m or self.tgpu >= self.f["hot_gpu"] - m
        if self._hot: return "hot"
        if self.gpu_power >= 35 or self.gpu_util >= 40: return "gaming"
        if self.cpu_load >= 55: return "heavy"
        if self.cpu_load < 8 and self.gpu_util < 10: return "idle"
        return "browsing"

    def floor_pct(self):
        f = 60 if (self.activity == "gaming" and self.gpu_power >= 80) else 50 if self.activity == "gaming" else 40 if self.activity == "heavy" else 0
        if f > 0 and self.tchassis: f = max(f, 60 if self.tchassis >= 68 else 50 if self.tchassis >= 63 else 0)    # board/VRM heat-soak hint
        return f

    def load_curve(self):
        data = self.st.get("fan_curve_data")
        try: return curves.validate(data) if data else curves.load(self.st.get("fan_curve", "truesense") if self.st.get("fan_curve", "truesense") != "native" else "truesense")
        except hw.HwError as e:
            self.log(f"fan curve invalid, using the built-in: {e}", 30); return curves.load("truesense")

    def smart_fan(self):
        """Curve-driven fans: duty follows the active curve (see curves.py), rising at once and falling slowly, with the activity floor and heat protection."""
        f = self.f
        if self.follower is None: self.follower = curves.Follower(self.load_curve())
        fo = self.follower
        calm_now = self.activity in ("idle", "browsing") and self.tcpu < f["calm_cpu"] and self.tgpu < f["calm_gpu"]
        self.calm = self.calm + 1 if calm_now else 0
        if self.calm >= f["calm_hold"] or (max(fo.duty.values()) < 20 and calm_now):
            fo.duty = {"cpu": 0.0, "gpu": 0.0}; self.write_fan(0, 0); return
        if self.activity == "hot":
            fo.duty = {"cpu": 100.0, "gpu": 100.0}; self.write_fan(100, 100); return
        shift = 0.0 if self.last_ac else fo.c["battery_shift"]
        fo.step("cpu", self.tcpu, self.slope(self.hist_c, self.tcpu) >= 1, shift=shift); fo.step("gpu", self.tgpu, self.slope(self.hist_g, self.tgpu) >= 1, shift=shift)
        self.write_fan(*fo.output(self.floor_pct()))

    # ---- CPU power limit + thermal governor -------------------------------------------------------------------------------------------
    def enforce_power_limit(self, force_now=False):
        u = self.st.get("pl_user")
        if self.pl_orig is None: self.pl_orig = {n: num(RAPL + n) for n in ("constraint_0_power_limit_uw", "constraint_1_power_limit_uw")}          # what the firmware set, kept to hand back
        if not u and not self.smart_active() and not force_now:
            if self.pl_touched:
                for n, v in self.pl_orig.items():
                    if v is not None and num(RAPL + n) != v: wr(RAPL + n, int(v))
                self.pl_touched = False
            return
        self.pl_touched = True
        if u: want = {"constraint_0_power_limit_uw": int(u["pl1"] * 1_000_000), "constraint_1_power_limit_uw": int(u["pl2"] * 1_000_000)}      # your own limits
        else:
            pl = int((self.p["pl_gaming_w"] if self.activity == "gaming" else self.p["pl_normal_w"]) * 1_000_000); want = {"constraint_0_power_limit_uw": pl, "constraint_1_power_limit_uw": pl}
        for n, v in want.items():
            if num(RAPL + n) != v: wr(RAPL + n, v)

    def thermal_governor(self):
        if not self.p["gov_enabled"] or not self.smart_active():
            if self.cap < self.p["cap_max_khz"]: self.cap = self.p["cap_max_khz"]; self.apply_cap()                  # not managing the CPU: give the full clock back
            return
        p = self.p; new = self.cap
        if self.tcpu >= p["gov_hi"] and self.tick - self.cap_changed >= 2: new = max(p["cap_min_khz"], self.cap - p["cap_down_khz"])
        elif self.tcpu <= p["gov_lo"] and self.cap < p["cap_max_khz"] and self.tick - self.cap_changed >= 3: new = min(p["cap_max_khz"], self.cap + p["cap_up_khz"])
        if new != self.cap: self.cap, self.cap_changed = new, self.tick; self.apply_cap()
        elif self.cap < p["cap_max_khz"] and self.tick % 30 == 0: self.apply_cap()

    def apply_cap(self):
        for p in self.cpus:
            if rd(p) != str(self.cap): wr(p, self.cap)

    # ---- power profile -------------------------------------------------------------------------------------------------------------------
    RANK = {"eco": 0, "quiet": 1, "balanced": 2, "performance": 3, "turbo": 4}

    def auto_kind(self):
        k = self.st.get("auto_mode")
        return k if k in ("off", "native", "smart") else ("smart" if self.st.get("auto_profile") else "off")

    def battery_pct(self):
        for p in glob.glob("/sys/class/power_supply/BAT*/capacity"):
            v = num(p)
            if v is not None: return v
        return 100

    def ensure_gpu_stream(self):
        if not self.gpu_started: self.gpu_started = True; threading.Thread(target=self.gpu_stream, daemon=True).start()

    def smart_active(self):
        """TrueSense's own power management (CPU clock cap by temperature, automatic power limits) only runs when you chose a smart feature. Otherwise the firmware is left
        alone, like PredatorSense on Windows."""
        return bool(self.st.get("smart_fan")) or self.auto_kind() == "smart" or bool(self.st.get("pl_user"))

    def bucket(self): return "busy" if self.activity in ("gaming", "heavy", "hot") else "light"

    def set_hold(self, minutes):
        """A mode you pick yourself only overrides auto until the situation changes (plug or unplug, load starts or stops, heat) or the time is up; then auto takes over by itself."""
        if not minutes: self.st.set("profile_hold_until", 0); self.hold_ctx = None; return 0
        self.st.set("profile_hold_until", time.time() + float(minutes) * 60); self.hold_ctx = {"ac": self.last_ac, "bucket": self.bucket()}; return float(minutes)

    def hold_broken(self):
        """True (and the hold is dropped) once the situation you picked the mode in no longer holds."""
        c = self.hold_ctx
        if not c: return False
        low = (not self.last_ac) and self.battery_pct() < self._smart_low()
        if c["ac"] != self.last_ac or c["bucket"] != self.bucket() or self.activity == "hot" or low:
            self.set_hold(0); self.mode_changed = -999; self.last_target = None; return True
        return False

    def _smart_low(self): return (self._smart or self.smart_cfg())["battery_low_pct"]

    def smart_cfg(self):
        data = self.st.get("smart_profile_data")
        try: return smartprofiles.validate(data) if data else smartprofiles.load(self.st.get("smart_profile", "truesense"))
        except hw.HwError as e: self.log(f"smart profile invalid, using the built-in: {e}", 30); return smartprofiles.load("truesense")

    def pm_guard(self):
        try:
            if not powermgr.status(): return
            r = powermgr.takeover()
            self.log(f"power managers stopped again: {', '.join(r['stopped']) or 'none'}" + (f"; failed: {r['failed']}" if r["failed"] else ""), 20)
            if r["stopped"]: self.say("Stopped another power manager: " + ", ".join(r["stopped"]), "power")
        except Exception as e: self.log(f"power manager check failed: {e}", 30)

    def game_mode(self):
        """Mode asked for by a running game rule (None if none). The process list is read every 3rd tick (cheap, and games do not start every second)."""
        rules = self.st.get("game_rules") or {}
        if not rules: self._game = None; return None
        if self.tick % 3 == 0 or not hasattr(self, "_game"):
            try: self._game = gamerules.running(rules)
            except Exception: self._game = None
        return self._game

    def smart_target(self):
        """Smart auto: pick the power mode from what the laptop is doing and how hot it is. Returns (mode, urgent)."""
        if self._smart is None: self._smart = self.smart_cfg()
        s = self._smart; a = self.activity; cur = self.cur_mode; ac = self.last_ac
        heavy = a in ("gaming", "heavy"); hot = a == "hot"
        self.heavy_for = self.heavy_for + 1 if heavy else max(0, self.heavy_for - 1); self.idle_for = self.idle_for + 1 if a == "idle" else 0
        if hot: self.backoff_until = self.tick + s["backoff_hold_s"]
        if ac:
            if heavy or hot:
                t = s["ac_busy"]
                if s["ac_turbo"] and self.heavy_for >= s["turbo_after_s"] and self.tcpu < s["turbo_max_cpu"] and self.tgpu < s["turbo_max_gpu"] and self.tick >= self.backoff_until: t = "turbo"
                if hot and cur == "turbo": return "performance", True                      # thermal back-off, at once
                return t, False
            if a == "browsing" or self.idle_for < s["idle_after_s"]: return s["ac_browsing"], False
            return s["ac_idle"], False
        if self.battery_pct() < s["battery_low_pct"]: return "eco", True
        if hot: return s["battery_hot"], True
        if heavy: return s["battery_busy"], False
        return (s["battery_browsing"] if a == "browsing" else s["battery_idle"]), False

    def auto_profile(self):
        """Automatic power mode. native = PredatorSense-like (power source only); smart = temperature, load and battery aware; off = hands off."""
        kind = self.auto_kind()
        if kind == "off" or self.legacy("predator-auto-off") or not self.cur_mode: return
        if time.time() < self.st.get("profile_hold_until", 0) and not self.hold_broken(): return          # your own pick is still in force
        if kind == "native": target, urgent = self.c["native"]["ac" if self.last_ac else "battery"], True        # follows the plug at once
        else: target, urgent = self.smart_target()
        g = self.game_mode()
        if g and self.RANK.get(g, 0) > self.RANK.get(target, 0):                                       # a configured game is running: it can only raise the mode
            if self.activity == "hot" and g == "turbo": g = "performance"                              # same thermal back-off as Smart auto
            if not self.last_ac and self.battery_pct() < 20: g = None                                  # a nearly empty battery wins
            if g: target = g
        self.reason = f"{self.activity}, {'on power' if self.last_ac else 'on battery'}" + (f", {int(self.heavy_for)} s heavy" if kind == "smart" and self.heavy_for else "")
        if target == self.cur_mode: self.last_target = target; return
        if target == self.last_target and self.tick - self.last_try < 30: return                       # it already failed recently
        up = self.RANK[target] > self.RANK.get(self.cur_mode, 2); s = self._smart or self.smart_cfg()
        dwell = s["up_dwell_s"] if up else s["down_dwell_s"]
        if not urgent and self.tick - self.mode_changed < dwell: return                                # no flapping
        self.last_target, self.last_try = target, self.tick
        try:
            self.cur_mode = hw.set_mode(target); self.mode_changed = self.tick
            self.say(f"Power mode: {target} ({kind} auto: {self.activity if self.activity not in ('idle', 'browsing') else ('on power' if self.last_ac else 'on battery')})")
        except hw.HwError as e: self.log(f"auto profile: {e}", 30)

    def operating_lighting(self):
        """Opt-in ([keyboard] operating_follow): re-send the OPERATINGMODE colour when the power mode changes (the keyboard does not track it by itself)."""
        if not self.st.get("operating_follow") or self.tick % 2 or not self.cur_mode: return
        if self.cur_mode != getattr(self, "_op_mode", None):
            self._op_mode = self.cur_mode
            try:
                from . import keyboard
                keyboard.follow_mode(self.cur_mode, self.c["keyboard"].get("operating_colors"))
            except Exception as e: self.log(f"operating lighting failed: {e}", 30)

    # ---- output --------------------------------------------------------------------------------------------------------------------------
    def publish(self, smart):
        fan = "auto" if self.written in (None, (0, 0)) else f"{self.written[0]},{self.written[1]}"
        self.live = {"mode": "smart" if smart else "manual", "activity": self.activity, "fan": fan, "tcpu": round(self.tcpu or 0), "tgpu": round(self.tgpu or 0),
                     "chassis": round(self.tchassis or 0), "cpu": round(self.cpu_load), "gpu": round(self.gpu_util), "gpuw": round(self.gpu_power),
                     "rpm1": self.rpm[0], "rpm2": self.rpm[1], "cap": self.cap // 1000, "power_mode": self.cur_mode or "?", "ac": bool(self.last_ac), "auto": self.auto_kind() != "off", "auto_mode": self.auto_kind(), "fan_curve": self.st.get("fan_curve", "native"), "smart_profile": self.st.get("smart_profile", "native"), "mode_reason": self.reason, "fan_off": self.fan_off, "hw_ok": self.hw_ok}
        txt = "".join(f"{k}={v}\n" if not isinstance(v, bool) else f"{k}={int(v)}\n" for k, v in self.live.items())
        paths = [RUN + "/state", TMP_STATE] + (["/tmp/predator-state"] if self.c["compat"]["predator_widgets"] else [])
        for path in paths:
            try:
                with open(path + ".tmp", "w") as f: f.write(txt)
                os.chmod(path + ".tmp", 0o644); os.replace(path + ".tmp", path)
            except OSError: pass

    # ---- main step -----------------------------------------------------------------------------------------------------------------------
    def apply_uv_boot(self):
        """Opt-in undervolt at boot with a crash guard: the flag file exists from applying until 10 minutes of uptime. If it is still there at the next start,
        the previous boot did not survive, so the undervolt is switched off instead of being applied again."""
        b = self.st.get("uv_boot")
        if not b: return
        if os.path.exists(UV_PENDING):
            self.st.set("uv_boot", None)
            try: os.remove(UV_PENDING)
            except OSError: pass
            print("undervolt at boot disabled: the previous boot did not stay up for 10 minutes after applying it", flush=True); return
        try:
            os.makedirs(os.path.dirname(UV_PENDING), exist_ok=True)
            with open(UV_PENDING, "w") as f: f.write(str(time.time()))
            cpu.uv_set(0, b["core"], True); cpu.uv_set(2, b["cache"], True); self.uv_since = time.monotonic()
            print(f"undervolt at boot: core {b['core']} mV, cache {b['cache']} mV", flush=True)
        except (hw.HwError, OSError) as e:
            print(f"undervolt at boot failed: {e}", flush=True)
            try: os.remove(UV_PENDING)
            except OSError: pass

    def apply_gpu_uv_boot(self):
        """Same guard as the CPU undervolt: skipped and switched off if the previous boot did not stay up 10 minutes after applying it."""
        b = self.st.get("gpu_uv_boot")
        if not b: return
        if os.path.exists(GPU_UV_PENDING):
            self.st.set("gpu_uv_boot", None)
            try: os.remove(GPU_UV_PENDING)
            except OSError: pass
            print("GPU undervolt at boot disabled: the previous boot did not stay up for 10 minutes after applying it", flush=True); return
        try:
            os.makedirs(os.path.dirname(GPU_UV_PENDING), exist_ok=True)
            with open(GPU_UV_PENDING, "w") as f: f.write(str(time.time()))
            gpuundervolt.apply(b["offset"], b["cap"]); self.gpu_uv_since = time.monotonic(); print(f"GPU undervolt at boot: offset {b['offset']} MHz, cap {b['cap']} MHz", flush=True)
        except (hw.HwError, OSError) as e:
            print(f"GPU undervolt at boot failed: {e}", flush=True)
            try: os.remove(GPU_UV_PENDING)
            except OSError: pass

    def charge_full_check(self):
        """\"Charge to 100 % once\": the 80 % limit is lifted for one charge and comes back by itself when you unplug, when the battery has been full for 5 minutes, or after 24 hours."""
        cf = self.st.get("charge_full")
        if not cf: return
        done = None
        if not self.last_ac: done = "unplugged"
        elif time.time() - cf["since"] > 86400: done = "24 hours passed"
        elif self.battery_pct() >= 100:
            cf["full_at"] = cf.get("full_at") or time.time(); self.st.set("charge_full", cf)
            if time.time() - cf["full_at"] > 300: done = "the battery is full"
        if done:
            try: hw.set_battery_limit(True)
            except hw.HwError as e: self.log(f"could not bring the 80 % limit back: {e}", 30); return
            self.st.set("charge_full", None); self.log(f"80 % charge limit is back ({done})", 5); self.say(f"80 % charge limit is back ({done})", "battery")

    def apply_boot_look(self, why):
        """Put the look chosen for boot on the lights (the keyboard may enumerate a little after we start, so retry for ~20 s)."""
        def work():
            look = looks.read_boot()
            if not look: return
            for attempt in range(20):
                try:
                    with self.lock: done = looks.apply(look)
                    print(f"boot look ({why}): " + "; ".join(done), flush=True); return
                except (hw.HwError, OSError) as e:
                    if attempt == 19: print(f"boot look ({why}) failed: {e}", flush=True)
                    time.sleep(1)
        threading.Thread(target=work, daemon=True, name="boot-look").start()

    def step(self):
        wall, mono = time.time(), time.monotonic()
        if self.fan_off and ((self.tcpu or 0) >= FAN_OFF_MAX_CPU or (self.tgpu or 0) >= FAN_OFF_MAX_GPU or not self.hw_ok):
            self.fan_off = False
            try: hw.fan_auto()
            except hw.HwError: pass
            self.written = None; self.log("fans back on Auto: the laptop got warm", 1); self.say("Fans back on Auto: it got warm", "fan")
        if self.gpu_uv_since and mono - self.gpu_uv_since > 600:
            self.gpu_uv_since = None
            try: os.remove(GPU_UV_PENDING)
            except OSError: pass
        if self.uv_since and mono - self.uv_since > 600:                                 # the boot undervolt survived 10 minutes: drop the crash-guard flag
            self.uv_since = None
            try: os.remove(UV_PENDING)
            except OSError: pass
        if self.last_clock and (wall - self.last_clock[0]) - (mono - self.last_clock[1]) > 5: self.apply_boot_look("resume from suspend")     # wall clock ran, monotonic did not
        self.last_clock = (wall, mono)
        with self.lock: self._step()
        # Adaptive pace: once a second while something smart is working or the laptop is busy, every two seconds when nothing smart is on or it has been calm for a while.
        smart = self.smart_active() or self.auto_kind() == "smart"
        self.interval = 1.0 if (smart and (self.activity != "idle" or self.calm < 30)) or self.fan_off else 2.0
        if smart: self.ensure_gpu_stream()
        if self.tick % 5 == 0: self.charge_full_check()
        if self.tick % 150 == 5 and self.st.get("pm_takeover"): self.pm_guard()           # while "TrueSense manages power" is on, a manager that came back (update, reboot) is stopped again

    def _step(self):
        self.tick += 1
        if self.tick % 30 == 1 and not self.coretemp: self.find_hwmon()
        self.read_cpu_load()
        pkg = num(self.coretemp + "/temp1_input") if self.coretemp else None
        s = None
        try: s = hw.sensors(); self.hw_ok = True
        except hw.HwError as e: self.hw_ok = False; self.log(f"device unavailable, waiting: {e}")
        raw_c = max((pkg or 0) / 1000, s["cpu_temp"] if s else 0)
        raw_g = max(s["gpu_temp"] if s else 0, self.gpu_temp if time.time() - self.gpu_seen <= 6 and self.gpu_temp else 0)
        if raw_c <= 0: self.write_fan(0, 0); return                       # no sensors at all: firmware decides
        self.tcpu = ema(self.tcpu, raw_c, 0.4); self.tgpu = ema(self.tgpu, raw_g if raw_g > 0 else raw_c, 0.4)
        if s: self.rpm = (s["cpu_fan_rpm"], s["gpu_fan_rpm"])
        if s and s["sys_temp"]: self.tchassis = ema(self.tchassis, s["sys_temp"], 0.2)
        self.activity = self.classify()
        if self.tick % 15 == 0: self.verify_fan()
        ac = hw.on_ac()
        if self.last_ac is not None and ac != self.last_ac: self.written = None; self.last_target = None; self.cap_changed = self.tick   # plug/unplug can reset the driver state
        self.last_ac = ac
        if self.hw_ok and (self.cur_mode is None or self.tick % 3 == 0):
            try: self.cur_mode = hw.get_mode()
            except hw.HwError: pass
        smart = bool(self.st.get("smart_fan")) and not self.legacy("predator-fan-mode") and self.hw_ok
        if smart: self.smart_fan()
        else: self.lvl, self.written, self.calm = [0, 0], None, 0
        if self.tick % 3 == 1: self.enforce_power_limit()
        self.thermal_governor()
        if self.hw_ok: self.auto_profile(); self.operating_lighting()
        self.publish(smart)

    def shutdown(self, *_):
        try: hw.fan_auto()
        except Exception: pass
        self.cap = self.p["cap_max_khz"]
        for p in self.cpus:                                               # uncap to the CPU's own maximum
            m = rd(os.path.dirname(p) + "/cpuinfo_max_freq")
            if m: wr(p, m)
        raise SystemExit(0)

    # ---- control socket handlers -----------------------------------------------------------------------------------------------------
    def handlers(self):
        def get_state():
            return {"live": self.live, "settings": self.st.snapshot(), "hold_left_s": max(0, int(self.st.get("profile_hold_until", 0) - time.time()))}

        def set_(key, value):
            if key == "auto_mode":
                if value not in ("off", "native", "smart"): raise hw.HwError("auto_mode: off | native | smart")
                self.st.set("auto_mode", value); self.st.set("auto_profile", value != "off"); self.log(f"setting auto_mode = {value}", 20)
                if value != "off": self.set_hold(0); self.mode_changed = -999; self.last_target = None; self.heavy_for = 0         # choosing auto means: take over now
                return self.st.snapshot()
            if key not in ("smart_fan", "auto_profile", "operating_follow"): raise hw.HwError(f"unknown setting {key!r}")
            if not isinstance(value, bool): raise hw.HwError("value must be true or false")
            if key == "auto_profile": self.st.set("auto_mode", "smart" if value else "off")
            self.st.set(key, value); self.log(f"setting {key} = {value}", 20)
            if key == "smart_fan" and not value: self.written = None
            return self.st.snapshot()

        def fan(mode, cpu=None, gpu=None, confirm=False):
            """One request = smart curve off + hardware write, atomically (the main loop takes the same lock)."""
            with self.lock:
                if mode == "smart": self.st.set("smart_fan", True); self.fan_off = False; self.log("setting smart_fan = True", 20); return "smart"
                if mode == "off": mode, cpu, gpu = "manual", 0, 0
                if mode not in ("auto", "max", "manual"): raise hw.HwError(f"fan mode {mode!r}")
                stop = False
                if mode == "manual":
                    c = int(cpu); g = int(c if gpu is None else gpu)
                    if any(v != 0 and not 20 <= v <= 100 for v in (c, g)): raise hw.HwError("manual fan duty is 20..100 % per fan, or 0 to stop that fan")
                    stop = 0 in (c, g)
                    if stop:
                        if not confirm: raise hw.HwError("stopping a fan needs explicit confirmation")
                        if self.tcpu is None or self.tcpu >= FAN_OFF_START_CPU or (self.tgpu or 0) >= FAN_OFF_START_GPU: raise hw.HwError(f"too warm to stop a fan (CPU {round(self.tcpu or 0)} C, GPU {round(self.tgpu or 0)} C; a fan may only be stopped below {FAN_OFF_START_CPU} / {FAN_OFF_START_GPU} C)")
                self.fan_off = False; self.st.set("smart_fan", False); self.log("setting smart_fan = False (fan %s)" % mode, 20); self.written = None; self.lvl, self.calm = [0, 0], 0
                if mode == "auto": hw.fan_auto()
                elif mode == "max": hw.fan_max()
                else:
                    hw.fan_manual(c, g)
                    if stop:
                        self.fan_off = True; which = "both fans" if c == g == 0 else ("the CPU fan" if c == 0 else "the GPU fan")
                        self.log(f"{which} stopped by you; watchdog: back to Auto at CPU {FAN_OFF_MAX_CPU} C / GPU {FAN_OFF_MAX_GPU} C", 5); return "off" if c == g == 0 else "partial-off"
                return mode

        def power(mode, hold_minutes=None):
            with self.lock:
                new = hw.set_mode(mode); self.cur_mode = new
                if hold_minutes: self.set_hold(hold_minutes)
                return new

        def fan_curve(action="get", name=None, data=None):
            """get -> the active curve; use -> switch to a built-in by name, or to a curve sent as data (kept by the daemon, so it works before login)."""
            with self.lock:
                if action == "get": return {"name": self.st.get("fan_curve", "native"), "curve": self.follower.c if self.follower else self.load_curve(), "names": curves.names()}
                if action != "use": raise hw.HwError("fan_curve: get | use")
                if data is not None: c = curves.validate(data); self.st.set("fan_curve_data", c); self.st.set("fan_curve", name or "custom")
                else: c = curves.load(name or "native"); self.st.set("fan_curve_data", None if name in curves.BUILTIN else c); self.st.set("fan_curve", name)
                if c.get("native"):                                                           # the native curve is no curve: the firmware runs the fans, like PredatorSense on Windows
                    self.st.set("smart_fan", False); self.fan_off = False; self.written = None
                    try: hw.fan_auto()
                    except hw.HwError as e: self.log(f"fan auto: {e}", 10)
                    return {"name": "native", "curve": c}
                self.follower = curves.Follower(c); self.written = None; self.log(f"setting fan_curve = {name}", 20); return {"name": self.st.get("fan_curve"), "curve": c}

        def smart_profile(action="get", name=None, data=None):
            """get -> the active Smart auto profile; use -> switch to a saved one by name or to a profile sent as data (kept by the daemon)."""
            with self.lock:
                if action == "get": return {"name": self.st.get("smart_profile", "truesense"), "profile": self._smart or self.smart_cfg(), "names": smartprofiles.names()}
                if action != "use": raise hw.HwError("smart_profile: get | use")
                if data is not None: p = smartprofiles.validate(data); self.st.set("smart_profile_data", p); self.st.set("smart_profile", name or "custom")
                else: p = smartprofiles.load(name or "truesense"); self.st.set("smart_profile_data", None if name in smartprofiles.BUILTIN else p); self.st.set("smart_profile", name)
                self._smart = p; self.log(f"setting smart_profile = {name}", 20); return {"name": self.st.get("smart_profile"), "profile": p}

        def undervolt_(action="get", core=None, cache=None, confirm=False, name=None, force=False):
            """get | apply (core, cache in mV, needs confirm) | reset | boot (name, core, cache: apply at boot, needs confirm) | boot_clear. Offsets never persist across a reboot."""
            with self.lock:
                if action == "get":
                    try: cur = {"core_mv": cpu.uv_get(0), "cache_mv": cpu.uv_get(2)}
                    except hw.HwError as e: cur = {"error": str(e)}
                    return {**cur, "applied": self.st.get("uv_applied"), "boot": self.st.get("uv_boot"), "pending": os.path.exists(UV_PENDING)}
                if action == "reset": r = cpu.uv_reset(); self.st.set("uv_applied", None); self.log("undervolt reset to stock", 5); return r
                if action == "boot_clear": self.st.set("uv_boot", None); return None
                if action in ("apply", "boot"):
                    if not confirm: raise hw.HwError("an undervolt needs explicit confirmation (it can freeze or reboot the laptop if too aggressive)")
                    p = undervolt.validate({"core": core, "cache": cache}); lvl, why = undervolt.risk(p["core"], p["cache"])
                    if lvl == "danger" and not force: raise hw.HwError(why + " Add the explicit force flag if you really mean it (CLI: --force).")
                    if action == "boot": self.st.set("uv_boot", {"name": name, "core": p["core"], "cache": p["cache"]}); return self.st.get("uv_boot")
                    try: cpu.uv_set(0, p["core"], True); cpu.uv_set(2, p["cache"], True)
                    except hw.HwError:
                        try: cpu.uv_reset()
                        except hw.HwError: pass
                        raise
                    self.st.set("uv_applied", {"name": name, "core": p["core"], "cache": p["cache"]}); self.log(f"undervolt applied: core {p['core']} mV, cache {p['cache']} mV", 5)
                    return {"core_mv": cpu.uv_get(0), "cache_mv": cpu.uv_get(2)}
                raise hw.HwError("undervolt: get | apply | reset | boot | boot_clear")

        def gpu_undervolt(action="get", offset=None, cap=0, confirm=False, name=None, force=False):
            """get | apply (offset MHz, cap MHz; needs confirm) | reset | boot | boot_clear. Neither setting survives a reboot."""
            with self.lock:
                if action == "get":
                    try: cur = gpuundervolt.read()
                    except hw.HwError as e: cur = {"error": str(e)}
                    return {**cur, "applied": self.st.get("gpu_uv_applied"), "boot": self.st.get("gpu_uv_boot"), "pending": os.path.exists(GPU_UV_PENDING)}
                if action == "reset": r = gpuundervolt.reset(); self.st.set("gpu_uv_applied", None); self.log("GPU undervolt reset", 5); return r
                if action == "boot_clear": self.st.set("gpu_uv_boot", None); return None
                if action in ("apply", "boot"):
                    if not confirm: raise hw.HwError("a GPU undervolt needs explicit confirmation (it can crash games or the driver if too aggressive)")
                    p = gpuundervolt.validate({"offset": offset, "cap": cap}); lvl, why = gpuundervolt.risk(p["offset"], p["cap"])
                    if lvl == "danger" and not force: raise hw.HwError(why + " Add the explicit force flag if you really mean it (CLI: --force).")
                    if action == "boot": self.st.set("gpu_uv_boot", {"name": name, **p}); return self.st.get("gpu_uv_boot")
                    try: r = gpuundervolt.apply(p["offset"], p["cap"])
                    except hw.HwError:
                        try: gpuundervolt.reset()
                        except hw.HwError: pass
                        raise
                    self.st.set("gpu_uv_applied", {"name": name, **p}); self.log(f"GPU undervolt applied: offset {p['offset']} MHz, cap {p['cap']} MHz", 5); return r
                raise hw.HwError("gpu_undervolt: get | apply | reset | boot | boot_clear")

        def battery_full(action="get"):
            """get | start | cancel: charge to 100 % once, then the 80 % limit returns by itself."""
            with self.lock:
                cf = self.st.get("charge_full")
                if action == "get": return {"active": bool(cf), "limit": hw.get_battery_limit(), "capacity": self.battery_pct(), "ac": bool(self.last_ac)}
                if action == "start":
                    if not self.last_ac: raise hw.HwError("plug the charger in first")
                    if not hw.get_battery_limit(): raise hw.HwError("the 80 % limit is off already: the battery charges to 100 %")
                    hw.set_battery_limit(False); self.st.set("charge_full", {"since": time.time(), "full_at": None}); self.log("charging to 100 % once", 5); return {"active": True}
                if action == "cancel":
                    hw.set_battery_limit(True); self.st.set("charge_full", None); return {"active": False}
                raise hw.HwError("battery_full: get | start | cancel")

        def boot_look(action="get", look=None):
            if action == "get": return looks.read_boot()
            if action == "clear": looks.write_boot(None); return None
            if action == "set":
                if not isinstance(look, dict) or not (look.get("keyboard") or look.get("rear")): raise hw.HwError("a look needs keyboard and/or rear settings")
                return looks.write_boot(look)
            raise hw.HwError("boot_look: get | set | clear")

        def game_rules(action="list", name=None, mode=None):
            """Program name -> power mode while it runs. list | add | remove | clear."""
            with self.lock:
                rules = dict(self.st.get("game_rules") or {})
                if action == "add":
                    k = gamerules.clean_name(name)
                    if mode not in gamerules.MODES: raise hw.HwError("mode: " + ", ".join(gamerules.MODES))
                    rules[k] = mode
                    if len(rules) > gamerules.MAX_RULES: raise hw.HwError(f"at most {gamerules.MAX_RULES} rules")
                elif action == "remove":
                    if gamerules.clean_name(name) not in rules: raise hw.HwError(f"no rule for '{name}'")
                    del rules[gamerules.clean_name(name)]
                elif action == "clear": rules = {}
                elif action != "list": raise hw.HwError("game_rules: list | add | remove | clear")
                if action != "list": self.st.set("game_rules", rules); self.last_target = None
                return {"rules": rules, "running": gamerules.running(rules)}

        def power_managers(action="status"):
            """Other power managers (a fixed list): status | takeover (stop them, fix CPU defaults, remember) | restore (undo exactly that)."""
            if action == "status":
                return {"on": bool(self.st.get("pm_takeover")), "found": powermgr.status(), "recorded": bool(powermgr.load())}
            if action == "takeover":
                self.st.set("pm_takeover", True); r = powermgr.takeover()
                if r["failed"]: self.log(f"power managers: could not stop {r['failed']}", 30)
                self.log(f"TrueSense manages power: stopped {r['stopped'] or 'nothing (none running)'}", 20); return {**r, "on": True}
            if action == "restore":
                self.st.set("pm_takeover", False); r = powermgr.restore(); self.log(f"power managers restored: {r['restored'] or 'nothing to restore'}", 20); return {**r, "on": False}
            raise hw.HwError("power_managers: status | takeover | restore")

        def hold(minutes=None):
            with self.lock: return self.set_hold(minutes)

        def limits(pl1=None, pl2=None, reset=False, force=False):
            """Set CPU power limits and keep them (the daemon re-asserts them every few seconds); reset=True goes back to the automatic limits from the config."""
            with self.lock:
                if reset: self.st.set("pl_user", None); self.enforce_power_limit(force_now=True); return {"automatic": True, **cpu.info()}
                pl1, pl2 = float(pl1), float(pl2); lvl, why = cpu.pl_risk(pl1, pl2)
                if lvl == "danger" and not force: raise hw.HwError(why + " Add the explicit force flag if you really mean it.")
                r = cpu.set_limits(pl1, pl2); self.st.set("pl_user", {"pl1": pl1, "pl2": pl2}); self.log(f"setting power limits = {pl1}/{pl2} W", 5); return r

        def cpu_info():
            d = cpu.info(); d["user"] = self.st.get("pl_user"); d["auto"] = {"normal_w": self.p["pl_normal_w"], "gaming_w": self.p["pl_gaming_w"]}; return d

        def touch(action="status"):
            cur = touchpad.enabled()
            if action == "status": return cur
            return touchpad.set_enabled({"on": True, "off": False, "toggle": not cur}[action])

        return {"ping": lambda: "pong", "state": get_state, "set": set_, "fan": fan, "fan_curve": fan_curve, "smart_profile": smart_profile, "undervolt": undervolt_, "gpu_undervolt": gpu_undervolt, "battery_full": battery_full, "power": power, "boot_look": boot_look, "hold": hold, "game_rules": game_rules, "power_managers": power_managers, "cpu_limits": limits, "cpu_info": cpu_info, "touchpad": touch}


def main():
    cfg = config.load(); problems = config.validate(cfg)
    for p in problems: print("config problem:", p, flush=True)
    if problems:
        print("falling back to the built-in defaults for the whole configuration", flush=True)
        cfg = config.merge(config.copy.deepcopy(config.DEFAULTS), {"user": cfg["user"]})
    os.makedirs(RUN, exist_ok=True)
    st = statemod.State({"smart_fan": cfg["daemon"]["smart_fan"], "auto_profile": cfg["daemon"]["auto_profile"], "auto_mode": "native" if cfg["daemon"]["auto_profile"] else "off", "fan_curve": "native", "fan_curve_data": None, "pl_user": None, "smart_profile": "native", "smart_profile_data": None, "operating_follow": cfg["keyboard"]["operating_follow"], "profile_hold_until": 0})
    st.set("uv_applied", None); st.set("gpu_uv_applied", None)                                       # offsets never survive a reboot, so nothing is applied now
    brain = Brain(cfg, st)
    uids = [brain.pw.pw_uid] if brain.pw else []
    server = ipc.Server(brain.handlers(), uids); server.start()
    signal.signal(signal.SIGTERM, brain.shutdown); signal.signal(signal.SIGINT, brain.shutdown)
    brain.apply_boot_look("boot"); brain.apply_uv_boot(); brain.apply_gpu_uv_boot()
    sd_notify("READY=1"); nxt = time.monotonic()
    try:
        while True:
            try: brain.step()
            except SystemExit: raise
            except Exception as e:                                        # never die: a stuck low fan under load is the worst outcome
                print("step failed:", repr(e), flush=True)
                if brain.tcpu is None or brain.tcpu >= 80:
                    try: hw.fan_manual(100, 100)
                    except Exception: pass
            sd_notify("WATCHDOG=1"); nxt += brain.interval; now = time.monotonic()
            if nxt < now - 5: nxt = now; brain.written = None; brain.cap_changed = brain.tick     # woke from suspend: resync
            time.sleep(max(0.0, nxt - now))
    finally:
        server.stop(); brain.shutdown()
