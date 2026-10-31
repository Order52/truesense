"""Configuration: built-in defaults, overridden by /etc/truesense/config.toml (system) and ~/.config/truesense/config.toml (the user's own).

Everything is optional; a missing file or key falls back to the default. `write_user()` updates the user's file (used by `truesense keys set ...` etc.).
"""
import copy, os, tomllib

SYSTEM_PATH = "/etc/truesense/config.toml"
USER_PATH = os.path.expanduser("~/.config/truesense/config.toml")

DEFAULTS = {
    "user": "",
    "daemon": {"smart_fan": True, "auto_profile": True},
    "fan": {
        "levels": [30, 40, 50, 60, 70, 85, 100],          # fan %
        "cpu_up": [52, 60, 68, 75, 80, 85, 89],           # deg C at which each level is entered
        "gpu_up": [48, 55, 61, 66, 70, 74, 79],
        "hysteresis": 4, "down_hold": 8, "predict": 5, "battery_shift": 3, "couple": 20,
        "hot_cpu": 92, "hot_gpu": 82, "calm_cpu": 58, "calm_gpu": 55, "calm_hold": 20,
    },
    "cpu": {"pl_normal_w": 80, "pl_gaming_w": 65, "gov_hi": 88, "gov_lo": 83, "gov_enabled": True,
            "cap_max_khz": 5_000_000, "cap_min_khz": 2_800_000, "cap_down_khz": 200_000, "cap_up_khz": 100_000},
    "native": {"ac": "balanced", "battery": "eco"},                      # "native auto", like PredatorSense on Windows: follows the power source only
    "smart": {"ac_idle": "balanced", "ac_browsing": "balanced", "ac_busy": "performance", "ac_turbo": True, "turbo_after_s": 30, "turbo_max_cpu": 85, "turbo_max_gpu": 75,
              "backoff_hold_s": 90, "idle_after_s": 60, "battery_idle": "eco", "battery_browsing": "eco", "battery_busy": "balanced", "battery_hot": "quiet", "battery_low_pct": 20,
              "up_dwell_s": 5, "down_dwell_s": 30},
    "profile": {"osd": True, "hold_minutes": 30,
                "auto": {"ac_busy": "turbo", "ac_normal": "balanced", "battery_busy": "balanced", "battery_idle": "eco"}},
    "keys": {"profile": "default", "tap": "power-next", "double": "output-toggle", "triple": "none", "long": "fan-max-toggle", "fn_esc": "none",
             "long_ms": 800, "multi_ms": 450, "power_cycle": ["eco", "quiet", "balanced", "performance", "turbo"]},
    "startup": {"profile": "", "keyboard": "last", "rear": "last"},   # applied at login: last | default | none | profile:NAME (keyboard) | off (rear)
    "keyboard": {"operating_follow": False, "operating_colors": {}},
    "audio": {"jack": "auto", "notify": True, "headset_mode": "conference", "array_mode": "conference"},
    "osd": {"style": "native"},                      # native | noctalia | none  (env TRUESENSE_OSD overrides)
    "compat": {"predator_widgets": False},           # also write /tmp/predator-state and honour ~/.cache/predator-fan-mode (old status-bar widgets)
}


def merge(base, over):
    for k, v in over.items():
        base[k] = merge(copy.deepcopy(base[k]), v) if isinstance(v, dict) and isinstance(base.get(k), dict) else v
    return base


def _read(path):
    try:
        with open(path, "rb") as f: return tomllib.load(f)
    except FileNotFoundError: return {}
    except tomllib.TOMLDecodeError as e: raise ValueError(f"{path}: {e}")


def load(system=SYSTEM_PATH, user=USER_PATH):
    cfg = merge(copy.deepcopy(DEFAULTS), _read(system))
    if user: cfg = merge(cfg, _read(user))
    cfg["user"] = cfg["user"] or os.environ.get("SUDO_USER") or os.environ.get("USER") or ""
    return cfg


def validate(cfg):
    """Return a list of problems (empty = fine). Catches typos that would otherwise silently fall back."""
    out = []
    f = cfg["fan"]
    if not (len(f["levels"]) == len(f["cpu_up"]) == len(f["gpu_up"])): out.append("[fan] levels, cpu_up and gpu_up must have the same length")
    if any(not 20 <= x <= 100 for x in f["levels"]): out.append("[fan] levels must be 20..100")
    if f["cpu_up"] != sorted(f["cpu_up"]) or f["gpu_up"] != sorted(f["gpu_up"]): out.append("[fan] cpu_up / gpu_up must be ascending")
    c = cfg["cpu"]
    if not 25 <= c["pl_normal_w"] <= 120 or not 25 <= c["pl_gaming_w"] <= 120: out.append("[cpu] power limits must be 25..120 W")
    if cfg["audio"]["jack"] not in ("auto", "headphones", "headset"): out.append("[audio] jack must be auto, headphones or headset")
    s = cfg["startup"]
    modes = ("eco", "quiet", "balanced", "performance", "turbo")
    for k, v in cfg["smart"].items():
        if k.startswith(("ac_", "battery_")) and isinstance(v, str) and v not in modes: out.append(f"[smart] {k} must be one of {', '.join(modes)}")
    for k, v in cfg["native"].items():
        if v not in modes: out.append(f"[native] {k} must be one of {', '.join(modes)}")
    if not isinstance(s["keyboard"], str) or not (s["keyboard"] in ("last", "default", "none", "off") or s["keyboard"].startswith(("profile:", "look:"))): out.append("[startup] keyboard must be last, default, none, off, profile:NAME or look:NAME")
    if not isinstance(s["rear"], str) or not (s["rear"] in ("last", "default", "none", "off") or s["rear"].startswith("look:")): out.append("[startup] rear must be last, default, none, off or look:NAME")
    if cfg["osd"]["style"] not in ("native", "noctalia", "none"): out.append("[osd] style must be native, noctalia or none")
    return out


# ---- minimal TOML writer (the standard library only reads TOML) ----------------------------------------------------------------------
def _val(v):
    if isinstance(v, bool): return "true" if v else "false"
    if isinstance(v, (int, float)): return repr(v)
    if isinstance(v, str): return '"' + v.replace("\\", "\\\\").replace('"', '\\"') + '"'
    if isinstance(v, (list, tuple)): return "[" + ", ".join(_val(x) for x in v) + "]"
    raise TypeError(f"cannot write {type(v).__name__} to TOML")


def dumps(d, _prefix=""):
    """Dict of scalars / lists / nested dicts -> TOML text."""
    lines = [f"{k} = {_val(v)}" for k, v in d.items() if not isinstance(v, dict)]
    out = "\n".join(lines) + ("\n" if lines else "")
    for k, v in d.items():
        if isinstance(v, dict):
            name = f"{_prefix}{k}"
            body = dumps(v, name + ".")
            out += f"\n[{name}]\n" + body if any(not isinstance(x, dict) for x in v.values()) else body
    return out


def write_user(updates, path=USER_PATH):
    """Merge `updates` (nested dict) into the user's config file and write it back atomically."""
    cur = _read(path); merge(cur, updates)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w") as f: f.write(dumps(cur))
    os.replace(tmp, path)
    return cur
