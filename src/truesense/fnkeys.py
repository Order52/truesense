"""Fn / PREDATOR key service (runs as the logged-in user).

The keyboard's vendor HID interface (usage page 0x88, report id 4) sends: 04 80 FF Fn+F10, 04 8A FF Fn+Esc, 04 81 FF PREDATOR press, 04 00 00 release.
Gestures on the PREDATOR key: tap / double / triple (within [keys] multi_ms) and long (held [keys] long_ms; fires while still held, so it feels instant).
Each gesture runs a configurable action (see actions.py). Fn+F11/F12 brightness is done by the keyboard firmware and deliberately ignored.
"""
import glob, os, select, threading, time
from . import util
from . import actions, config, osd
from .device import HwError

CODES = {b"\x80\xff": "fn_f10", b"\x8a\xff": "fn_esc", b"\x81\xff": "predator_down", b"\x00\x00": "release"}


def find_node():
    for h in sorted(glob.glob("/sys/class/hidraw/hidraw*")):
        try:
            if "000004F2:00000117" not in util.read(h + "/device/uevent").upper(): continue
            d = util.read(h + "/device/report_descriptor", "rb")
        except OSError: continue
        if b"\x06\x88\x00" in d or b"\x05\x88" in d: return "/dev/" + os.path.basename(h)
    return None


class Gestures:
    """Turns press/release events into tap, double, triple and long. Pure logic (clock and callbacks injected) so it is unit-testable."""

    def __init__(self, fire, long_ms=800, multi_ms=450, timer=threading.Timer):
        self.fire, self.long_s, self.multi_s, self.Timer = fire, long_ms / 1000, multi_ms / 1000, timer
        self.taps = 0; self.long_done = False; self.t_long = None; self.t_multi = None; self.lock = threading.Lock()

    def press(self):
        with self.lock:
            self._cancel(self.t_multi); self.long_done = False
            self.t_long = self.Timer(self.long_s, self._long); self.t_long.daemon = True; self.t_long.start()

    def release(self):
        with self.lock:
            self._cancel(self.t_long)
            if self.long_done: self.taps = 0; return
            self.taps += 1
            if self.taps >= 3: self._flush()
            else: self.t_multi = self.Timer(self.multi_s, self._flush_locked); self.t_multi.daemon = True; self.t_multi.start()

    @staticmethod
    def _cancel(t):
        if t: t.cancel()

    def _long(self):
        with self.lock: self.long_done = True; self.taps = 0
        self.fire("long")

    def _flush_locked(self):
        with self.lock: self._flush()

    def _flush(self):
        n, self.taps = self.taps, 0
        if n: self.fire({1: "tap", 2: "double", 3: "triple"}[min(n, 3)])


def run(cfg=None):
    cfg = cfg or config.load(); k = cfg["keys"]
    for g in ("tap", "double", "triple", "long", "fn_esc"):                    # a typo in the config must not break the key at press time
        err = actions.validate(k[g])
        if err: print(f"keys.{g} = {k[g]!r} is invalid ({err}); using 'none'", flush=True); k[g] = "none"
    node = find_node()
    if not node: raise SystemExit("Fn-key HID interface (usage page 0x88) not found")
    print("listening on", node, {g: k[g] for g in ("tap", "double", "triple", "long", "fn_esc")}, flush=True)

    def fire(spec, label):
        try:
            msg = actions.run(spec, cfg); print(label, "->", spec, "->", msg or "ok", flush=True)
            if msg: osd.show(msg, "power", cfg)
        except Exception as e:
            print(label, "->", spec, "FAILED:", e, flush=True); osd.show(f"TrueSense: {e}", "power", cfg)

    def do(gesture):
        spec = k[gesture]
        try:
            msg = actions.run(spec, cfg); print(gesture, "->", spec, "->", msg or "ok", flush=True)
            if msg: osd.show(msg, "power", cfg)
        except Exception as e:
            print(gesture, "->", spec, "FAILED:", e, flush=True); osd.show(f"TrueSense: {e}", "power", cfg)

    g = Gestures(do, k["long_ms"], k["multi_ms"])
    with open(node, "rb", buffering=0) as f:
        while True:
            r = f.read(8)
            if len(r) < 3 or r[0] != 0x04: continue
            ev = CODES.get(bytes(r[1:3]))
            if ev == "predator_down": g.press()
            elif ev == "release": g.release()
            elif ev == "fn_f10": threading.Thread(target=lambda: fire("touchpad-toggle", "fn_f10"), daemon=True).start()
            elif ev == "fn_esc": do("fn_esc")
