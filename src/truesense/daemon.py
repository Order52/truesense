"""truesense daemon (root): smart fan curve, thermal governor, CPU power limits, automatic power profile, and the control socket.

Safety: fans go back to firmware Auto and clocks are uncapped on ANY exit (SIGTERM/SIGINT/exception/stop); if the loop breaks while hot the fans go to 100 %;
a missing/failing device never kills the daemon (it keeps waiting and retrying). Measured fan response (manual %, both fans):
20 % ~1700 rpm, 30 2160, 40 2850, 50 3460, 60 4070, 70 4600, 80 5100, 100 6050. Firmware Auto is the only way to reach 0 rpm, so when calm control goes back to it.
"""
import glob, os, pwd, signal, socket, subprocess, sys, threading, time
from . import backend as hw, config, cpu, ipc, looks, osd, state as statemod, touchpad

RUN = os.environ.get("TRUESENSE_RUN", "/run/truesense")
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
        self.lvl = [0, 0]; self.changed = [0, 0]
        self.written = None                      # (cpu, gpu) last written, (0, 0) = firmware Auto
        self.activity = "idle"; self.last_target = None; self.last_ac = None; self.cur_mode = None
        self.cap = self.p["cap_max_khz"]; self.cap_changed = 0
        self.cpus = sorted(glob.glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_max_freq"))
        self.hw_ok = True; self._last_err = 0.0; self.live = {}; self.lock = threading.RLock(); self.last_clock = None
        self.find_hwmon()
        threading.Thread(target=self.gpu_stream, daemon=True).start()

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
        if self.tcpu >= self.f["hot_cpu"] or self.tgpu >= self.f["hot_gpu"]: return "hot"
        if self.gpu_power >= 35 or self.gpu_util >= 40: return "gaming"
        if self.cpu_load >= 55: return "heavy"
        if self.cpu_load < 8 and self.gpu_util < 10: return "idle"
        return "browsing"

    def level_for(self, pct):
        if pct <= 0: return 0
        for i, p in enumerate(self.f["levels"], 1):
            if p >= pct: return i
        return len(self.f["levels"])

    def floor_pct(self):
        f = 60 if (self.activity == "gaming" and self.gpu_power >= 80) else 50 if self.activity == "gaming" else 40 if self.activity == "heavy" else 0
        if f > 0 and self.tchassis: f = max(f, 60 if self.tchassis >= 68 else 50 if self.tchassis >= 63 else 0)    # board/VRM heat-soak hint
        return f

    def step_fan(self, i, temp, rising, up, shift, floor_idx):
        t_eff = temp + (self.f["predict"] if rising else 0); want = 0
        for k, thr in enumerate(up, 1):
            if t_eff >= thr + shift: want = k
        want = max(want, floor_idx, 1); cur = self.lvl[i]
        if want > cur: self.lvl[i], self.changed[i] = want, self.tick
        elif want < cur:
            entry = up[cur - 1] + shift
            if temp < entry - self.f["hysteresis"] and floor_idx < cur and self.tick - self.changed[i] >= self.f["down_hold"]:
                self.lvl[i], self.changed[i] = cur - 1, self.tick

    def smart_fan(self):
        f = self.f; levels = f["levels"]
        calm_now = self.activity in ("idle", "browsing") and self.tcpu < f["calm_cpu"] and self.tgpu < f["calm_gpu"]
        self.calm = self.calm + 1 if calm_now else 0
        if self.calm >= f["calm_hold"] or (self.lvl == [0, 0] and calm_now):
            self.lvl = [0, 0]; self.write_fan(0, 0); return
        if self.activity == "hot":
            self.lvl, self.changed = [len(levels)] * 2, [self.tick] * 2; self.write_fan(100, 100); return
        shift = 0 if self.last_ac else f["battery_shift"]; f_idx = self.level_for(self.floor_pct())
        self.step_fan(0, self.tcpu, self.slope(self.hist_c, self.tcpu) >= 1, f["cpu_up"], shift, f_idx)
        self.step_fan(1, self.tgpu, self.slope(self.hist_g, self.tgpu) >= 1, f["gpu_up"], shift, f_idx)
        c, g = levels[self.lvl[0] - 1], levels[self.lvl[1] - 1]
        couple = 10 if (self.tcpu >= 85 or self.tgpu >= 75) else f["couple"]      # shared heatpipes
        self.write_fan(max(c, g - couple), max(g, c - couple))

    # ---- CPU power limit + thermal governor -------------------------------------------------------------------------------------------
    def enforce_power_limit(self):
        pl = int((self.p["pl_gaming_w"] if self.activity == "gaming" else self.p["pl_normal_w"]) * 1_000_000)
        for n in ("constraint_0_power_limit_uw", "constraint_1_power_limit_uw"):
            if num(RAPL + n) != pl: wr(RAPL + n, pl)

    def thermal_governor(self):
        if not self.p["gov_enabled"]: return
        p = self.p; new = self.cap
        if self.tcpu >= p["gov_hi"] and self.tick - self.cap_changed >= 2: new = max(p["cap_min_khz"], self.cap - p["cap_down_khz"])
        elif self.tcpu <= p["gov_lo"] and self.cap < p["cap_max_khz"] and self.tick - self.cap_changed >= 3: new = min(p["cap_max_khz"], self.cap + p["cap_up_khz"])
        if new != self.cap: self.cap, self.cap_changed = new, self.tick; self.apply_cap()
        elif self.cap < p["cap_max_khz"] and self.tick % 30 == 0: self.apply_cap()

    def apply_cap(self):
        for p in self.cpus:
            if rd(p) != str(self.cap): wr(p, self.cap)

    # ---- power profile -------------------------------------------------------------------------------------------------------------------
    def auto_profile(self):
        if not self.st.get("auto_profile") or self.legacy("predator-auto-off"): return
        if time.time() < self.st.get("profile_hold_until", 0): return            # a manual choice is in force
        busy = self.activity in ("gaming", "heavy", "hot")
        self.busy_hold = 20 if busy else max(0, self.busy_hold - 1); ac = self.last_ac
        a = self.c["profile"]["auto"]
        target = (a["ac_busy"] if ac else a["battery_busy"]) if self.busy_hold > 0 else (a["ac_normal"] if ac else a["battery_idle"])
        if self.cur_mode and self.cur_mode != target and (target != self.last_target or self.tick - self.last_try >= 30):
            self.last_target, self.last_try = target, self.tick
            try:
                self.cur_mode = hw.set_mode(target)
                self.say(f"Power mode: {target} ({self.activity if busy else ('on power' if ac else 'on battery')})")
            except hw.HwError as e: self.log(f"auto profile: {e}", 30)
        elif self.cur_mode == target: self.last_target = target

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
                     "rpm1": self.rpm[0], "rpm2": self.rpm[1], "cap": self.cap // 1000, "power_mode": self.cur_mode or "?", "ac": bool(self.last_ac), "auto": bool(self.st.get("auto_profile")), "hw_ok": self.hw_ok}
        txt = "".join(f"{k}={v}\n" if not isinstance(v, bool) else f"{k}={int(v)}\n" for k, v in self.live.items())
        paths = [RUN + "/state", TMP_STATE] + (["/tmp/predator-state"] if self.c["compat"]["predator_widgets"] else [])
        for path in paths:
            try:
                with open(path + ".tmp", "w") as f: f.write(txt)
                os.chmod(path + ".tmp", 0o644); os.replace(path + ".tmp", path)
            except OSError: pass

    # ---- main step -----------------------------------------------------------------------------------------------------------------------
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
        if self.last_clock and (wall - self.last_clock[0]) - (mono - self.last_clock[1]) > 5: self.apply_boot_look("resume from suspend")     # wall clock ran, monotonic did not
        self.last_clock = (wall, mono)
        with self.lock: self._step()

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
            if key not in ("smart_fan", "auto_profile", "operating_follow"): raise hw.HwError(f"unknown setting {key!r}")
            if not isinstance(value, bool): raise hw.HwError("value must be true or false")
            self.st.set(key, value); self.log(f"setting {key} = {value}", 20)
            if key == "smart_fan" and not value: self.written = None
            return self.st.snapshot()

        def fan(mode, cpu=None, gpu=None):
            """One request = smart curve off + hardware write, atomically (the main loop takes the same lock)."""
            with self.lock:
                if mode == "smart": self.st.set("smart_fan", True); self.log("setting smart_fan = True", 20); return "smart"
                if mode not in ("auto", "max", "manual"): raise hw.HwError(f"fan mode {mode!r}")
                self.st.set("smart_fan", False); self.log("setting smart_fan = False (fan %s)" % mode, 20); self.written = None; self.lvl, self.calm = [0, 0], 0
                if mode == "auto": hw.fan_auto()
                elif mode == "max": hw.fan_max()
                else:
                    c = int(cpu); g = int(c if gpu is None else gpu)
                    if not (20 <= c <= 100 and 20 <= g <= 100): raise hw.HwError("manual fan duty is 20..100 %")
                    hw.fan_manual(c, g)
                return mode

        def power(mode, hold_minutes=None):
            with self.lock:
                new = hw.set_mode(mode); self.cur_mode = new
                if hold_minutes: self.st.set("profile_hold_until", time.time() + float(hold_minutes) * 60)
                return new

        def boot_look(action="get", look=None):
            if action == "get": return looks.read_boot()
            if action == "clear": looks.write_boot(None); return None
            if action == "set":
                if not isinstance(look, dict) or not (look.get("keyboard") or look.get("rear")): raise hw.HwError("a look needs keyboard and/or rear settings")
                return looks.write_boot(look)
            raise hw.HwError("boot_look: get | set | clear")

        def hold(minutes=None):
            if minutes is None: self.st.set("profile_hold_until", 0); return 0
            self.st.set("profile_hold_until", time.time() + float(minutes) * 60); return float(minutes)

        def limits(pl1, pl2): return cpu.set_limits(float(pl1), float(pl2))

        def touch(action="status"):
            cur = touchpad.enabled()
            if action == "status": return cur
            return touchpad.set_enabled({"on": True, "off": False, "toggle": not cur}[action])

        return {"ping": lambda: "pong", "state": get_state, "set": set_, "fan": fan, "power": power, "boot_look": boot_look, "hold": hold, "cpu_limits": limits, "touchpad": touch}


def main():
    cfg = config.load(); problems = config.validate(cfg)
    for p in problems: print("config problem:", p, flush=True)
    if problems:
        print("falling back to the built-in defaults for the whole configuration", flush=True)
        cfg = config.merge(config.copy.deepcopy(config.DEFAULTS), {"user": cfg["user"]})
    os.makedirs(RUN, exist_ok=True)
    st = statemod.State({"smart_fan": cfg["daemon"]["smart_fan"], "auto_profile": cfg["daemon"]["auto_profile"], "operating_follow": cfg["keyboard"]["operating_follow"], "profile_hold_until": 0})
    brain = Brain(cfg, st)
    uids = [brain.pw.pw_uid] if brain.pw else []
    server = ipc.Server(brain.handlers(), uids); server.start()
    signal.signal(signal.SIGTERM, brain.shutdown); signal.signal(signal.SIGINT, brain.shutdown)
    brain.apply_boot_look("boot")
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
            sd_notify("WATCHDOG=1"); nxt += 1; now = time.monotonic()
            if nxt < now - 5: nxt = now; brain.written = None; brain.cap_changed = brain.tick     # woke from suspend: resync
            time.sleep(max(0.0, nxt - now))
    finally:
        server.stop(); brain.shutdown()
