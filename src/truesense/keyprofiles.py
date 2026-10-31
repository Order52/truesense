"""Saved keyboard lighting profiles: ~/.config/truesense/lighting/<name>.json (128 colours)."""
import json, os
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

def cli(V, B):
    act = V[0] if V else "list"
    if act == "list": print("\n".join(sorted(f[:-5] for f in os.listdir(DIR))) if os.path.isdir(DIR) else "(none)")
    elif act == "load": load(V[1], B)
    elif act == "save":                                    # save NAME fill|rainbow|gradient c1 c2|keys a=b ...
        spec = V[2:]; kind = spec[0] if spec else "fill"
        if kind == "rainbow": cv = km.rainbow()
        elif kind == "gradient": cv = km.gradient(km.hexcolor(spec[1]), km.hexcolor(spec[2]))
        elif kind == "fill": cv = km.canvas(km.hexcolor(spec[1]))
        else: raise HwError("save NAME fill COLOR | gradient C1 C2 | rainbow")
        save(V[1], km.to_list(cv)); print("saved", V[1])
    else: raise HwError("profile list | load NAME | save NAME fill COLOR|gradient C1 C2|rainbow")
