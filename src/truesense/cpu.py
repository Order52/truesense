"""CPU/GPU power: RAPL PL1/PL2 (powercap sysfs), NVIDIA power limit, and a GUARDED undervolt interface (MSR 0x150).

Safety: limits are range-checked; the undervolt is read-only unless --confirm is given, starts at -10 mV steps, refuses beyond -120 mV,
and `uv reset` writes 0. Nothing here runs automatically; the daemon only touches PL1/PL2 (config [cpu] pl_*_w).
"""
import glob, os, struct, subprocess
from . import util
from .backend import HwError

RAPL = "/sys/class/powercap/intel-rapl:0/"
PL_MIN_W, PL_MAX_W = 25, 120          # this 13700HX laptop: stock 80 W; refuse anything outside

def _rd(p):
    try: return util.read(p).strip()
    except OSError: return None

def info():
    out = {"pl1_w": None, "pl2_w": None, "rapl_enabled": _rd(RAPL + "enabled"), "msr_lock_bit": None, "nvidia_power_limit_w": None}
    for k, n in (("pl1_w", "constraint_0_power_limit_uw"), ("pl2_w", "constraint_1_power_limit_uw")):
        v = _rd(RAPL + n); out[k] = int(v) / 1e6 if v else None
    try:
        fd = os.open("/dev/cpu/0/msr", os.O_RDONLY); os.lseek(fd, 0x610, 0); v = struct.unpack("<Q", os.read(fd, 8))[0]; os.close(fd)
        out["msr_lock_bit"] = bool(v >> 63)
    except OSError: out["msr_lock_bit"] = "unreadable (needs root + msr module)"
    try:
        r = subprocess.run(["nvidia-smi", "--query-gpu=power.limit,power.default_limit,power.min_limit,power.max_limit", "--format=csv,noheader,nounits"], capture_output=True, text=True, timeout=5).stdout.split(",")
        out["nvidia_power_limit_w"] = {"current": r[0].strip(), "default": r[1].strip(), "min": r[2].strip(), "max": r[3].strip()}
    except Exception: pass
    return out

def set_limits(pl1, pl2):
    if not (PL_MIN_W <= pl1 <= PL_MAX_W and PL_MIN_W <= pl2 <= PL_MAX_W and pl2 >= pl1): raise HwError(f"PL1/PL2 must be {PL_MIN_W}..{PL_MAX_W} W with PL2 >= PL1")
    old = (_rd(RAPL + "constraint_0_power_limit_uw"), _rd(RAPL + "constraint_1_power_limit_uw"))
    try:
        for n, w in (("constraint_0_power_limit_uw", pl1), ("constraint_1_power_limit_uw", pl2)): util.write(RAPL + n, str(int(w * 1e6)))
    except OSError as e: raise HwError(f"cannot write RAPL (needs root, or the daemon): {e}")
    got = (int(_rd(RAPL + "constraint_0_power_limit_uw")) / 1e6, int(_rd(RAPL + "constraint_1_power_limit_uw")) / 1e6)
    if got != (pl1, pl2):
        for n, v in zip(("constraint_0_power_limit_uw", "constraint_1_power_limit_uw"), old): util.write(RAPL + n, v)
        raise HwError(f"readback {got} != requested; restored previous limits")
    return {"pl1_w": got[0], "pl2_w": got[1]}

def set_nvidia_limit(w):
    r = subprocess.run(["nvidia-smi", "-pl", str(int(w))], capture_output=True, text=True)
    if r.returncode: raise HwError((r.stdout + r.stderr).strip())
    return r.stdout.strip()

# ---- undervolt (MSR 0x150 mailbox): core plane 0, cache plane 2 ----------------------------------------------------------------
UV_MAX_MV = 120

def _msr(op, val=None):
    fd = os.open("/dev/cpu/0/msr", os.O_RDWR)
    try:
        if val is None: os.lseek(fd, op, 0); return struct.unpack("<Q", os.read(fd, 8))[0]
        os.lseek(fd, op, 0); os.write(fd, struct.pack("<Q", val))
    finally: os.close(fd)

def _uv_cmd(plane, write, offset_mv=0):
    off = 0
    if write: off = (round(offset_mv * 1.024) & 0xFFF) << 21
    return (1 << 63) | (plane << 40) | ((0x11 if write else 0x10) << 32) | off

def uv_get(plane=0):
    """Reads the current offset (a mailbox READ request: harmless). Needs root and the msr module."""
    try: _msr(0x150, _uv_cmd(plane, False)); v = _msr(0x150)
    except OSError as e: raise HwError(f"MSR 0x150 not accessible: {e} (sudo modprobe msr; run as root)")
    raw = (v >> 21) & 0x7FF; raw -= 0x800 if raw & 0x400 else 0
    return round(raw / 1.024)

def uv_set(plane, offset_mv, confirm=False):
    if not confirm: raise HwError("undervolt needs explicit --confirm (can crash the machine if too aggressive)")
    if not (0 <= -offset_mv <= UV_MAX_MV): raise HwError(f"offset must be between 0 and -{UV_MAX_MV} mV (start with -10)")
    try: _msr(0x150, _uv_cmd(plane, True, offset_mv))
    except OSError as e: raise HwError(f"MSR write refused: {e} (BIOS may lock undervolting)")
    got = uv_get(plane)
    if abs(got - offset_mv) > 2: raise HwError(f"readback {got} mV != {offset_mv} mV (BIOS lock?); reset with: uv reset")
    return got

def uv_reset():
    for p in (0, 2):
        try: _msr(0x150, _uv_cmd(p, True, 0))
        except OSError: pass
    return {"core_mv": uv_get(0), "cache_mv": uv_get(2)}
