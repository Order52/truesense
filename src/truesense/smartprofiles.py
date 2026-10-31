# SPDX-License-Identifier: MIT
"""Smart-auto profiles: how Smart auto chooses the power mode (see daemon.smart_target).

A profile is a dict of the [smart] settings. Built-in profiles are read-only (copy one to make your own); yours live in
~/.config/truesense/smart/NAME.json. The profile in use is kept by the daemon, so it works before anyone logs in.
"""
import json
import os
import re
from .device import HwError

DIR = os.path.expanduser("~/.config/truesense/smart")
MODES = ("eco", "quiet", "balanced", "performance", "turbo")
LABELS = {"ac_idle": "On power, idle", "ac_browsing": "On power, everyday use", "ac_busy": "On power, gaming or heavy work", "battery_idle": "On battery, idle",
          "battery_browsing": "On battery, everyday use", "battery_busy": "On battery, gaming or heavy work", "battery_hot": "On battery, too hot"}
NUMS = {"turbo_after_s": (5, 300, "Heavy load for this long before Turbo (s)"), "turbo_max_cpu": (60, 95, "No Turbo above this CPU temperature (°C)"),
        "turbo_max_gpu": (55, 85, "No Turbo above this GPU temperature (°C)"), "backoff_hold_s": (10, 600, "After overheating, wait before Turbo again (s)"),
        "idle_after_s": (10, 600, "Idle for this long before the idle mode (s)"), "battery_low_pct": (5, 50, "Below this battery level, force Eco (%)"),
        "up_dwell_s": (2, 120, "Wait before moving to a faster mode (s)"), "down_dwell_s": (2, 300, "Wait before moving to a slower mode (s)")}
DEFAULTS = {"ac_idle": "balanced", "ac_browsing": "balanced", "ac_busy": "performance", "ac_turbo": True, "turbo_after_s": 30, "turbo_max_cpu": 85, "turbo_max_gpu": 75,
            "backoff_hold_s": 90, "idle_after_s": 60, "battery_idle": "eco", "battery_browsing": "eco", "battery_busy": "balanced", "battery_hot": "quiet", "battery_low_pct": 20,
            "up_dwell_s": 5, "down_dwell_s": 30}

BUILTIN = {
    "native": {"description": "PredatorSense default: Balanced on power, Eco on battery, nothing else (no load or heat logic, like Native auto)", **DEFAULTS, "ac_idle": "balanced", "ac_browsing": "balanced", "ac_busy": "balanced",
               "ac_turbo": False, "battery_idle": "eco", "battery_browsing": "eco", "battery_busy": "eco", "battery_hot": "eco", "up_dwell_s": 2, "down_dwell_s": 2},
    "truesense": {"description": "Our profile for this laptop: Balanced day to day, Performance when you game, Turbo only when sustained and cool, Eco on battery", **DEFAULTS},
    "battery-first": {"description": "Longest battery and quietest: no Turbo, light modes everywhere", **DEFAULTS, "ac_idle": "quiet", "ac_browsing": "quiet", "ac_turbo": False,
                      "battery_idle": "eco", "battery_browsing": "eco", "battery_busy": "quiet", "battery_low_pct": 30, "down_dwell_s": 20},
    "performance-first": {"description": "Fastest: Performance for everyday use on power, Turbo after 10 s of heavy load", **DEFAULTS, "ac_idle": "balanced", "ac_browsing": "performance",
                          "turbo_after_s": 10, "turbo_max_cpu": 90, "turbo_max_gpu": 80, "backoff_hold_s": 45, "battery_idle": "quiet", "battery_browsing": "balanced",
                          "battery_busy": "performance", "battery_low_pct": 15, "up_dwell_s": 3},
    "silent": {"description": "As quiet as possible: Quiet and Balanced only, no Turbo", **DEFAULTS, "ac_idle": "quiet", "ac_browsing": "quiet", "ac_busy": "balanced", "ac_turbo": False,
               "battery_idle": "eco", "battery_browsing": "eco", "battery_busy": "quiet", "battery_hot": "eco"},
}


def validate(p):
    if not isinstance(p, dict): raise HwError("a smart profile is an object of settings")
    out = {**DEFAULTS, **{k: v for k, v in p.items() if k in DEFAULTS or k == "description"}}
    for k in LABELS:
        if out[k] not in MODES: raise HwError(f"{k} must be one of {', '.join(MODES)}")
    if not isinstance(out["ac_turbo"], bool): raise HwError("ac_turbo is true or false")
    for k, (lo, hi, _) in NUMS.items():
        if not isinstance(out[k], (int, float)) or isinstance(out[k], bool) or not lo <= out[k] <= hi: raise HwError(f"{k}: {lo}..{hi}")
        out[k] = int(out[k])
    return out


def _path(name):
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,40}", name or ""): raise HwError("profile names use letters, digits, . _ - (max 40)")
    return os.path.join(DIR, name + ".json")


def names(): return list(BUILTIN) + sorted(f[:-5] for f in (os.listdir(DIR) if os.path.isdir(DIR) else []) if f.endswith(".json") and f[:-5] not in BUILTIN)


def load(name):
    if name in BUILTIN: return validate(BUILTIN[name])
    try:
        with open(_path(name)) as f: return validate(json.load(f))
    except FileNotFoundError: raise HwError(f"no smart profile '{name}' (truesense power smart list)")
    except ValueError: raise HwError(f"smart profile '{name}' is not valid JSON")


def save(name, profile):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and read-only; copy it (power smart copy {name}) and edit the copy")
    p = validate(profile); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(p, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return p


def delete(name):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and cannot be deleted")
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no smart profile '{name}'")


def copy(name, new=None):
    base = new or name + "-copy"; n, target = 2, base
    while target in names(): target = f"{base}-{n}"; n += 1
    save(target, load(name)); return target
