"""Configuration: built-in defaults (tuned on the PH16-71 by the previous predator-daemon), overridden by /etc/truesense/config.toml."""
import os, tomllib

DEFAULTS = {
    "user": os.environ.get("RPS_USER") or os.environ.get("SUDO_USER") or os.environ.get("USER") or "",   # owner of the compat files and the OSD session (install.sh writes it to /etc/truesense/config.toml)
    "fan": {
        "levels": [30, 40, 50, 60, 70, 85, 100],          # fan %
        "cpu_up": [52, 60, 68, 75, 80, 85, 89],           # deg C at which each level is entered
        "gpu_up": [48, 55, 61, 66, 70, 74, 79],
        "hysteresis": 4, "down_hold": 8, "predict": 5, "battery_shift": 3, "couple": 20,
        "hot_cpu": 92, "hot_gpu": 82, "calm_cpu": 58, "calm_gpu": 55, "calm_hold": 20,
    },
    "cpu": {"pl_normal_w": 80, "pl_gaming_w": 65, "gov_hi": 88, "gov_lo": 83,
            "cap_max_khz": 5_000_000, "cap_min_khz": 2_800_000, "cap_down_khz": 200_000, "cap_up_khz": 100_000},
    "profile": {"auto": True, "osd": True},
    "keyboard": {"operating_follow": False, "operating_colors": {}},     # opt-in: keyboard colour follows the power mode (replaces the per-key lighting)
}


def _merge(a, b):
    for k, v in b.items():
        a[k] = _merge(dict(a[k]), v) if isinstance(v, dict) and isinstance(a.get(k), dict) else v
    return a


def load(path="/etc/truesense/config.toml"):
    cfg = _merge({k: (dict(v) if isinstance(v, dict) else v) for k, v in DEFAULTS.items()}, {})
    try:
        with open(path, "rb") as f:
            cfg = _merge(cfg, tomllib.load(f))
    except FileNotFoundError:
        pass
    return cfg
