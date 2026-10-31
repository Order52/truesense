"""Lighting looks: a named snapshot of the keyboard look (effect, colour, speed, brightness, or per-key colours) and the rear light bar.

Saved per user in ~/.config/truesense/looks/NAME.json. One look can also be chosen for BOOT: the daemon (root) keeps a self-contained copy in
/var/lib/truesense/boot-look.json and applies it as soon as it starts and again after every resume from suspend, before anyone logs in.
"""
import json, os, re
from . import backend as hw, keyboard, userstate
from .device import HwError

DIR = os.path.expanduser("~/.config/truesense/looks")
BOOT_FILE = os.environ.get("TRUESENSE_BOOT_LOOK", "/var/lib/truesense/boot-look.json")


def _path(name):
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,40}", name or ""): raise HwError("look names use letters, digits, . _ - (max 40)")
    return os.path.join(DIR, name + ".json")


def names():
    try: return sorted(f[:-5] for f in os.listdir(DIR) if f.endswith(".json"))
    except FileNotFoundError: return []


def capture():
    """What is on the lights now, as remembered by this tool (the hardware cannot be asked which effect is running)."""
    kb, rear = keyboard.last(), userstate.get("rear_state")
    if not kb and not isinstance(rear, dict): raise HwError("nothing to save yet: set the keyboard or rear light first")
    return {"keyboard": kb or None, "rear": rear if isinstance(rear, dict) else None}


def save(name, look=None):
    look = look or capture(); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(look, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return look


def load(name):
    try:
        with open(_path(name)) as f: return json.load(f)
    except FileNotFoundError: raise HwError(f"no look '{name}' (truesense look list)")


def delete(name):
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no look '{name}'")


def describe(look):
    k, r = look.get("keyboard") or {}, look.get("rear") or {}
    kd = (f"keyboard {k.get('name', k.get('mode'))} brightness {k.get('brightness', '?')}" + (f" speed {k['speed']}" if k.get("speed") else "")) if k else "keyboard unchanged"
    rd = f"rear {r.get('mode')} brightness {r.get('brightness')}" if r else "rear unchanged"
    return f"{kd}; {rd}"


def apply(look):
    """Put a look on the lights. Returns a list of what was done."""
    done = []
    k = look.get("keyboard")
    if k:
        r = keyboard.replay(k)
        if r is None and k.get("mode") == "off": keyboard.off(); r = "off"
        done.append(f"keyboard: {r or 'nothing to restore'}")
    r = look.get("rear")
    if r: hw.rear_set(r["mode"], r["color"], r["speed"], r["brightness"], r.get("direction", 1), r.get("zones")); done.append(f"rear: {r['mode']}")
    return done


# ---- boot look (root; used by the daemon) -------------------------------------------------------------------------------------------------
def read_boot():
    try:
        with open(BOOT_FILE) as f: return json.load(f)
    except (OSError, ValueError): return None


def write_boot(look):
    if look is None:
        try: os.remove(BOOT_FILE)
        except FileNotFoundError: pass
        return None
    os.makedirs(os.path.dirname(BOOT_FILE), exist_ok=True)
    with open(BOOT_FILE + ".tmp", "w") as f: json.dump(look, f)
    os.replace(BOOT_FILE + ".tmp", BOOT_FILE); return look
