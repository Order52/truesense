# SPDX-License-Identifier: MIT
"""Saved keyboard lighting profiles: ~/.config/truesense/lighting/<name>.json (128 colours)."""
import json
import os
from . import keyboard as kb, keymap as km, util
from .backend import HwError

DIR = os.path.expanduser("~/.config/truesense/lighting")

def _path(n):
    if not n.replace("-", "").replace("_", "").isalnum(): raise HwError("profile names: letters, digits, - and _")
    return f"{DIR}/{n}.json"

def save(name, cells):
    os.makedirs(DIR, exist_ok=True); util.write(_path(name), json.dumps({"cells": [list(c) for c in cells]}))

def exists(name):
    try: return os.path.exists(_path(name))
    except HwError: return False

def names(): return sorted(f[:-5] for f in os.listdir(DIR)) if os.path.isdir(DIR) else []

def load(name, brightness=None):
    try: cells = [tuple(c) for c in json.loads(util.read(_path(name)))["cells"]]
    except FileNotFoundError: raise HwError(f"no profile '{name}' (truesense keyboard profile list)")
    kb.per_key(cells, brightness)

def delete(name):
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no lighting profile '{name}'")

def command(action, name=None, spec=()):
    """`truesense keyboard profile list|load|save|delete NAME [save: fill COLOR | gradient C1 C2 | rainbow]`. Returns a list (list) or a message."""
    if action == "list": return names()
    if not name: raise HwError(f"profile {action} needs a name")
    if action == "load": load(name); return f"loaded lighting profile '{name}'"
    if action == "delete": delete(name); return f"deleted lighting profile '{name}'"
    if action == "save":
        spec = list(spec) or ["fill", "00aec7"]; kind = spec[0]
        if kind == "rainbow": cv = km.rainbow()
        elif kind == "gradient" and len(spec) == 3: cv = km.gradient(km.hexcolor(spec[1]), km.hexcolor(spec[2]))
        elif kind == "fill" and len(spec) == 2: cv = km.canvas(km.hexcolor(spec[1]))
        else: raise HwError("save NAME fill COLOR | gradient C1 C2 | rainbow")
        save(name, km.to_list(cv)); return f"saved lighting profile '{name}'"
    raise HwError("profile list | load NAME | save NAME ... | delete NAME")
