# SPDX-License-Identifier: MIT
"""Game rules: a program name -> the power mode to use while it runs ("hl2_linux": "performance", "eldenring.exe": "turbo").

The service looks at the running programs every few seconds. A matching program raises the automatic power mode (Native and Smart auto both honour it); when the program ends
the automatic profile takes over again by itself. Names match the process name (as `ps -e` shows it, 15 characters), the program file's name, or - for Wine/Proton games - any
`.exe` on the command line. Case does not matter. Turbo still backs off to Performance if the laptop overheats, and Eco is forced when the battery is low.
"""
import os
import re
from .device import HwError

MODES = ("eco", "quiet", "balanced", "performance", "turbo")
MAX_RULES = 64


def clean_name(n):
    n = (n or "").strip().lower()
    if not re.fullmatch(r"[a-z0-9._+ -]{1,80}", n): raise HwError("program name: letters, digits, . _ + - and spaces (max 80)")
    return n


def validate(rules):
    if not isinstance(rules, dict): raise HwError("game rules are a name -> mode map")
    out = {}
    for k, m in rules.items():
        if m not in MODES: raise HwError(f"{k}: mode must be one of {', '.join(MODES)}")
        out[clean_name(k)] = m
    if len(out) > MAX_RULES: raise HwError(f"at most {MAX_RULES} rules")
    return out


def names_of(pid, proc="/proc"):
    """The lower-case names one process answers to."""
    res = set()
    try:
        with open(f"{proc}/{pid}/comm", "rb") as f: res.add(f.read().decode("utf-8", "replace").strip().lower())
        with open(f"{proc}/{pid}/cmdline", "rb") as f: args = [a.decode("utf-8", "replace") for a in f.read(4096).split(b"\0") if a]
    except OSError: return res
    for i, a in enumerate(args[:12]):
        b = a.replace("\\", "/").rsplit("/", 1)[-1].lower()
        if i == 0 or b.endswith(".exe"): res.add(b); res.add(b[:15])
    return res


def running(rules, proc="/proc"):
    """The mode of the highest-ranked rule whose program is running now, or None."""
    if not rules: return None
    want = dict(rules); best = None
    try: pids = [p for p in os.listdir(proc) if p.isdigit()]
    except OSError: return None
    for pid in pids:
        for n in names_of(pid, proc):
            m = want.get(n) or want.get(n[:15])
            if m and (best is None or MODES.index(m) > MODES.index(best)): best = m
    return best
