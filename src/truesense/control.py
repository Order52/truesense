"""Fast paths shared by the CLI, key service, profiles and window: fans and power mode go through the daemon in ONE request, so there is no
settle delay (the daemon changes its own fan control and writes the hardware under one lock). Without a daemon they write directly."""
import time
from . import backend as hw, ipc
from .device import HwError


def _legacy_fan(mode, cpu, gpu):
    """Daemon too old to know `fan`: stop its smart curve, wait one tick, then write."""
    if mode == "smart": ipc.call("set", key="smart_fan", value=True); return
    try: ipc.call("set", key="smart_fan", value=False); time.sleep(1.2)
    except HwError: pass
    _direct_fan(mode, cpu, gpu)


def _direct_fan(mode, cpu, gpu):
    if mode == "auto": hw.fan_auto()
    elif mode == "max": hw.fan_max()
    elif mode == "manual": hw.fan_manual(cpu, gpu if gpu is not None else cpu)
    elif mode == "off": raise HwError("turning the fans off needs the TrueSense service")
    else: raise HwError("the smart curve needs the TrueSense daemon")


def fan(mode, cpu=None, gpu=None, confirm=False):
    """mode: smart | auto | max | manual (cpu/gpu = 20..100 %) | off (both fans stopped: needs confirm and the service's temperature watchdog)."""
    if mode == "off":
        if not ipc.available(): raise HwError("turning the fans off needs the TrueSense service (it brings them back when it gets warm)")
        return ipc.call("fan", mode="off", confirm=confirm)
    if mode == "manual" and (cpu is None or not 20 <= cpu <= 100 or (gpu is not None and not 20 <= gpu <= 100)): raise HwError("manual fan duty is 20..100 %")
    if not ipc.available(): return _direct_fan(mode, cpu, gpu)
    try: return ipc.call("fan", mode=mode, cpu=cpu, gpu=gpu)
    except HwError as e:
        if "unknown command" in str(e): return _legacy_fan(mode, cpu, gpu)
        raise


def power(mode, hold_minutes=None):
    """Set the power mode and (optionally) hold it against the automatic profile."""
    if ipc.available():
        try: return ipc.call("power", mode=mode, hold_minutes=hold_minutes)
        except HwError as e:
            if "unknown command" not in str(e): raise
    new = hw.set_mode(mode)
    if hold_minutes:
        try: ipc.call("hold", minutes=hold_minutes)
        except HwError: pass
    return new
