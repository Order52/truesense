"""NVIDIA GPU undervolt through NVML (the same interface `nvidia-smi` uses): a clock-curve offset plus an optional cap on the maximum clock.

Linux drivers do not expose GPU voltage directly. Shifting the voltage/frequency curve UP by an offset (so each clock needs less voltage) and capping the maximum
clock is how an undervolt is done here: the GPU reaches its cap at a lower voltage. Needs root (the TrueSense service). Neither setting survives a reboot.
Built-in profiles are read-only (copy one to make your own); applying always needs confirmation; "apply at boot" is opt-in and crash-guarded (see daemon).
Values are conservative starting points for the RTX 4060 Laptop, NOT validated on every unit.
"""
import ctypes, ctypes.util, json, os, re
from .device import HwError

DIR = os.path.expanduser("~/.config/truesense/gpu-undervolt")
OFFSET_RANGE = (-300, 250)          # MHz of curve shift; the driver reports its own limit too (about +-1000)
CAP_RANGE = (600, 3300)             # MHz, or 0 = no cap
BUILTIN = {
    "stock": {"description": "No offset and no clock cap", "offset": 0, "cap": 0},
    "truesense": {"description": "Our profile for the RTX 4060 Laptop: a moderate curve shift with a clock cap, aiming for less power and heat at about the same speed. Unverified on your unit: test under load", "offset": 120, "cap": 2400},
    "mild": {"description": "Very safe: small shift, high cap", "offset": 60, "cap": 2500},
    "cool": {"description": "Cooler and quieter: lower cap, bigger shift", "offset": 100, "cap": 2100},
    "efficient": {"description": "Lowest power for light gaming: some games will run slower", "offset": 150, "cap": 1800},
}
_ERR = {2: "invalid argument", 3: "not supported by this GPU or driver", 4: "needs root (the TrueSense service)", 9: "driver not loaded", 15: "GPU is lost", 999: "unknown error"}


def validate(p):
    if not isinstance(p, dict): raise HwError("a GPU undervolt profile has 'offset' (MHz) and 'cap' (MHz, 0 = none)")
    out = {"description": p.get("description", ""), "offset": p.get("offset"), "cap": p.get("cap", 0)}
    o, c = out["offset"], out["cap"]
    if not isinstance(o, (int, float)) or isinstance(o, bool) or not OFFSET_RANGE[0] <= o <= OFFSET_RANGE[1]: raise HwError(f"offset: {OFFSET_RANGE[0]}..{OFFSET_RANGE[1]} MHz")
    if not isinstance(c, (int, float)) or isinstance(c, bool) or not (c == 0 or CAP_RANGE[0] <= c <= CAP_RANGE[1]): raise HwError(f"cap: 0 (none) or {CAP_RANGE[0]}..{CAP_RANGE[1]} MHz")
    out["offset"], out["cap"] = int(o), int(c); return out


# ---- NVML (root) ------------------------------------------------------------------------------------------------------------------------
_lib = _dev = None

def _nvml():
    global _lib, _dev
    if _lib: return _lib, _dev
    path = ctypes.util.find_library("nvidia-ml") or "libnvidia-ml.so.1"
    try: lib = ctypes.CDLL(path)
    except OSError: raise HwError("NVIDIA driver library (libnvidia-ml) not found")
    if lib.nvmlInit_v2(): raise HwError("could not start NVML (is the NVIDIA driver loaded?)")
    dev = ctypes.c_void_p()
    if lib.nvmlDeviceGetHandleByIndex_v2(0, ctypes.byref(dev)): raise HwError("no NVIDIA GPU found")
    _lib, _dev = lib, dev; return lib, dev


def _check(r, what):
    if r: raise HwError(f"{what}: {_ERR.get(r, f'NVML error {r}')}")


def read():
    """Current curve offset and its allowed range (a harmless read)."""
    lib, dev = _nvml(); off = ctypes.c_int(); _check(lib.nvmlDeviceGetGpcClkVfOffset(dev, ctypes.byref(off)), "reading the GPU offset")
    mn, mx = ctypes.c_int(), ctypes.c_int(); lib.nvmlDeviceGetGpcClkMinMaxVfOffset(dev, ctypes.byref(mn), ctypes.byref(mx))
    return {"offset_mhz": off.value, "range_mhz": [mn.value, mx.value]}


def apply(offset, cap):
    lib, dev = _nvml(); lo, hi = read()["range_mhz"]
    if hi and not lo <= offset <= hi: raise HwError(f"the driver allows offsets {lo}..{hi} MHz")
    _check(lib.nvmlDeviceSetGpcClkVfOffset(dev, ctypes.c_int(offset)), "setting the GPU offset")
    if cap: _check(lib.nvmlDeviceSetGpuLockedClocks(dev, ctypes.c_uint(210), ctypes.c_uint(cap)), "capping the GPU clock")
    else: lib.nvmlDeviceResetGpuLockedClocks(dev)
    got = read()["offset_mhz"]
    if got != offset: raise HwError(f"the GPU kept offset {got} MHz instead of {offset} (driver limit?)")
    return {"offset_mhz": got, "cap_mhz": cap}


def reset():
    lib, dev = _nvml(); _check(lib.nvmlDeviceSetGpcClkVfOffset(dev, ctypes.c_int(0)), "resetting the GPU offset"); lib.nvmlDeviceResetGpuLockedClocks(dev)
    return {"offset_mhz": read()["offset_mhz"], "cap_mhz": 0}


# ---- profiles -----------------------------------------------------------------------------------------------------------------------------
def _path(name):
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,40}", name or ""): raise HwError("profile names use letters, digits, . _ - (max 40)")
    return os.path.join(DIR, name + ".json")

def names(): return list(BUILTIN) + sorted(f[:-5] for f in (os.listdir(DIR) if os.path.isdir(DIR) else []) if f.endswith(".json") and f[:-5] not in BUILTIN)

def load(name):
    if name in BUILTIN: return validate(BUILTIN[name])
    try:
        with open(_path(name)) as f: return validate(json.load(f))
    except FileNotFoundError: raise HwError(f"no GPU undervolt profile '{name}' (truesense gpu undervolt list)")
    except ValueError: raise HwError(f"GPU undervolt profile '{name}' is not valid JSON")

def save(name, profile):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and read-only; copy it (gpu undervolt copy {name}) and edit the copy")
    p = validate(profile); os.makedirs(DIR, exist_ok=True)
    with open(_path(name) + ".tmp", "w") as f: json.dump(p, f, indent=1)
    os.replace(_path(name) + ".tmp", _path(name)); return p

def delete(name):
    if name in BUILTIN: raise HwError(f"'{name}' is built in and cannot be deleted")
    try: os.remove(_path(name))
    except FileNotFoundError: raise HwError(f"no GPU undervolt profile '{name}'")

def copy(name, new=None):
    base = new or name + "-copy"; n, target = 2, base
    while target in names(): target = f"{base}-{n}"; n += 1
    save(target, load(name)); return target


SAFE_OFFSET = 150          # MHz of curve shift: up to here is usually fine with a clock cap
DANGER_OFFSET = 200

def risk(offset, cap):
    """("safe" | "caution" | "danger", message) for a GPU offset and clock cap."""
    if offset > DANGER_OFFSET: return "danger", f"Beyond the danger line (+{DANGER_OFFSET} MHz). Games and the driver can crash; the screen can glitch."
    if offset > SAFE_OFFSET: return "caution", f"Past the safety line (+{SAFE_OFFSET} MHz). It may crash in some games: test under load."
    if offset > 0 and not cap: return "caution", "A positive offset without a clock cap can push the clock above stock: set a cap (2400 MHz is typical) or keep the offset small."
    if offset < -100: return "caution", "A large negative offset only slows the GPU; it does not save voltage the way a positive offset with a cap does."
    if cap and cap < 1500: return "caution", "A cap this low will cost a lot of speed in games."
    return ("safe", "Inside the safe range.") if (offset or cap) else ("safe", "Stock.")
