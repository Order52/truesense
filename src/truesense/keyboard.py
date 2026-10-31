# SPDX-License-Identifier: MIT
"""Keyboard lighting (Chicony 04F2:0117, interface 3 / usage page 0xFF02) over hidraw feature reports (see docs/PROTOCOL.md).

Packets are 8 bytes, checksum = 0xFF - (sum of bytes 0..6) & 0xFF, sent with HIDIOCSFEATURE (report id 0 prefix).
The packet formats are in docs/PROTOCOL.md; what was checked on the real keyboard is in docs/STATUS.md.
"""
import fcntl
import glob
import os
import threading
import time
from . import util, userstate
from .backend import HwError

EFFECTS = {"off": 0, "static": 1, "breathing": 2, "wave": 3, "snake": 5, "ripple": 6, "neon": 8, "raindrop": 10, "lightning": 18,
           "spot": 37, "star": 38, "fireball": 39, "snow": 40, "heartbeat": 41}
NO_COLOR = {"off", "wave", "neon"}
MAX_BRIGHT = 50


def _chk(p): return (0xFF - sum(p)) & 0xFF

def _pkt(*b): return bytes(b) + bytes([_chk(bytes(b))])

def _hidraw():
    for h in sorted(glob.glob("/sys/class/hidraw/hidraw*")):
        try:
            if "000004F2:00000117" not in util.read(h + "/device/uevent").upper(): continue
            d = util.read(h + "/device/report_descriptor", "rb")
        except OSError: continue
        if d[:3] == b"\x06\x02\xff": return "/dev/" + os.path.basename(h)   # usage page 0xFF02
    raise HwError("keyboard lighting interface (usage page 0xFF02) not found")

SIM = {"brightness": 25, "packets": [], "frames": 0, "timeout": True}      # TRUESENSE_SIM=1: a fake keyboard (the tests run the keyboard commands against it)

def _sim(): return bool(os.environ.get("TRUESENSE_SIM"))

# The keyboard's vendor interface. Opening it costs about 50 ms (measured on the PH16-71; the USB transfers themselves take 0.3 ms), so a process opens it ONCE and keeps the
# handle. Every sequence of transfers runs under one lock (a brightness read can never land in the middle of a frame upload), and a stale handle (the keyboard re-enumerates
# after suspend) is dropped and reopened once. The tiny wrappers below are what the tests replace.
_HID_LOCK = threading.RLock()
_HID = {"fd": None}
_dev_open, _dev_close, _dev_ioctl, _dev_write = os.open, os.close, fcntl.ioctl, os.write


def _hid_open():
    node = _hidraw()
    try: fd = _dev_open(node, os.O_RDWR)
    except PermissionError: raise HwError(f"no permission on {node}: add yourself to group plugdev and install the udev rule") from None
    _HID["fd"] = fd
    return fd


def _hid_drop():
    fd, _HID["fd"] = _HID["fd"], None
    if fd is not None:
        try: _dev_close(fd)
        except OSError: pass


def _hid_call(fn):
    """fn(fd) on the keyboard interface, serialised, on the kept handle; one reopen-and-retry if the handle has gone stale."""
    with _HID_LOCK:
        for attempt in (0, 1):
            fd = _HID["fd"] if _HID["fd"] is not None else _hid_open()
            try: return fn(fd)
            except OSError:
                _hid_drop()
                if attempt: raise
    return None


def prewarm():
    """Open the keyboard interface in the background (it takes about 50 ms): call it early in a process that will talk to the keyboard soon, so that time overlaps other work."""
    if _sim(): return

    def go():
        try: _hid_call(lambda fd: None)
        except (HwError, OSError): pass
    threading.Thread(target=go, daemon=True, name="hid-prewarm").start()


def _set_feature(fd, payload):
    buf = bytearray(b"\x00" + bytes(payload))                          # report id 0 + 8 data bytes
    _dev_ioctl(fd, 0xC0000000 | (len(buf) << 16) | (ord("H") << 8) | 0x06, buf)       # HIDIOCSFEATURE(len)


def _send(pkts):
    if _sim():
        for p in pkts:
            SIM["packets"].append(bytes(p))
            if p[0] == 0x08: SIM["brightness"] = p[4]
        return

    def body(fd):
        for p in pkts: _set_feature(fd, p)
    _hid_call(body)

# The keyboard stores the speed byte 0 (fastest) .. 11 (slowest) and reads it back (byte 3 of the `88` report); 12 and above are refused and become 6 (measured on the PH16-71).
# The UI scale runs the other way round: 1 = slowest .. 12 = fastest, byte = 12 - speed. (Before 0.3.10 it was 1..11 with byte = 10 - speed: the two slowest steps were
# unreachable and the top setting sent 0xFF, which the keyboard turns into a medium speed.)
SPEED_MIN, SPEED_MAX = 1, 12


def set_effect(name, color="ffffff", speed=5, brightness=30, direction=0):
    if name == "operatingmode": return set_operating_mode(color)
    if name not in EFFECTS: raise HwError(f"effect: {', '.join(EFFECTS)}")
    if not SPEED_MIN <= speed <= SPEED_MAX or not 0 <= brightness <= MAX_BRIGHT: raise HwError(f"speed {SPEED_MIN}..{SPEED_MAX}, brightness 0..{MAX_BRIGHT}")
    r, g, b = (0, 0, 0) if name in NO_COLOR else bytes.fromhex(color)
    pre = bytes([0xB1, 0, 0, 0, 0, 0, 0, 0x4E])
    if name == "off":
        _send([pre, _pkt(0x08, 0x02, 0, 0, 0, 0, 0), _pkt(0x14, 0, 0, 0, 0, 0, 0)]); userstate.set("keyboard", {"mode": "off"}); return None
    _send([pre, _pkt(0x14, 0, 0, r, g, b, 0), _pkt(0x08, 0x02, EFFECTS[name], SPEED_MAX - speed, brightness, 0x01, direction + 1)])
    userstate.set("keyboard", {"mode": "effect", "name": name, "color": color.lstrip("#").lower(), "speed": speed, "brightness": brightness, "direction": direction})
    return None

def _hexcells(cells): return ["%02x%02x%02x" % tuple(c) for c in cells[:128]]

def last():
    """What this tool last put on the keyboard (the hardware cannot be asked which effect is running): {} if unknown."""
    s = userstate.get("keyboard"); return s if isinstance(s, dict) else {}

def _get_feature(fd, n=8):
    buf = bytearray(b"\x00" + bytes(n))
    _dev_ioctl(fd, 0xC0000000 | (len(buf) << 16) | (ord("H") << 8) | 0x07, buf)   # HIDIOCGFEATURE(len)
    return bytes(buf[1:])

def get_brightness():
    """Current keyboard brightness (0..50) as the firmware reports it: byte 4 of the GET_REPORT after the `88` packet (verified: tracks writes)."""
    if _sim(): return SIM["brightness"]
    return _hid_call(lambda fd: (_set_feature(fd, [0x88, 0, 0, 0, 0, 0, 0, 0x77]), _get_feature(fd)[4])[1])

def per_key(colors, brightness=None, remember=True):
    """Direct/per-key upload (docs/PROTOCOL.md): 128 cells, 4 bytes each (00 R G B), 8 x 64-byte output reports.
    colors: list of up to 128 (r,g,b); missing cells are black. cell = col*6 + row (see re/reference/keymap_ph16-71_verified.json)."""
    if brightness is None:                                   # keep what the user set with Fn+F11/F12; never upload into darkness
        brightness = get_brightness() or 25
    if not 0 <= brightness <= MAX_BRIGHT: raise HwError(f"brightness 0..{MAX_BRIGHT}")
    cells = list(colors) + [(0, 0, 0)] * (128 - len(colors))
    data = b"".join(bytes([0, r, g, b]) for r, g, b in cells[:128])
    if _sim():
        SIM["frames"] += 8; SIM["brightness"] = brightness
        if remember: userstate.set("keyboard", {"mode": "perkey", "brightness": brightness, "cells": _hexcells(cells)})
        return
    def frame(fd):
        _set_feature(fd, [0x88, 0, 0, 0, 0, 0, 0, 0x77]); _get_feature(fd)
        _set_feature(fd, [0x12, 0, 0, 0x08, 0, 0, 0, 0xE5])
        for i in range(8):
            _dev_write(fd, b"\x00" + data[i * 64:(i + 1) * 64]); time.sleep(0.016)   # leading 0 = report id: hidraw strips it, so the device gets exactly 64 data bytes     # Windows spaces frames ~15 ms apart; unpaced writes lose the last frame
        time.sleep(0.05)
        _set_feature(fd, _pkt(0x08, 0x02, 0x33, 0x05, brightness, 0x08, 0x01))
    _hid_call(frame)
    if remember: userstate.set("keyboard", {"mode": "perkey", "brightness": brightness, "cells": _hexcells(cells)})

def restore_default(brightness=12):
    """The factory look of the keyboard: cells 0..125 = #00aec7, brightness 12."""
    per_key([(0x00, 0xAE, 0xC7)] * 126, brightness)

def set_operating_mode(color="00aec7", speed=1, brightness=0x32):
    """OPERATINGMODE (0x22): the keyboard colour reflects the power mode (docs/PROTOCOL.md: no prelude; colour packet 14 00 01 R G B 00 chk'; effect 08 03 22 05 32 02 01 chk)."""
    r, g, b = hexc(color)
    _send([_pkt(0x14, 0, 0x01, r, g, b, 0), _pkt(0x08, 0x03, 0x22, 0x05, brightness, 0x02, 0x01)])
    userstate.set("keyboard", {"mode": "operating", "color": color.lstrip("#").lower(), "brightness": brightness})

# The vendor's per-mode colours are not known: these defaults are OURS, overridable in config [keyboard.operating_colors].
OPERATING_COLORS = {"eco": "00cc66", "quiet": "3366ff", "balanced": "00aec7", "performance": "ff9900", "turbo": "ff0000"}

def follow_mode(mode, colors=None):
    """Operating-mode lighting that tracks the power mode: the keyboard does not do it by itself (verified), so the colour is re-sent on each change."""
    c = {**OPERATING_COLORS, **(colors or {})}
    set_operating_mode(c.get(mode, "00aec7"))

def hexc(c):
    import re
    c = c.lstrip("#")
    if not re.fullmatch(r"[0-9a-fA-F]{6}", c): raise HwError("colour must be RRGGBB")
    return tuple(bytes.fromhex(c))

def set_brightness(level):
    """Change brightness WITHOUT leaving the current look. The per-key brightness packet (mode 0x33) would drop a running effect back to static,
    so the effect is re-sent with the new level. If this tool did not set the current look (unknown), the per-key packet is used."""
    if not 0 <= level <= MAX_BRIGHT: raise HwError(f"brightness 0..{MAX_BRIGHT}")
    s = last()
    if s.get("mode") == "effect" and s.get("name") in EFFECTS and s["name"] != "off":
        _send([_pkt(0x08, 0x02, 0x33, 0x05, level, 0x08, 0x01)])        # the keyboard's own brightness level first (some effects, e.g. spot, follow it) ...
        return set_effect(s["name"], s.get("color", "ffffff"), s.get("speed", 5), level, s.get("direction", 0))
    if s.get("mode") == "operating": return set_operating_mode(s.get("color", "00aec7"), brightness=level)
    _send([_pkt(0x08, 0x02, 0x33, 0x05, level, 0x08, 0x01)])
    if s.get("mode") == "perkey": userstate.set("keyboard", {**s, "brightness": level})
    return None

def off():
    """Backlight off, keeping the look: brightness 0 in whatever mode is running (the effect `off` packet did not blank this keyboard)."""
    s = last()
    if s.get("mode") != "off" and (get_brightness() or 0) > 0: userstate.set("kb_restore", {**s, "brightness": get_brightness()})
    set_brightness(0)

def on():
    """Backlight back on, in the look it had before `off`."""
    s = userstate.get("kb_restore"); lvl = (s or {}).get("brightness") or 25
    if isinstance(s, dict) and s.get("mode"): userstate.set("keyboard", s)
    set_brightness(lvl)

def replay(s=None):
    """Put a remembered look back on the keyboard. Returns what was done, or None if nothing was remembered."""
    s = s if s is not None else last(); m = s.get("mode")
    if m == "effect" and s.get("name") in EFFECTS: set_effect(s["name"], s.get("color", "ffffff"), s.get("speed", 5), s.get("brightness", 25), s.get("direction", 0)); return f"effect {s['name']}"
    if m == "operating": set_operating_mode(s.get("color", "00aec7"), brightness=s.get("brightness", 0x32)); return "operating mode"
    if m == "perkey" and s.get("cells"): per_key([tuple(bytes.fromhex(c)) for c in s["cells"]], s.get("brightness") or 25); return "per-key colours"
    return None

def set_speed(speed):
    """Animation speed (1 slowest .. 12 fastest) of the running effect, keeping its colour and brightness."""
    if not SPEED_MIN <= speed <= SPEED_MAX: raise HwError(f"speed {SPEED_MIN}..{SPEED_MAX}")
    s = last()
    if s.get("mode") != "effect" or s.get("name") in (None, "off", "static"): raise HwError("the current look has no animation speed; pick an effect first")
    return set_effect(s["name"], s.get("color", "ffffff"), speed, s.get("brightness", get_brightness() or 25), s.get("direction", 0))

def set_color(color):
    """Change the colour of the current look without changing the effect. Effects without a colour (wave, neon) cannot take one."""
    hexc(color); color = color.lstrip("#").lower(); s = last(); b = get_brightness() or 25
    if s.get("mode") == "effect" and s.get("name") in EFFECTS:
        if s["name"] in NO_COLOR: raise HwError(f"the {s['name']} effect has no colour; pick another effect (or use `keyboard fill`)")
        return set_effect(s["name"], color, s.get("speed", 5), b, s.get("direction", 0))
    if s.get("mode") == "operating": return set_operating_mode(color, brightness=b)
    from . import keymap
    per_key(keymap.to_list(keymap.canvas(keymap.hexcolor(color))), b)
    return None


def current_cells():
    """The per-key colours this tool last uploaded (list of 128 (r,g,b)); the stock cyan if none are remembered."""
    s = last()
    if s.get("mode") == "perkey" and s.get("cells"): return [tuple(bytes.fromhex(c)) for c in s["cells"]]
    return [(0x00, 0xAE, 0xC7)] * 126 + [(0, 0, 0)] * 2

def paint(items, brightness=None):
    """Colour named keys or groups ({"w": "ff0000", "arrows": "00ff00"}) and leave every other key as it is."""
    from . import keymap
    cells = current_cells()
    for key, col in items.items():
        rgb = keymap.hexcolor(col)
        for c in keymap.cells_for(key): cells[c] = rgb
    per_key(cells, brightness)
    return len(items)


def paint_cells(changes, brightness=None):
    """Colour individual cells ({cell: 'rrggbb'}) and keep every other key as it is."""
    cells = current_cells()
    for c, col in changes.items(): cells[int(c)] = hexc(col)
    per_key(cells, brightness)


# ---- the keyboard controller's OWN "backlight off after 30 s" flag ------------------------------------------------------------------------
# Separate from the firmware (WMI) flag: SET is `30 01 <0|1> 00 00 00 00 chk`, GET is `b0 00 .. 4f` then read byte 2 (docs/PROTOCOL.md).
# Its factory state is ON, which is why the lights went dark after 30 s while the firmware flag read "off".
def hid_timeout_get():
    if _sim(): return bool(SIM["timeout"])

    def read(fd):
        _set_feature(fd, [0xB0, 0, 0, 0, 0, 0, 0, 0x4F]); r = _get_feature(fd)
        return bool(r[2]) if r[0] == 0xB0 else None
    return _hid_call(read)

def hid_timeout_set(on):
    _send([_pkt(0x30, 0x01, 1 if on else 0, 0, 0, 0, 0)])
    if _sim(): SIM["timeout"] = bool(on)
