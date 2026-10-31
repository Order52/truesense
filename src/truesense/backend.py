"""Hardware features of the Acer Predator Helios 16 (PH16-71), one function per feature.

Everything goes through the kernel helper (device.py). Every setter reads the old value, writes, and reads back (retrying for up to 1 s, because the firmware
can answer -1 / the old value right after a write); set_* return the verified new value or raise HwError.
"""
import glob, os, re, subprocess, time
from . import util
from .device import HwError, cmd, ival

MODES = {"quiet": 0, "balanced": 1, "performance": 4, "turbo": 5, "eco": 6}
MODE_NAME = {v: k for k, v in MODES.items()}
GPU_MODES = {"optimus": 1, "nvidia": 2, "auto": 3}
GPU_NAME = {v: k for k, v in GPU_MODES.items()}
USB_LEVELS = (0, 10, 20, 30)
REAR_MODES = {"static": 0, "breathing": 1, "neon": 2, "wave": 3, "twinkling": 7}
FAN_AUTO_BEHAVIOR = 0x41


def settle(getter, want, what, tries=10, delay=0.1):
    """Read back until the firmware reports the new value; fail only after ~1 s."""
    got = None
    for _ in range(tries):
        try: got = getter()
        except (HwError, ValueError): got = None
        if got == want: return want
        time.sleep(delay)
    raise HwError(f"{what} readback mismatch (wanted {want!r}, last read {got!r})")


def version(): return cmd("version")["version"]
def capabilities(): d = cmd("caps"); return {k: v == "1" for k, v in d.items()}


# ---- power mode ----------------------------------------------------------------------------------------------------------------------
def get_mode():
    n = ival(cmd("get_mode"), "mode"); return MODE_NAME.get(n, f"unknown({n})")

def set_mode(name):
    if name not in MODES: raise HwError(f"power mode must be one of: {', '.join(MODES)}")
    cmd(f"set_mode {MODES[name]}"); return settle(get_mode, name, "power mode")

def supported_modes():
    mask = ival(cmd("get_modes"), "mask"); return [n for n, v in MODES.items() if mask >> v & 1]


# ---- sensors and fans ----------------------------------------------------------------------------------------------------------------
def sensors():
    """One call returns everything: temperatures in C and fan RPM."""
    d = cmd("sensors")
    return {"cpu_temp": ival(d, "cpu"), "sys_temp": ival(d, "sys"), "gpu_temp": ival(d, "gpu"), "cpu_fan_rpm": ival(d, "cpu_rpm"), "gpu_fan_rpm": ival(d, "gpu_rpm")}

def fan_state():
    d = cmd("get_fan"); beh = ival(d, "behavior")
    return {"behavior_raw": f"0x{beh:x}", "mode": {0x41: "auto", 0x82: "max", 0xC3: "manual"}.get(beh, "unknown"),
            "duty_register_pct": {"cpu": ival(d, "cpu"), "gpu": ival(d, "gpu")}}

def fan_auto():
    cmd("set_fan_auto"); settle(lambda: fan_state()["mode"], "auto", "fan auto")

def fan_max():
    cmd("set_fan_max"); settle(lambda: fan_state()["mode"], "max", "fan max")

def fan_manual(cpu, gpu):
    if not (20 <= cpu <= 100 and 20 <= gpu <= 100): raise HwError("manual duty must be 20..100 %")
    cmd(f"set_fan_manual {cpu} {gpu}"); settle(lambda: fan_state()["mode"], "manual", "fan manual")


# ---- on/off firmware settings --------------------------------------------------------------------------------------------------------
def get_overdrive(): v = ival(cmd("get_overdrive"), "overdrive"); return None if v < 0 else bool(v)
def set_overdrive(on): cmd(f"set_overdrive {int(bool(on))}"); return settle(get_overdrive, bool(on), "overdrive")

def get_boot_sound(): return bool(ival(cmd("get_boot"), "boot"))
def set_boot_sound(on): cmd(f"set_boot {int(bool(on))}"); return settle(get_boot_sound, bool(on), "boot sound")

def get_kb_timeout(): v = ival(cmd("get_kbtimeout"), "kbtimeout"); return None if v < 0 else bool(v)
def set_kb_timeout(on): cmd(f"set_kbtimeout {int(bool(on))}"); return settle(get_kb_timeout, bool(on), "backlight timeout")

def get_usb_charge(): return ival(cmd("get_usb"), "usb")
def set_usb_charge(level):
    if level not in USB_LEVELS: raise HwError("usb-charge level: 0 (off), 10, 20 or 30")
    cmd(f"set_usb {level}"); return settle(get_usb_charge, level, "usb charging")

def get_battery_limit(): return bool(ival(cmd("get_battery 1"), "battery"))
def set_battery_limit(on): cmd(f"set_battery 1 {int(bool(on))}"); return settle(get_battery_limit, bool(on), "battery limit")

def get_calibration(): return bool(ival(cmd("get_battery 2"), "battery"))
def set_calibration(on):
    """Starts/stops the firmware battery calibration (needs AC power; a full cycle takes hours)."""
    cmd(f"set_battery 2 {int(bool(on))}"); return settle(get_calibration, bool(on), "battery calibration")


# ---- GPU mode (MUX; takes effect after a reboot) -------------------------------------------------------------------------------------
def get_gpu_mode(): n = ival(cmd("get_gpu"), "gpu"); return GPU_NAME.get(n, f"unknown({n})")
def set_gpu_mode(name):
    if name not in GPU_MODES: raise HwError("gpu mode: optimus | nvidia | auto (applies after reboot)")
    cmd(f"set_gpu {GPU_MODES[name]}"); return settle(get_gpu_mode, name, "gpu mode")


# ---- rear light bar --------------------------------------------------------------------------------------------------------------------
def rear_get():
    r = bytes.fromhex(cmd("get_rear")["raw"]); b = r[1:]      # byte 0 is a status byte, then the 16-byte layout of set_rear
    return {"raw": r.hex(), "mode": next((n for n, v in REAR_MODES.items() if v == b[0]), b[0]), "speed": b[1], "brightness": b[2], "direction": b[4],
            "color": "%02x%02x%02x" % tuple(b[5:8])}

def rear_set(mode, color="ffffff", speed=3, brightness=100, direction=1):
    if mode not in REAR_MODES: raise HwError(f"rear mode: {', '.join(REAR_MODES)}")
    if not re.fullmatch(r"[0-9a-fA-F]{6}", color): raise HwError("color must be RRGGBB hex")
    if not (1 <= speed <= 5 and 0 <= brightness <= 100 and direction in (0, 1)): raise HwError("speed 1..5, brightness 0..100, direction 0|1")
    pkt = bytes([REAR_MODES[mode], speed, brightness, 0, direction]) + bytes.fromhex(color) + bytes([3, 2]) + bytes(6)
    cmd(f"set_rear {pkt.hex()}")
    if mode == "static": cmd(f"set_color {color.lower()}")      # the static colour is a separate call; mode 0 ignores the colour bytes above
    got = settle(lambda: (lambda g: (g["mode"], g["brightness"]))(rear_get()), (mode, brightness), "rear light")
    return rear_get()


# ---- plain Linux sensors (CPU package temp, GPU) ---------------------------------------------------------------------------------------
def cpu_temp():
    best = None
    for h in glob.glob("/sys/class/hwmon/hwmon*"):
        try:
            if util.read(h + "/name").strip() != "coretemp": continue
        except OSError: continue
        for t in glob.glob(h + "/temp*_input"):
            try: v = int(util.read(t)) / 1000
            except OSError: continue
            best = v if best is None else max(best, v)
    return best

def gpu_temp():
    try:
        out = subprocess.run(["nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits"], capture_output=True, text=True, timeout=3).stdout
        return float(out.split()[0])
    except Exception:
        return None

def on_ac():
    for n in ("ACAD", "AC", "AC0", "ADP0", "ADP1"):
        try: return util.read(f"/sys/class/power_supply/{n}/online").strip() == "1"
        except OSError: continue
    return True
