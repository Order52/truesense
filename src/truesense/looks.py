# SPDX-License-Identifier: MIT
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


# What PredatorSense leaves on this laptop on Windows (from the captures): keyboard cells #00aec7 at brightness 12, rear bar Breathing #3723c7 speed 3 brightness 100.
BUILTIN = {"native": {"keyboard": {"mode": "perkey", "brightness": 12, "cells": ["00aec7"] * 126 + ["000000"] * 2},
                      "rear": {"mode": "breathing", "color": "3723c7", "speed": 3, "brightness": 100, "direction": 1, "zones": None}}}


def names():
    try: mine = sorted(f[:-5] for f in os.listdir(DIR) if f.endswith(".json") and f[:-5] not in BUILTIN)
    except FileNotFoundError: mine = []
    return list(BUILTIN) + mine


def capture():
    """What is on the lights now, as remembered by this tool (the hardware cannot be asked which effect is running)."""
    kb, rear = keyboard.last(), userstate.get("rear_state")
    if not kb and not isinstance(rear, dict): raise HwError("nothing to save yet: set the keyboard or rear light first")
    return {"keyboard": kb or None, "rear": rear if isinstance(rear, dict) else None}


def save(name, look=None):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and read-only; save your look under another name")
    look = look or capture(); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(look, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return look


def load(name):
    if name in BUILTIN: return json.loads(json.dumps(BUILTIN[name]))
    try:
        with open(_path(name)) as f: return json.load(f)
    except FileNotFoundError: raise HwError(f"no look '{name}' (truesense look list)")


def delete(name):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and cannot be deleted")
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no look '{name}'")


def copy(name, new=None):
    base = new or name + "-copy"; n, target = 2, base
    while target in names(): target = f"{base}-{n}"; n += 1
    save(target, load(name)); return target


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
