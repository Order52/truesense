"""Hardware backend for the Acer Predator Helios 16 (PH16-71).

Two kernel interfaces, both usable without root for members of `plugdev` / `linuwu_sense`:
  /dev/acer-rear                     whitelisted WMI commands (kernel/acer-rear-wmi): power mode, fans, sensors, GPU mode, boot sound, overdrive, rear light
  /sys/devices/platform/acer-wmi/predator_sense/*   Linuwu-Sense: battery limiter/calibration, USB charging, keyboard backlight timeout
Every setter reads the old value, writes, reads back and raises HwError on mismatch (set_* return the verified new value).
"""
import glob, os, re, subprocess, time

DEV = "/dev/acer-rear"
PS = "/sys/devices/platform/acer-wmi/predator_sense"
MODES = {"quiet": 0, "balanced": 1, "performance": 4, "turbo": 5, "eco": 6}
MODE_NAME = {v: k for k, v in MODES.items()}
GPU_MODES = {"optimus": 1, "nvidia": 2, "auto": 3}
GPU_NAME = {v: k for k, v in GPU_MODES.items()}
USB_LEVELS = (0, 10, 20, 30)


class HwError(RuntimeError):
    pass


def _wmi(cmd):
    """Send one whitelisted command, return the reply payload as bytes (b'' if none)."""
    try:
        with open(DEV, "r+b", buffering=0) as f:
            f.write(cmd.encode())
            reply = f.read(256).decode().strip()
    except FileNotFoundError:
        raise HwError(f"{DEV} missing: load acer_rear_wmi (tools/install_kernel_module.sh)")
    except PermissionError:
        raise HwError(f"no permission on {DEV}: add yourself to group plugdev")
    except OSError as e:
        raise HwError(f"'{cmd}' rejected by the driver: {e}")
    if not reply.startswith("ok"):
        raise HwError(f"'{cmd}': {reply}")
    m = re.search(r"buf ([0-9a-f]+)", reply)
    if m:
        return bytes.fromhex(m.group(1))
    m = re.search(r"int 0x([0-9a-f]+)", reply)
    return int(m.group(1), 16).to_bytes(8, "little") if m else b""


def _u64(b):
    return int.from_bytes(b[:8], "little")


def _sys_read(name):
    try:
        return open(f"{PS}/{name}").read().strip()
    except OSError as e:
        raise HwError(f"cannot read {PS}/{name}: {e}")


def _sys_write(name, val):
    try:
        open(f"{PS}/{name}", "w").write(str(val))
    except OSError as e:
        raise HwError(f"cannot write {PS}/{name}: {e}")


def _settle(getter, want, what, tries=10, delay=0.1):
    """Read back until the firmware reports the new value (it can briefly answer -1 / the old value right after a write); fail only after ~1 s."""
    got = None
    for _ in range(tries):
        try: got = getter()
        except (HwError, ValueError): got = None
        if got == want: return want
        time.sleep(delay)
    raise HwError(f"{what} readback mismatch (wanted {want!r}, last read {got!r})")


# ---- power mode -------------------------------------------------------------------------------
def get_mode():
    return MODE_NAME.get(_wmi("get_mode")[1], f"unknown({_wmi('get_mode')[1]})")

def set_mode(name):
    if name not in MODES: raise HwError(f"mode must be one of {', '.join(MODES)}")
    _wmi(f"set_mode {MODES[name]}")
    got = get_mode()
    if got != name: raise HwError(f"mode readback {got} != {name}")
    return got

def supported_modes():
    mask = _wmi("get_modes")[1]
    return [n for n, v in MODES.items() if mask >> v & 1]


# ---- sensors ----------------------------------------------------------------------------------
def _sysinfo(code):
    return (_u64(_wmi(f"get_sysinfo {code:04x}")) >> 8) & 0xFFFFFF

def sensors():
    """Temperatures in C and fan RPM via firmware (GetGamingSysInfo)."""
    return {"cpu_temp": _sysinfo(0x0101) & 0xFF, "sys_temp": _sysinfo(0x0301) & 0xFF, "gpu_temp": _sysinfo(0x0A01) & 0xFF,
            "cpu_fan_rpm": _sysinfo(0x0201) & 0x1FFF, "gpu_fan_rpm": _sysinfo(0x0601) & 0x1FFF}


# ---- fans -------------------------------------------------------------------------------------
def fan_state():
    beh = (_u64(_wmi("get_fan_behavior")) >> 8) & 0xFFFFFF
    spd = {n: (_u64(_wmi(f"get_fan_speed {i}")) >> 8) & 0xFF for n, i in (("cpu", 1), ("gpu", 4))}
    return {"behavior_raw": f"0x{beh:x}", "duty_register_pct": spd}

def fan_auto():
    _wmi("set_fan_auto")

def fan_max():
    _wmi("set_fan_max")

def fan_manual(cpu, gpu):
    if not (20 <= cpu <= 100 and 20 <= gpu <= 100): raise HwError("manual duty must be 20..100 %")
    _wmi(f"set_fan_manual {cpu} {gpu}")


def fan_refresh(cpu, gpu):
    """Speed-only write (experimental, unneeded: a single fan_manual write holds indefinitely)."""
    if not (20 <= cpu <= 100 and 20 <= gpu <= 100): raise HwError("manual duty must be 20..100 %")
    _wmi(f"refresh_fan {cpu} {gpu}")


# ---- GPU / boot / overdrive -------------------------------------------------------------------
def get_gpu_mode():
    v = _wmi("get_gpu")[1]
    return GPU_NAME.get(v, f"unknown({v})")

def set_gpu_mode(name):
    if name not in GPU_MODES: raise HwError("gpu mode: optimus | nvidia | auto (applies after reboot)")
    _wmi(f"set_gpu {GPU_MODES[name]}")
    got = get_gpu_mode()
    if got != name: raise HwError(f"gpu readback {got} != {name}")
    return got

def get_boot_sound():
    return bool(_wmi("get_boot")[1])

def set_boot_sound(on):
    _wmi(f"set_boot {int(bool(on))}")
    if get_boot_sound() != bool(on): raise HwError("boot sound readback mismatch")
    return bool(on)

def get_overdrive():
    return _sys_read("lcd_override") == "1"

def set_overdrive(on):
    _sys_write("lcd_override", int(bool(on))); return _settle(get_overdrive, bool(on), "overdrive")


# ---- battery / USB / backlight (Linuwu sysfs) -------------------------------------------------
def get_battery_limit():
    return _sys_read("battery_limiter") == "1"

def set_battery_limit(on):
    _sys_write("battery_limiter", int(bool(on))); return _settle(get_battery_limit, bool(on), "battery limiter")

def get_usb_charge():
    return int(_sys_read("usb_charging"))

def set_usb_charge(level):
    if level not in USB_LEVELS: raise HwError("usb-charge level: 0 (off) 10 20 30")
    _sys_write("usb_charging", level); return _settle(get_usb_charge, level, "usb charging")

def get_kb_timeout():
    return _sys_read("backlight_timeout") == "1"

def set_kb_timeout(on):
    _sys_write("backlight_timeout", int(bool(on))); return _settle(get_kb_timeout, bool(on), "backlight timeout")

def calibrate_battery(start):
    """Starts/stops the firmware battery calibration (needs AC power; takes hours)."""
    _sys_write("battery_calibration", int(bool(start)))
    return _sys_read("battery_calibration") == "1"


# ---- rear light ---------------------------------------------------------------------------------
REAR_MODES = {"static": 0, "breathing": 1, "neon": 2, "wave": 3, "twinkling": 7}

def rear_get():
    r = _wmi("get_rear")          # byte 0 = status, then the 16-byte layout of set_rear
    b = r[1:]
    return {"raw": r.hex(), "mode": next((n for n, v in REAR_MODES.items() if v == b[0]), b[0]), "speed": b[1], "brightness": b[2],
            "direction": b[4], "color": "%02x%02x%02x" % tuple(b[5:8])}

def rear_set(mode, color="ffffff", speed=3, brightness=100, direction=1):
    if mode not in REAR_MODES: raise HwError(f"rear mode: {', '.join(REAR_MODES)}")
    if not re.fullmatch(r"[0-9a-fA-F]{6}", color): raise HwError("color must be RRGGBB hex")
    pkt = bytes([REAR_MODES[mode], speed, brightness, 0, direction]) + bytes.fromhex(color) + bytes([3, 2]) + bytes(6)
    _wmi(f"set_rear {pkt.hex()}")
    if mode == "static":          # static colour is a separate call (doc 3b); mode 0 ignores the colour bytes above
        _wmi(f"set_color {color.lower()}")
    got = rear_get()
    if got["mode"] != mode or got["brightness"] != brightness: raise HwError(f"rear readback mismatch: {got}")
    return got


# ---- plain Linux sensors --------------------------------------------------------------------------
def cpu_temp():
    best = None
    for h in glob.glob("/sys/class/hwmon/hwmon*"):
        try:
            if open(h + "/name").read().strip() != "coretemp": continue
        except OSError: continue
        for t in glob.glob(h + "/temp*_input"):
            try: v = int(open(t).read()) / 1000
            except OSError: continue
            best = v if best is None else max(best, v)
    return best

def gpu_temp():
    try:
        out = subprocess.run(["nvidia-smi", "--query-gpu=temperature.gpu", "--format=csv,noheader,nounits"], capture_output=True, text=True, timeout=3).stdout
        return float(out.split()[0])
    except Exception:
        return None


def other_fan_controllers():
    """Names of active services that also write the fans (they will fight any manual setting)."""
    out = []
    for u in ("predator.service",):          # the old daemon; rpredatorsense.service is handled via the fan-mode file (see selftest)
        try:
            if subprocess.run(["systemctl", "is-active", "--quiet", u]).returncode == 0: out.append(u)
        except OSError: pass
    return out
