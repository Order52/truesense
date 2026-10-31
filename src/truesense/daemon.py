"""truesense daemon: smart fan curve + thermal governor + automatic power profile (Acer Predator Helios 16 PH16-71).

Port of the earlier predator-daemon.py (measured fan table, activity classes, hysteresis, RAPL, governor) onto the verified backend.
Measured fan response (manual %, both fans): 20% ~1700 rpm, 30 2160, 40 2850, 50 3460, 60 4070, 70 4600, 80 5100, 100 6050.
Firmware Auto is the only way to reach 0 rpm, so when the machine is calm control returns to it.
Safety: fans go back to firmware Auto and clocks are uncapped on ANY exit (SIGTERM/SIGINT/exception/stop); if the loop breaks while hot the fans go to 100 %.
Compat files (kept so existing widgets keep working): <home>/.cache/predator-fan-mode ("smart"|"manual"), <home>/.cache/predator-auto-off,
/tmp/predator-state, <home>/.cache/predator-log.
"""
import glob, os, pwd, signal, socket, subprocess, threading, time
from . import backend as hw, config

PROFILE = "/sys/firmware/acpi/platform_profile"
RAPL = "/sys/class/powercap/intel-rapl:0/"
STATE = "/tmp/predator-state"


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

def notify(msg):
    path = os.environ.get("NOTIFY_SOCKET")
    if not path: return
    try:
        s = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM); s.connect("\0" + path[1:] if path[0] == "@" else path); s.send(msg.encode()); s.close()
    except OSError: pass


class Brain:
    def __init__(self, cfg):
        self.c = cfg; self.f = cfg["fan"]; self.p = cfg["cpu"]
        try: pw = pwd.getpwnam(cfg["user"])
        except KeyError: pw = pwd.getpwuid(os.getuid())
        self.pw = pw; home = pw.pw_dir
        self.fanmode_f, self.autooff_f, self.log_f = home + "/.cache/predator-fan-mode", home + "/.cache/predator-auto-off", home + "/.cache/predator-log"
        self.hold_f = home + "/.cache/predator-profile-hold"
        self.acer = self.coretemp = None
        self.prev_total = self.prev_idle = None
        self.cpu_load = self.gpu_util = self.gpu_power = 0.0
        self.gpu_temp = None; self.gpu_seen = 0.0
        self.tcpu = self.tgpu = self.tchassis = None
        self.hist_c, self.hist_g = [], []
        self.tick = self.calm = self.busy_hold = self.last_try = 0
        self.lvl = [0, 0]; self.changed = [0, 0]
        self.written = None                     # (cpu, gpu) last written, (0, 0) = firmware Auto
        self.activity = "idle"; self.last_target = None; self.last_ac = None
        self.cap = self.p["cap_max_khz"]; self.cap_changed = 0
        self.cpus = sorted(glob.glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_max_freq"))
        self.find_hwmon()
        threading.Thread(target=self.gpu_stream, daemon=True).start()

    # ---- sensors ---------------------------------------------------------
    def find_hwmon(self):
        for base in glob.glob("/sys/class/hwmon/hwmon*"):
            n = rd(base + "/name")
            if n == "acer": self.acer = base
            elif n == "coretemp": self.coretemp = base

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

    @staticmethod
    def on_ac():
        for n in ("ACAD", "AC", "AC0", "ADP0", "ADP1"):
            v = rd(f"/sys/class/power_supply/{n}/online")
            if v is not None: return v == "1"
        return True

    # ---- fan ---------------------------------------------------------------
    def write_fan(self, c, g):
        """Write only on change; (0, 0) = firmware Auto. A single write holds (verified), so no periodic rewrite."""
        if (c, g) == self.written: return
        try:
            hw.fan_auto() if (c, g) == (0, 0) else hw.fan_manual(max(20, c), max(20, g))
            self.written = (c, g)
        except hw.HwError as e:
            print("fan write failed:", e, flush=True); self.written = None

    def verify_fan(self):
        """Every 15 s: if the firmware/driver reset the fans behind our back (plug/unplug, resume), re-assert."""
        if self.written is None: return
        try: manual = hw.fan_state()["behavior_raw"] != "0x41"
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
        if f > 0 and self.tchassis:                      # board/VRM heat-soak hint, only under load
            f = max(f, 60 if self.tchassis >= 68 else 50 if self.tchassis >= 63 else 0)
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
        shift = 0 if self.on_ac() else f["battery_shift"]; f_idx = self.level_for(self.floor_pct())
        self.step_fan(0, self.tcpu, self.slope(self.hist_c, self.tcpu) >= 1, f["cpu_up"], shift, f_idx)
        self.step_fan(1, self.tgpu, self.slope(self.hist_g, self.tgpu) >= 1, f["gpu_up"], shift, f_idx)
        c, g = levels[self.lvl[0] - 1], levels[self.lvl[1] - 1]
        couple = 10 if (self.tcpu >= 85 or self.tgpu >= 75) else f["couple"]      # shared heatpipes
        self.write_fan(max(c, g - couple), max(g, c - couple))

    # ---- CPU power limit + thermal governor ----------------------------------
    def enforce_power_limit(self):
        pl = int((self.p["pl_gaming_w"] if self.activity == "gaming" else self.p["pl_normal_w"]) * 1_000_000)
        for n in ("constraint_0_power_limit_uw", "constraint_1_power_limit_uw"):
            if num(RAPL + n) != pl: wr(RAPL + n, pl)

    def thermal_governor(self):
        p = self.p; new = self.cap
        if self.tcpu >= p["gov_hi"] and self.tick - self.cap_changed >= 2: new = max(p["cap_min_khz"], self.cap - p["cap_down_khz"])
        elif self.tcpu <= p["gov_lo"] and self.cap < p["cap_max_khz"] and self.tick - self.cap_changed >= 3: new = min(p["cap_max_khz"], self.cap + p["cap_up_khz"])
        if new != self.cap: self.cap, self.cap_changed = new, self.tick; self.apply_cap()
        elif self.cap < p["cap_max_khz"] and self.tick % 30 == 0: self.apply_cap()

    def apply_cap(self):
        for p in self.cpus:
            if rd(p) != str(self.cap): wr(p, self.cap)

    # ---- profile -----------------------------------------------------------------
    def osd(self, text):
        if not self.c["profile"]["osd"]: return
        uid = self.pw.pw_uid
        env = {"PATH": "/usr/bin", "HOME": self.pw.pw_dir, "XDG_RUNTIME_DIR": f"/run/user/{uid}", "WAYLAND_DISPLAY": "wayland-1",
               "DBUS_SESSION_BUS_ADDRESS": f"unix:path=/run/user/{uid}/bus"}
        cmd = ["noctalia", "msg", "osd-show", "gauge", text]
        if os.geteuid() == 0: cmd = ["runuser", "-u", self.c["user"], "--"] + cmd
        try: subprocess.Popen(cmd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except OSError: pass

    def auto_profile(self):
        if not self.c["profile"]["auto"] or os.path.exists(self.autooff_f): return
        try:
            if time.time() < float(rd(self.hold_f, "0")): return                   # manual power-mode choice still in force
        except ValueError: pass
        busy = self.activity in ("gaming", "heavy", "hot")
        self.busy_hold = 20 if busy else max(0, self.busy_hold - 1); ac = self.on_ac()
        target = ("performance" if ac else "balanced") if self.busy_hold > 0 else ("balanced" if ac else "low-power")   # firmware accepts only low-power/balanced on battery
        cur = rd(PROFILE)
        if cur and cur != target and (target != self.last_target or self.tick - self.last_try >= 30):
            self.last_target, self.last_try = target, self.tick; wr(PROFILE, target)
            self.osd(f"Profile: {dict(performance='Performance', balanced='Balanced')[target] if target != 'low-power' else 'Eco'} ({self.activity if busy else ('on power' if ac else 'on battery')})")
        elif cur == target: self.last_target = target

    def operating_lighting(self):
        """Opt-in ([keyboard] operating_follow = true): re-send the OPERATINGMODE colour when the power mode changes (the keyboard does not track it by itself)."""
        k = self.c.get("keyboard", {})
        if not k.get("operating_follow") or self.tick % 2: return
        try: m = hw.get_mode()
        except hw.HwError: return
        if m != getattr(self, "_op_mode", None):
            try:
                from . import keyboard
                keyboard.follow_mode(m, k.get("operating_colors")); self._op_mode = m
            except Exception as e: print("operating lighting failed:", e, flush=True); self._op_mode = m

    # ---- output ------------------------------------------------------------------
    def publish(self, mode):
        fan = "manual" if mode == "manual" else ("auto" if self.written in (None, (0, 0)) else f"{self.written[0]},{self.written[1]}")
        s = (f"mode={mode}\nactivity={self.activity}\nfan={fan}\ntcpu={self.tcpu:.0f}\ntgpu={self.tgpu:.0f}\nchassis={(self.tchassis or 0):.0f}\n"
             f"cpu={self.cpu_load:.0f}\ngpu={self.gpu_util:.0f}\ngpuw={self.gpu_power:.0f}\ncap={self.cap // 1000}\n")
        try:
            with open(STATE + ".tmp", "w") as f: f.write(s)
            os.chmod(STATE + ".tmp", 0o644); os.replace(STATE + ".tmp", STATE)
        except OSError: pass

    def log_line(self, mode):
        if self.tick % 10 or self.activity not in ("gaming", "heavy", "hot"): return
        line = (f"{time.strftime('%T')} act={self.activity} fan={'manual' if mode == 'manual' else self.written} cpu={self.tcpu:.0f}C/{self.cpu_load:.0f}% cap={self.cap // 1000} "
                f"gpu={self.tgpu:.0f}C/{self.gpu_util:.0f}%/{self.gpu_power:.0f}W board={(self.tchassis or 0):.0f}C prof={rd(PROFILE)}\n")
        try:
            os.makedirs(os.path.dirname(self.log_f), exist_ok=True)
            with open(self.log_f, "a") as f: f.write(line)
            if os.geteuid() == 0: os.chown(self.log_f, self.pw.pw_uid, self.pw.pw_gid)
            if self.tick % 600 == 0 and os.path.getsize(self.log_f) > 400_000:
                lines = open(self.log_f).readlines()[-2000:]; open(self.log_f, "w").writelines(lines)
        except OSError: pass

    # ---- main step -----------------------------------------------------------------
    def step(self):
        self.tick += 1
        if self.tick % 30 == 1 and not (self.acer and self.coretemp and os.path.exists(self.acer)): self.find_hwmon()
        if self.tick % 15 == 0: self.verify_fan()
        self.read_cpu_load()
        pkg = num(self.coretemp + "/temp1_input") if self.coretemp else None
        ec = [num(self.acer + f"/temp{i}_input") if self.acer else None for i in (1, 2, 3)]
        raw_c = max(pkg or 0, ec[0] or 0) / 1000
        raw_g = max((ec[1] or 0) / 1000, self.gpu_temp if time.time() - self.gpu_seen <= 6 and self.gpu_temp else 0)
        if raw_c <= 0: self.write_fan(0, 0); return                  # no sensors: firmware decides
        self.tcpu = ema(self.tcpu, raw_c, 0.4); self.tgpu = ema(self.tgpu, raw_g if raw_g > 0 else raw_c, 0.4)
        if ec[2]: self.tchassis = ema(self.tchassis, ec[2] / 1000, 0.2)
        self.activity = self.classify()
        ac = self.on_ac()
        if self.last_ac is not None and ac != self.last_ac: self.written = None; self.last_target = None; self.cap_changed = self.tick   # plug/unplug resets the driver state
        self.last_ac = ac
        mode = "manual" if rd(self.fanmode_f) == "manual" else "smart"
        if mode == "smart": self.smart_fan()
        else: self.lvl, self.written, self.calm = [0, 0], None, 0
        if self.tick % 3 == 1: self.enforce_power_limit()
        self.thermal_governor(); self.auto_profile(); self.operating_lighting(); self.publish(mode); self.log_line(mode)

    def shutdown(self, *_):
        try: hw.fan_auto()
        except Exception: wr("/sys/module/linuwu_sense/drivers/platform:acer-wmi/acer-wmi/predator_sense/fan_speed", "0,0")
        self.cap = self.p["cap_max_khz"]; self.apply_cap()
        raise SystemExit(0)


def main():
    brain = Brain(config.load())
    signal.signal(signal.SIGTERM, brain.shutdown); signal.signal(signal.SIGINT, brain.shutdown)
    notify("READY=1"); nxt = time.monotonic()
    try:
        while True:
            try: brain.step()
            except SystemExit: raise
            except Exception as e:                                # never die: a stuck low fan under load is the worst outcome
                print("step failed:", repr(e), flush=True)
                if brain.tcpu is None or brain.tcpu >= 80:
                    try: hw.fan_manual(100, 100)
                    except Exception: pass
            notify("WATCHDOG=1"); nxt += 1; now = time.monotonic()
            if nxt < now - 5: nxt = now; brain.written = None; brain.cap_changed = brain.tick     # woke from suspend: resync
            time.sleep(max(0.0, nxt - now))
    finally:
        brain.shutdown()
