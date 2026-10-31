# SPDX-License-Identifier: MIT
"""PREDATOR key binding profiles: a named set of what each gesture does (tap, double, triple, long press, Fn+Esc) and the press timing.

Built-in profiles are read-only (copy one to make your own); yours live in ~/.config/truesense/keys/NAME.json. Using a profile writes its actions into your
config ([keys]) and records its name; the key service notices the change and reloads by itself.
"""
import json, os, re
from . import actions, config
from .device import HwError

DIR = os.path.expanduser("~/.config/truesense/keys")
GESTURES = ("tap", "double", "triple", "long", "fn_esc")
LABELS = {"tap": "Tap", "double": "Double tap", "triple": "Triple tap", "long": "Long press", "fn_esc": "Fn + Esc"}
TIMING = {"long_ms": (300, 2000), "multi_ms": (200, 1000)}
DEFAULT = {"tap": "power-next", "double": "output-toggle", "triple": "none", "long": "fan-max-toggle", "fn_esc": "none", "long_ms": 800, "multi_ms": 450}

BUILTIN = {
    "native": {"description": "PredatorSense default: the PREDATOR key opens the app, nothing else is bound", **DEFAULT, "tap": "exec:truesense gui", "double": "none", "long": "none"},
    "default": {"description": "The standard behaviour: tap steps the power mode, double tap switches speakers/headphones, long press toggles Max fans", **DEFAULT},
    "gaming": {"description": "Gaming: tap toggles Performance and Turbo, double tap Max fans, triple tap the next lighting effect, long press Smart auto, Fn+Esc mutes the microphone",
               **DEFAULT, "tap": "power-cycle:performance,turbo", "double": "fan-max-toggle", "triple": "keyboard-effect-next", "long": "auto-next", "fn_esc": "mic-toggle"},
    "work": {"description": "Work: tap steps Eco, Quiet and Balanced, double tap switches audio, triple tap mutes the microphone, long press toggles the touchpad",
             **DEFAULT, "tap": "power-cycle:eco,quiet,balanced", "double": "output-toggle", "triple": "mic-toggle", "long": "touchpad-toggle"},
    "lights": {"description": "Lighting: tap the next keyboard effect, double tap the next rear effect, triple tap the keyboard light, long press the rear light",
               **DEFAULT, "tap": "keyboard-effect-next", "double": "rear-effect-next", "triple": "keyboard:toggle", "long": "rear:toggle"},
}


def validate(p):
    if not isinstance(p, dict): raise HwError("a key profile is an object with tap, double, triple, long and fn_esc")
    out = {"description": p.get("description", "")}
    for g in GESTURES:
        spec = p.get(g, DEFAULT[g]); err = actions.validate(spec) if isinstance(spec, str) else "an action is text"
        if err: raise HwError(f"{LABELS[g]}: {err}")
        out[g] = spec
    for k, (lo, hi) in TIMING.items():
        v = p.get(k, DEFAULT[k])
        if not isinstance(v, int) or isinstance(v, bool) or not lo <= v <= hi: raise HwError(f"{k}: {lo}..{hi}")
        out[k] = v
    return out


def _path(name):
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,40}", name or ""): raise HwError("profile names use letters, digits, . _ - (max 40)")
    return os.path.join(DIR, name + ".json")


def names(): return list(BUILTIN) + sorted(f[:-5] for f in (os.listdir(DIR) if os.path.isdir(DIR) else []) if f.endswith(".json") and f[:-5] not in BUILTIN)


def load(name):
    if name in BUILTIN: return validate(BUILTIN[name])
    try:
        with open(_path(name)) as f: return validate(json.load(f))
    except FileNotFoundError: raise HwError(f"no key profile '{name}' (truesense keys profile list)")
    except ValueError: raise HwError(f"key profile '{name}' is not valid JSON")


def save(name, profile):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and read-only; copy it (keys profile copy {name}) and edit the copy")
    p = validate(profile); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(p, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return p


def delete(name):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and cannot be deleted")
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no key profile '{name}'")


def copy(name, new=None):
    base = new or name + "-copy"; n, target = 2, base
    while target in names(): target = f"{base}-{n}"; n += 1
    save(target, load(name)); return target


def use(name):
    """Make a profile the active one: its actions go into the user config; the key service reloads on its own."""
    p = load(name); config.write_user({"keys": {**{g: p[g] for g in GESTURES}, **{k: p[k] for k in TIMING}, "profile": name}}); return p


def current():
    """The bindings in force now (what the key service uses), plus the profile they came from."""
    k = config.load()["keys"]; return {**{g: k[g] for g in GESTURES}, **{t: k[t] for t in TIMING}, "profile": k.get("profile", "default")}
