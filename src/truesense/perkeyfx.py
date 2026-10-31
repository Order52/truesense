# SPDX-License-Identifier: MIT
"""Per-key animations (software). The keyboard's hardware effects cover the whole keyboard; a different animation on chosen keys has to be drawn frame by frame.

The settings live in the user state ("perkey_fx": mode, speed, keys, base colours); `loop()` runs inside the key service (it already has the keyboard) and draws about
5 frames a second while an animation is on, then puts the base colours back when you stop it. Each frame is a full upload (the keyboard takes about 190 ms), so the
animations are smooth for slow effects, not for fast ones.
"""
import math
import time
from . import keyboard, keymap, userstate
from .device import HwError

MODES = {"pulse": "Pulse", "wave": "Wave", "cycle": "Colour cycle", "twinkle": "Twinkle",
         "rainbow": "Rainbow sweep", "breathing": "Breathing (base colours)", "scanner": "Scanner", "comet": "Comet", "ripple": "Ripples",
         "rain": "Rain", "matrix": "Matrix rain", "fire": "Fire", "plasma": "Plasma", "aurora": "Aurora", "lightning": "Lightning",
         "confetti": "Confetti", "starlight": "Starlight", "mood": "Mood lamp"}
FRAME_S = 0.2
COLS = 22


def _rgb(h): return tuple(bytes.fromhex(h))


def _h(i, k=0, z=0):
    """Stateless pseudo-random 0..1 from two integers (no state to keep between frames)."""
    x = (i * 2654435761 + k * 40503 + z * 7919 + 97) & 0xFFFFFFFF; x ^= x >> 15; x = (x * 2246822519) & 0xFFFFFFFF; x ^= x >> 13
    return (x & 0xFFFF) / 65536


def _scale(c, f):
    f = max(0.0, min(1.0, f)); return (int(c[0] * f), int(c[1] * f), int(c[2] * f))


def _mix(a, b, f):
    f = max(0.0, min(1.0, f)); return tuple(int(a[i] + (b[i] - a[i]) * f) for i in range(3))


def _heat(v):
    """0..1 -> black, red, orange, yellow-white."""
    v = max(0.0, min(1.0, v))
    if v < 0.4: return (int(255 * v / 0.4), 0, 0)
    if v < 0.75: return (255, int(160 * (v - 0.4) / 0.35), 0)
    return (255, 160 + int(95 * (v - 0.75) / 0.25), int(200 * (v - 0.75) / 0.25))


def _cell_fx(mode, c, base, t, rate):
    """Colour of one cell. Cells are numbered column * 6 + row (22 columns, 6 rows); row 0 is the top row."""
    col, row = c // 6, c % 6; x = col / (COLS - 1); lit = base if max(base) > 60 else (255, 255, 255)      # a dark key still shows the effect
    if mode == "pulse":
        return _scale(lit, 0.15 + 0.85 * (0.5 + 0.5 * math.sin(2 * math.pi * rate * t)))
    if mode == "breathing":
        return _scale(lit, 0.5 - 0.5 * math.cos(2 * math.pi * rate * 0.5 * t))
    if mode == "wave": return keymap.hsv(x - t * rate)
    if mode == "rainbow": return keymap.hsv(x * 0.8 + t * rate * 0.3 + row * 0.02)
    if mode == "cycle": return keymap.hsv(t * rate * 0.5)
    if mode == "mood": return _mix((255, 255, 255), keymap.hsv(t / 60.0 * max(1, rate * 10) / 5), 0.55)
    if mode == "twinkle":
        s = max(0.0, math.sin(2 * math.pi * (t * rate * 0.6 + _h(c, 1)))) ** 12; return _mix(base, (255, 255, 255), s)
    if mode == "starlight":
        u = t * rate * 0.35 + _h(c, 2); ph = u % 1.0; on = _h(c, 3, int(u)) < 0.25            # each key gets a new chance every cycle
        return _mix(_scale(base, 0.12), (255, 255, 255), (1 - abs(ph * 2 - 1)) if on else 0)
    if mode == "scanner":
        pos = (COLS - 1) * (1 - abs(((t * rate * 0.5) % 1.0) * 2 - 1)); return _mix(_scale(base, 0.08), lit, 1 - abs(col - pos) / 3.0)
    if mode == "comet":
        head = (t * rate * 8) % (COLS + 8); d = head - col
        return _mix(_scale(base, 0.05), lit, (1 - d / 8.0) ** 2 if 0 <= d <= 8 else 0)
    if mode == "ripple":
        dx = (col - (COLS - 1) / 2) / 3.0; dy = row - 2.5; d = math.hypot(dx, dy) * 1.5; rad = (t * rate * 6) % 12
        return _mix(_scale(base, 0.06), lit, 1 - abs(d - rad) / 1.6)
    if mode == "rain":
        top = 5 - row; head = (_h(col, 7) * 6 + t * rate * 4) % 9
        d = head - top; return _mix(_scale(base, 0.05), lit, 1 - d / 3.0 if 0 <= d < 3 else 0)
    if mode == "matrix":
        top = 5 - row; head = (t * rate * 5.5 + _h(col, 8) * 12) % 12; d = head - top
        if d < 0 or d > 4: return (0, 6, 0)
        return (180, 255, 180) if d < 1 else (0, int(255 * (1 - d / 4.0)), 0)
    if mode == "fire":
        top = 5 - row; k = int(t * rate * 4); f = (t * rate * 4) % 1.0
        n = _h(c, k) * (1 - f) + _h(c, k + 1) * f; return _heat(n * 0.55 + (0.5 - top * 0.09))
    if mode == "plasma":
        v = math.sin(x * 9 + t * rate * 2) + math.sin(row * 1.7 + t * rate * 1.3) + math.sin((x * 6 + row) + t * rate)
        return keymap.hsv(v / 6.0 + 0.5 + t * rate * 0.05)
    if mode == "aurora":
        v = 0.5 + 0.5 * math.sin(x * 6 + t * rate * 0.8 + row * 0.7) * math.sin(x * 2.5 - t * rate * 0.4)
        return _mix((0, 200, 90), (110, 40, 210), v) if v > 0.2 else (0, 10, 25)
    if mode == "lightning":
        k = int(t * rate * 6); on = _h(k, 9) < 0.18; sc = int(_h(k, 10) * COLS); w = 2 + int(_h(k, 11) * 4)
        return (220, 230, 255) if on and abs(col - sc) <= w else (5, 5, 18)
    if mode == "confetti":
        k = int(t * rate * 4); f = (t * rate * 4) % 1.0
        for j in (0, 1):
            if _h(c, k - j, 12) < 0.07: return _scale(keymap.hsv(_h(c, k - j, 13)), 1 - (j + f) / 2)
        return _scale(base, 0.1)
    return base


def render(mode, t, base, keys=None, speed=5):
    """One frame: list of 128 (r, g, b). `keys` = cells the animation applies to (None = every lit key); the rest keep their base colour."""
    rate = max(1, min(10, speed)) * 0.1                    # cycles per second
    cells = [_rgb(h) for h in base]; sel = list(keys) if keys else list(range(126))
    for c in sel: cells[c] = _cell_fx(mode, c, cells[c], t, rate)
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
    running = False; bright = 25; last_b = 0.0; last = None
    while not (stop and stop.is_set()):
        fx = get()
        if fx.get("mode") in MODES and fx.get("base"):
            now = time.time()
            if now - last_b > 5:
                try: bright = keyboard.get_brightness() or bright
                except (HwError, OSError): pass
                last_b = now
            frame = render(fx["mode"], now - fx.get("t0", now), fx["base"], fx.get("keys"), fx.get("speed", 5))
            if (frame, bright) != last:                    # an identical frame is not sent again (less USB traffic)
                try: keyboard.per_key(frame, bright, remember=False); running = True; last = (frame, bright)
                except (HwError, OSError): last = None; time.sleep(1.0)
            time.sleep(FRAME_S)
        else:
            if running:
                try: keyboard.per_key([_rgb(h) for h in (fx.get("base") or [])] or keyboard.current_cells(), None, remember=False)           # hand the base colours back
                except (HwError, OSError): pass
                running = False; last = None
            time.sleep(0.5)
