# SPDX-License-Identifier: MIT
"""Key designs (a still picture on the keyboard) and image painting.

A design is plain data: {"base": "rrggbb", "keys": {"w": "ff6600", "arrows": "00aaff"}, "cells": {"12": "ffffff"}}; keys and groups use the names of `keyboard list-keys`.
Designs live in ~/.config/truesense/designs/NAME.json (yours win over the built-in ones of the same name). No code is loaded, so a design file cannot do anything but colour keys.
"""
import json, os
from . import keymap
from .device import HwError

BUILTIN = {
    "gaming": {"info": "WASD and the keys around it lit for play", "base": "101018", "keys": {"wasd": "ff6a00", "lshift": "ff3300", "space": "ff3300", "ctrl": "ff3300", "q": "00aaff", "e": "00aaff", "r": "00aaff", "f": "00aaff",
               "numrow": "22dd44", "tab": "ff0066", "esc": "ff0066", "arrows": "ffd000"}},
    "coding": {"info": "dark board, function keys and Esc picked out", "base": "08080f", "keys": {"letters": "1d6f8a", "numrow": "2a8fb0", "fkeys": "ff9900", "esc": "ff2244", "enter": "55ee66", "backspace": "ff5566", "arrows": "7a7aff"}},
    "arrows": {"info": "only the arrow keys and WASD", "base": "000000", "keys": {"wasd": "ffffff", "arrows": "ffffff"}},
    "ice": {"info": "cool blue to white", "base": "0a2a50", "keys": {"letters": "3ab0ff", "numrow": "7fd0ff", "fkeys": "c8ecff", "esc": "ffffff"}},
    "ember": {"info": "warm red and orange", "base": "3a0600", "keys": {"letters": "ff4a00", "numrow": "ff7a10", "fkeys": "ffb030", "esc": "ffe080"}},
    "stealth": {"info": "almost off, just the letters", "base": "000000", "keys": {"letters": "14323c"}},
}


def user_dir():
    return os.path.join(os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config"), "truesense", "designs")


def _clean(d):
    if not isinstance(d, dict): raise HwError("a design is a JSON object")
    out = {"info": str(d.get("info", ""))[:80], "base": d.get("base", "000000"), "keys": dict(d.get("keys") or {}), "cells": dict(d.get("cells") or {})}
    keymap.hexcolor(out["base"])
    for k, c in out["keys"].items(): keymap.hexcolor(c); keymap.cells_for(k)
    for k, c in out["cells"].items():
        keymap.hexcolor(c)
        if not (str(k).isdigit() and 0 <= int(k) < 128): raise HwError(f"cell '{k}': 0..127")
    return out


def load_all():
    """name -> design (built-in, then files from the user folder)."""
    res = {k: _clean(v) for k, v in BUILTIN.items()}; res = {k: dict(v, source="built-in") for k, v in res.items()}
    try: names = sorted(os.listdir(user_dir()))
    except OSError: names = []
    for fn in names:
        if not fn.endswith(".json"): continue
        try:
            with open(os.path.join(user_dir(), fn), encoding="utf-8") as f: res[fn[:-5]] = dict(_clean(json.load(f)), source="yours")
        except (ValueError, OSError, HwError): continue                       # a broken file is skipped, never fatal
    return res


def cells_of(design):
    """The 128 cell colours for a design: base, then keys/groups, then single cells."""
    cv = [keymap.hexcolor(design["base"])] * 126 + [(0, 0, 0)] * 2
    for k, c in design["keys"].items():
        rgb = keymap.hexcolor(c)
        for i in keymap.cells_for(k): cv[i] = rgb
    for k, c in design["cells"].items(): cv[int(k)] = keymap.hexcolor(c)
    return cv


def get(name):
    d = load_all().get(name)
    if not d: raise HwError(f"no design '{name}' (see: truesense keyboard design list)")
    return d


def save(name, cells):
    """Store the picture now on the keyboard as one of your designs."""
    if not name or any(ch in name for ch in "/\\.") or len(name) > 40: raise HwError("design name: letters, digits, - and _")
    os.makedirs(user_dir(), exist_ok=True)
    d = {"info": "saved from the keyboard", "base": "000000", "cells": {str(i): "%02x%02x%02x" % tuple(c) for i, c in enumerate(cells[:126])}}
    p = os.path.join(user_dir(), name + ".json"); tmp = p + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f: json.dump(d, f, indent=1)
    os.replace(tmp, p); return p


def image_cells(path, fit="stretch"):
    """Paint a picture across the keys: it is scaled to the 22 x 6 key grid (needs Pillow: pacman -S python-pillow / apt install python3-pil)."""
    try: from PIL import Image
    except ImportError: raise HwError("image painting needs Pillow (Arch: python-pillow, Debian/Ubuntu: python3-pil, Fedora: python3-pillow)")
    try: im = Image.open(path).convert("RGB")
    except (OSError, ValueError) as e: raise HwError(f"cannot read the image: {e}")
    im = im.resize((22, 6), Image.LANCZOS)
    cv = [(0, 0, 0)] * 128
    for col in range(22):
        for row in range(6):
            if col * 6 + row < 126: cv[col * 6 + row] = im.getpixel((col, row))
    return cv
