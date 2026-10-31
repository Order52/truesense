"""Key names -> lighting cells for the PH16-71 keyboard (cell = col*6 + row), from the owner-verified map.
Corrections vs the community map (verified by key presses): cell 2 = Esc, cell 5 = CapsLock, cell 70 = main-row minus."""
import json, os, re
from .backend import HwError
from . import util

_D = json.loads(util.read(os.path.join(os.path.dirname(__file__), "data", "keymap_ph16-71_verified.json")))
OVERRIDE = {2: "esc", 5: "caps", 70: "minus"}
ALIAS = {"oemminus": "minus", "capsl": "caps", "lshif": "lshift", "rshif": "rshift", "windo": "win", "optio": "menu", "retur": "enter", "backs": "backspace",
         "preda": "predator", "forwa": "next", "backw": "prev", "playp": "playpause", "numlk": "numlock", "numen": "numenter", "num.": "num.", "oemplus": "="}

def _norm(l): return ALIAS.get((l or "").lower(), (l or "").lower())

NAMES = {}            # name -> [cells]
for i, v in _D["cells"].items():
    i = int(i); n = OVERRIDE.get(i) or _norm(v.get("label"))
    if n: NAMES.setdefault(n, []).append(i)
ALL = list(range(126))                                       # cells the Windows app lit (0..125)
GROUPS = {
    "wasd": [c for n in "wasd" for c in NAMES.get(n, [])],
    "arrows": [c for n in ("up", "down", "left", "right") for c in NAMES.get(n, [])],
    "fkeys": [c for i in range(1, 13) for c in NAMES.get(f"f{i}", [])],
    "numrow": [c for n in list("1234567890") for c in NAMES.get(n, [])],
    "letters": [c for n in "abcdefghijklmnopqrstuvwxyz" for c in NAMES.get(n, [])],
    "numpad": [c for n in list(NAMES) if n.startswith("num") for c in NAMES[n]],
    "all": ALL,
}

def cells_for(name):
    n = name.lower()
    if n in GROUPS: return GROUPS[n]
    if n in NAMES: return NAMES[n]
    if re.fullmatch(r"cell\d+", n) and int(n[4:]) < 128: return [int(n[4:])]
    raise HwError(f"unknown key '{name}' (see: truesense keyboard list-keys)")

def hexcolor(c):
    c = c.lstrip("#")
    if not re.fullmatch(r"[0-9a-fA-F]{6}", c): raise HwError(f"bad colour '{c}', use RRGGBB")
    return tuple(bytes.fromhex(c))

def lerp(a, b, t): return tuple(round(a[i] + (b[i] - a[i]) * t) for i in range(3))

def hsv(h):
    h = h % 1.0; i = int(h * 6); f = h * 6 - i; q = 1 - f
    r, g, b = [(1, f, 0), (q, 1, 0), (0, 1, f), (0, q, 1), (f, 0, 1), (1, 0, q)][i % 6]
    return (round(r * 255), round(g * 255), round(b * 255))

def canvas(base=(0, 0, 0)):
    return {c: base for c in ALL}

def gradient(c1, c2):
    """Left-to-right gradient over the 22 columns (cell // 6)."""
    return {c: lerp(c1, c2, (c // 6) / 21) for c in ALL}

def rainbow():
    return {c: hsv((c // 6) / 22) for c in ALL}

def to_list(cv): return [cv.get(i, (0, 0, 0)) for i in range(128)]
