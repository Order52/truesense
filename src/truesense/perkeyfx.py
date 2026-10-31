"""Per-key animations (software). The keyboard's hardware effects cover the whole keyboard; a different animation on chosen keys has to be drawn frame by frame.

The settings live in the user state ("perkey_fx": mode, speed, keys, base colours); `loop()` runs inside the key service (it already has the keyboard) and draws about
5 frames a second while an animation is on, then puts the base colours back when you stop it. Each frame is a full upload (the keyboard takes about 190 ms), so the
animations are smooth for slow effects, not for fast ones.
"""
import math, os, random, time
from . import keyboard, keymap, userstate
from .device import HwError

MODES = {"pulse": "Pulse", "wave": "Wave", "cycle": "Colour cycle", "twinkle": "Twinkle"}
FRAME_S = 0.2


def _rgb(h): return tuple(bytes.fromhex(h))


def render(mode, t, base, keys=None, speed=5):
    """One frame: list of 128 (r, g, b). `keys` = cells the animation applies to (None = every lit key); the rest keep their base colour."""
    rate = max(1, min(10, speed)) * 0.1                    # cycles per second
    cells = [_rgb(h) for h in base]; sel = list(keys) if keys else list(range(126))
    for c in sel:
        r, g, b = cells[c]
        if mode == "pulse":
            f = 0.15 + 0.85 * (0.5 + 0.5 * math.sin(2 * math.pi * rate * t)); cells[c] = (int(r * f), int(g * f), int(b * f))
        elif mode == "wave": cells[c] = keymap.hsv((c // 6) / 22 - t * rate)
        elif mode == "cycle": cells[c] = keymap.hsv(t * rate * 0.5)
        elif mode == "twinkle":
            ph = random.Random(c * 7919).random(); s = max(0.0, math.sin(2 * math.pi * (t * rate * 0.6 + ph))) ** 12
            cells[c] = (int(r + (255 - r) * s), int(g + (255 - g) * s), int(b + (255 - b) * s))
    return cells


def get():
    fx = userstate.get("perkey_fx"); return fx if isinstance(fx, dict) else {"mode": "off"}


def set_fx(mode, speed=5, keys=None, base=None):
    if mode != "off" and mode not in MODES: raise HwError(f"animation: off, {', '.join(MODES)}")
    if not 1 <= speed <= 10: raise HwError("speed 1..10")
    base = base or ["%02x%02x%02x" % tuple(c) for c in keyboard.current_cells()]
    userstate.set("perkey_fx", {"mode": mode, "speed": speed, "keys": sorted(set(keys)) if keys else None, "base": base, "t0": time.time()}); return get()


def loop(stop=None):
    """Runs in the key service. Idle (a cheap file check twice a second) unless an animation is switched on."""
    running = False; bright = 25; last_b = 0.0
    while not (stop and stop.is_set()):
        fx = get()
        if fx.get("mode") in MODES and fx.get("base"):
            now = time.time()
            if now - last_b > 5:
                try: bright = keyboard.get_brightness() or bright
                except (HwError, OSError): pass
                last_b = now
            try: keyboard.per_key(render(fx["mode"], now - fx.get("t0", now), fx["base"], fx.get("keys"), fx.get("speed", 5)), bright, remember=False); running = True
            except (HwError, OSError): time.sleep(1.0)
            time.sleep(FRAME_S)
        else:
            if running:
                try: keyboard.per_key([_rgb(h) for h in (fx.get("base") or [])] or keyboard.current_cells(), None, remember=False)           # hand the base colours back
                except (HwError, OSError): pass
                running = False
            time.sleep(0.5)
