# SPDX-License-Identifier: MIT
"""What the lights and profile do at login (`truesense startup`, run by the truesense-startup user service).

[startup] keyboard / rear:  last = put back what was set last (saved automatically on every change)
                            default = the stock look   none = leave it as the firmware has it   off = dark
                            keyboard also: profile:NAME (a saved lighting profile)
[startup] profile:          a full profile (power, fans, lighting, battery, audio) applied first, or empty
"""
from . import backend as hw, config, keyboard, keyprofiles, looks, profiles, userstate
from .device import HwError


def apply(cfg=None):
    cfg = cfg or config.load(); s = cfg["startup"]; done = []
    def step(label, fn):
        try: r = fn(); done.append(f"{label}: {r}" if r else f"{label}: nothing saved")
        except (HwError, OSError, ValueError) as e: done.append(f"{label}: FAILED ({e})")
    if s["profile"]: step("profile", lambda: profiles.apply(s["profile"]))
    k = s["keyboard"]; r = s["rear"]
    if k.startswith("look:") or r.startswith("look:"):                         # a saved look sets keyboard and rear together
        look = looks.load((k if k.startswith("look:") else r)[5:])
        if k.startswith("look:"): step("keyboard", lambda: looks.apply({"keyboard": look.get("keyboard")})[0] if look.get("keyboard") else "unchanged")
        if r.startswith("look:"): step("rear", lambda: looks.apply({"rear": look.get("rear")})[0] if look.get("rear") else "unchanged")
        k = "none" if k.startswith("look:") else k; r = "none" if r.startswith("look:") else r
    if k == "last": step("keyboard", lambda: keyboard.replay())
    elif k == "default": step("keyboard", lambda: (keyboard.restore_default(None), "stock look")[1])
    elif k == "off": step("keyboard", lambda: (keyboard.off(), "off")[1])
    elif k.startswith("profile:"): step("keyboard", lambda: (keyprofiles.load(k[8:], None), k)[1])
    st = userstate.get("rear_state")
    if r == "last": step("rear", lambda: (hw.rear_set(st["mode"], st["color"], st["speed"], st["brightness"], st["direction"], st.get("zones")), "restored")[1] if isinstance(st, dict) else None)
    elif r == "default": step("rear", lambda: (hw.rear_set("breathing", "3723c7", 3, 100, 1), "stock look")[1])
    elif r == "off": step("rear", lambda: (hw.rear_set("static", "00aec7", 3, 0, 1), "off")[1])
    return done
