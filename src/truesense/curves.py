# SPDX-License-Identifier: MIT
"""Fan curves for the smart fan: temperature (°C) -> fan duty (%), separately for the CPU fan and the GPU fan.

A curve is {"cpu": [[temp, pct], ...], "gpu": [...], "hysteresis": °C, "down_rate": %/s, "predict": °C, "couple": %, "battery_shift": °C}.
Between points the duty is interpolated; 0 % means "let the firmware run the fans" (they stop when cool). Duty may only rise with temperature.
Built-in curves are below; yours live in ~/.config/truesense/curves/NAME.json. The curve in use is kept by the daemon (so it works before login).
"""
import json, os, re
from .device import HwError

DIR = os.path.expanduser("~/.config/truesense/curves")
DEFAULTS = {"hysteresis": 4.0, "down_rate": 1.5, "predict": 5.0, "couple": 15, "battery_shift": 3.0}

# Tuned for the PH16-71 (i7-13700HX + RTX 4060, two fans on shared heatpipes): the CPU reaches 90 C in seconds under load while the GPU side is calmer, so the CPU
# curve ramps from 56 C and tops out at 89 C, the GPU curve from 54 C to 78 C. Firmware Auto below the first point (silent when cool).
BUILTIN = {
    "native": {"description": "PredatorSense default: the firmware controls the fans exactly as it does on Windows (they stop when cool). Nothing of ours runs", "native": True,
               "cpu": [[52, 0], [89, 100]], "gpu": [[48, 0], [78, 100]]},
    "truesense": {"description": "Our curve for this laptop: silent when cool, early and smooth ramp, full speed before throttling",
                  "cpu": [[52, 0], [56, 30], [62, 38], [68, 48], [74, 58], [80, 70], [85, 85], [89, 100]],
                  "gpu": [[48, 0], [54, 30], [60, 38], [65, 48], [70, 60], [74, 78], [78, 100]]},
    "quiet": {"description": "Lowest noise: the fans stay off longer and ramp later",
              "cpu": [[58, 0], [66, 30], [72, 40], [78, 55], [84, 75], [89, 100]],
              "gpu": [[52, 0], [60, 30], [66, 40], [72, 58], [77, 80], [80, 100]], "predict": 3.0, "down_rate": 1.0},
    "performance": {"description": "Cooler and louder: starts early and ramps hard",
                    "cpu": [[45, 30], [55, 40], [62, 55], [70, 70], [78, 85], [84, 100]],
                    "gpu": [[42, 30], [52, 40], [60, 55], [66, 70], [72, 85], [76, 100]], "predict": 6.0, "down_rate": 2.5},
    "max-cooling": {"description": "Fans at full speed above 60 C",
                    "cpu": [[40, 40], [60, 100]], "gpu": [[40, 40], [58, 100]], "predict": 8.0, "down_rate": 4.0},
}


def interp(points, t):
    """Piecewise-linear duty for temperature t (clamped to the first and last point)."""
    if t <= points[0][0]: return float(points[0][1])
    for (t0, p0), (t1, p1) in zip(points, points[1:]):
        if t <= t1: return p0 + (p1 - p0) * (t - t0) / (t1 - t0) if t1 > t0 else float(p1)
    return float(points[-1][1])


def parse_points(text):
    """'52:0,60:35,70:55,88:100' -> [[52, 0], [60, 35], ...]"""
    try: pts = [[float(a), float(b)] for a, b in (p.split(":") for p in text.replace(" ", "").split(",") if p)]
    except ValueError: raise HwError("points look like 55:0,62:35,70:55,80:80,88:100  (temperature:percent)")
    return [[int(a) if a == int(a) else a, int(b) if b == int(b) else b] for a, b in pts]


def validate(c):
    """Raise HwError for a curve that could hurt (too cool for the load) or is malformed; return the curve with defaults filled in."""
    if not isinstance(c, dict): raise HwError("a curve is an object with 'cpu' and 'gpu' points")
    out = {**DEFAULTS, **{k: v for k, v in c.items() if k in DEFAULTS or k in ("cpu", "gpu", "description", "native")}}
    for side, cap in (("cpu", 95), ("gpu", 85)):
        pts = out.get(side)
        if not isinstance(pts, list) or not 2 <= len(pts) <= 12: raise HwError(f"{side}: 2 to 12 points")
        try: pts = sorted([[float(t), float(p)] for t, p in pts])
        except (TypeError, ValueError): raise HwError(f"{side}: every point is [temperature, percent]")
        for t, p in pts:
            if not 20 <= t <= 110 or not (p == 0 or 20 <= p <= 100): raise HwError(f"{side}: temperature 20..110 C, percent 0 (firmware auto) or 20..100")
        if any(b[1] < a[1] for a, b in zip(pts, pts[1:])): raise HwError(f"{side}: the duty may only rise with temperature")
        if len({t for t, _ in pts}) != len(pts): raise HwError(f"{side}: two points share a temperature")
        if interp(pts, cap) < 100: raise HwError(f"{side}: must reach 100 % by {cap} C (the CPU and GPU throttle above that)")
        out[side] = [[int(t) if t == int(t) else t, int(p) if p == int(p) else p] for t, p in pts]
    for k, lo, hi in (("hysteresis", 0, 15), ("down_rate", 0.2, 20), ("predict", 0, 15), ("couple", 0, 50), ("battery_shift", 0, 15)):
        if not isinstance(out[k], (int, float)) or not lo <= out[k] <= hi: raise HwError(f"{k}: {lo}..{hi}")
    return out


def _path(name):
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,40}", name or ""): raise HwError("curve names use letters, digits, . _ - (max 40)")
    return os.path.join(DIR, name + ".json")


def names(): return list(BUILTIN) + sorted(f[:-5] for f in (os.listdir(DIR) if os.path.isdir(DIR) else []) if f.endswith(".json") and f[:-5] not in BUILTIN)


def load(name):
    if name in BUILTIN: return validate(BUILTIN[name])
    try:
        with open(_path(name)) as f: return validate(json.load(f))
    except FileNotFoundError: raise HwError(f"no fan curve '{name}' (truesense fan curve list)")
    except ValueError: raise HwError(f"fan curve '{name}' is not valid JSON")


def save(name, curve):
    if name in BUILTIN: raise HwError(f"'{name}' is built in; save your curve under another name")
    c = validate(curve); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(c, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return c


def delete(name):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and cannot be deleted")
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no fan curve '{name}'")


class Follower:
    """Turns temperatures into fan duties: rises at once, falls slowly and only after the temperature has dropped by the hysteresis."""
    def __init__(self, curve): self.set_curve(curve)

    def set_curve(self, curve): self.c = curve; self.duty = {"cpu": 0.0, "gpu": 0.0}

    def target(self, side, temp, rising, shift=0.0):
        c = self.c; t = temp + (c["predict"] if rising else 0) - shift          # battery_shift (>0 on battery) makes the fans come in later
        return interp(c[side], t)

    def step(self, side, temp, rising, dt=1.0, shift=0.0):
        c = self.c; up = self.target(side, temp, rising, shift); cur = self.duty[side]
        if up >= cur: cur = up
        else:
            down = interp(c[side], temp - shift - c["hysteresis"])
            if down < cur: cur = max(down, cur - c["down_rate"] * dt)
        self.duty[side] = cur; return cur

    def output(self, floor=0):
        """(cpu %, gpu %) in steps of 5; (0, 0) = firmware Auto. The two fans share heatpipes, so the quieter one never trails the louder by more than `couple`."""
        def q(d): return 0 if d < 20 else int(5 * round(d / 5))
        c, g = q(self.duty["cpu"]), q(self.duty["gpu"])
        if floor: c, g = max(c, floor), max(g, floor)
        if c == 0 and g == 0: return (0, 0)
        cp = self.c["couple"]; return (max(c, g - cp, 20), max(g, c - cp, 20))
