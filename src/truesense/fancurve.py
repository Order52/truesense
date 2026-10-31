"""Software fan curve (the firmware has no curve table). Reads CPU/GPU temp, writes fixed duty. Always returns fans to firmware Auto on exit."""
import signal, time
from . import backend as hw

DEFAULT_CURVE = {40: 20, 50: 25, 55: 30, 60: 35, 65: 40, 70: 50, 75: 60, 80: 70, 85: 80, 90: 90, 95: 100}
CPU_CRIT, GPU_CRIT, CPU_OK, GPU_OK = 95, 87, 90, 82


class Follower:
    def __init__(self, curve=None, delay=2.0, min_duty=20):
        self.pts = sorted((curve or DEFAULT_CURVE).items()); self.delay = delay; self.min = min_duty
        self.band = None; self.cur = None; self.pending = None; self.pending_t = 0.0; self.emergency = False

    def _duty(self, b):
        below = [d for t, d in self.pts if t <= b]
        return max(self.min, below[-1]) if below else self.min

    def step(self, cpu, gpu, now):
        if cpu is None or gpu is None: return None
        if cpu >= CPU_CRIT or gpu >= GPU_CRIT: self.emergency = True
        if self.emergency and cpu <= CPU_OK and gpu <= GPU_OK: self.emergency = False; self.cur = None
        if self.emergency: self.cur = 100; return 100
        hot = max(cpu, gpu); b = int(hot // 5) * 5
        if self.band is None or b > self.band or (b < self.band and hot <= self.band - 5): self.band = b
        want = self._duty(self.band)
        if self.cur is None: self.cur = want; return want
        if want == self.cur: self.pending = None; return self.cur
        if self.pending != want: self.pending = want; self.pending_t = now
        if now - self.pending_t >= self.delay: self.cur = want; self.pending = None
        return self.cur


def parse_curve(s):
    try: return {int(a): int(b) for a, b in (p.split(":") for p in s.split(","))}
    except Exception: raise hw.HwError("bad curve, expected e.g. 40:20,60:35,80:70,95:100")




def run(curve=None, delay=2.0, log=print):
    f = Follower(curve, delay); stop = []
    signal.signal(signal.SIGINT, lambda *a: stop.append(1)); signal.signal(signal.SIGTERM, lambda *a: stop.append(1))
    last = None
    try:
        while not stop:
            now = time.monotonic(); d = f.step(hw.cpu_temp(), hw.gpu_temp(), now)
            if d is None:
                if last is not None: hw.fan_auto(); last = None; log("sensor missing -> firmware Auto")
            elif d != last:
                hw.fan_manual(d, d); last = d; log(f"duty -> {d} %")   # write only on change: VERIFIED a single write holds, repeated writes glitch the fans
            time.sleep(1)
    finally:
        hw.fan_auto(); log("fans returned to firmware Auto")
