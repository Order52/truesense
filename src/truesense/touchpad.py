# SPDX-License-Identifier: MIT
"""Touchpad on/off through the kernel's per-device `inhibited` switch (works on any compositor). Writing needs root: the daemon does it for users."""
import re
from . import util
from .device import HwError


def inhibit_file():
    try: txt = util.read("/proc/bus/input/devices")
    except OSError: return None
    for blk in txt.split("\n\n"):
        if "Touchpad" in blk and "SYNA" in blk.upper():
            m = re.search(r"event\d+", "".join(l for l in blk.splitlines() if l.startswith("H: Handlers=")))
            if m: return f"/sys/class/input/{m.group(0)}/device/inhibited"
    return None


def enabled():
    f = inhibit_file()
    if not f: return None
    try: return util.read(f).strip() == "0"
    except OSError: return None


def set_enabled(on):
    f = inhibit_file()
    if not f: raise HwError("touchpad not found")
    try: util.write(f, "0" if on else "1")
    except OSError as e: raise HwError(f"cannot switch the touchpad ({e.strerror}); this needs root: go through the daemon")
    if enabled() != bool(on): raise HwError("touchpad readback mismatch")
    return bool(on)
