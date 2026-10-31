"""CPU undervolt profiles (Intel MSR 0x150 offsets: core plane and cache plane, in millivolts, 0 to -120).

Built-in profiles are read-only (copy one to make your own). NOTHING here applies a voltage by itself: applying always needs an explicit confirmation,
and "apply at boot" is opt-in with a crash guard (see daemon.apply_uv_boot). Offsets are not saved by the CPU: a reboot always returns to stock.
Values below are common conservative starting points for 13th-gen HX chips, NOT validated on every unit: test each profile under load before trusting it.
"""
import json, os, re
from .device import HwError

DIR = os.path.expanduser("~/.config/truesense/undervolt")
MAX_MV = 120
BUILTIN = {
    "stock": {"description": "No undervolt (0 mV)", "core": 0, "cache": 0},
    "truesense": {"description": "Our profile for the i7-13700HX: a conservative offset that usually gives a few degrees less heat at the same speed. Unverified on your unit: test under load", "core": -50, "cache": -40},
    "mild": {"description": "Very safe: small offset", "core": -30, "cache": -30},
    "balanced": {"description": "Moderate: more heat saved, more chance of instability", "core": -70, "cache": -50},
    "aggressive": {"description": "Maximum offered: only if you have tested it; can freeze or reboot", "core": -100, "cache": -80},
}


def validate(p):
    if not isinstance(p, dict): raise HwError("an undervolt profile has 'core' and 'cache' offsets in mV")
    out = {"description": p.get("description", ""), "core": p.get("core"), "cache": p.get("cache")}
    for k in ("core", "cache"):
        v = out[k]
        if not isinstance(v, (int, float)) or isinstance(v, bool) or not -MAX_MV <= v <= 0: raise HwError(f"{k}: 0 to -{MAX_MV} mV")
        out[k] = int(v)
    return out


def _path(name):
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,40}", name or ""): raise HwError("profile names use letters, digits, . _ - (max 40)")
    return os.path.join(DIR, name + ".json")


def names(): return list(BUILTIN) + sorted(f[:-5] for f in (os.listdir(DIR) if os.path.isdir(DIR) else []) if f.endswith(".json") and f[:-5] not in BUILTIN)


def load(name):
    if name in BUILTIN: return validate(BUILTIN[name])
    try:
        with open(_path(name)) as f: return validate(json.load(f))
    except FileNotFoundError: raise HwError(f"no undervolt profile '{name}' (truesense cpu undervolt list)")
    except ValueError: raise HwError(f"undervolt profile '{name}' is not valid JSON")


def save(name, profile):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and read-only; copy it (cpu undervolt copy {name}) and edit the copy")
    p = validate(profile); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(p, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return p


def delete(name):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and cannot be deleted")
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no undervolt profile '{name}'")


def copy(name, new=None):
    base = new or name + "-copy"; n, target = 2, base
    while target in names(): target = f"{base}-{n}"; n += 1
    save(target, load(name)); return target
