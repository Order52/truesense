"""`truesense selftest`: set -> read back -> restore for every firmware feature; prints PASS/FAIL. Fans, GPU mode and battery limit are restored in finally blocks."""
import os, subprocess, time
from . import backend as hw, daemon_ctl


def _check(name, fn):
    try:
        detail = fn(); print(f"PASS  {name}" + (f"  ({detail})" if detail else "")); return True
    except Exception as e:
        print(f"FAIL  {name}: {e}"); return False


def _toggle(get, set_, alt):
    orig = get(); new = alt(orig)
    try:
        set_(new); assert get() == new, "readback mismatch"; return f"{orig} -> {new} -> restored"
    finally:
        set_(orig); assert get() == orig, "RESTORE FAILED"


def run(skip_fans=False):
    res = []
    def mode():
        orig = hw.get_mode(); seen = []
        try:
            for m in hw.supported_modes(): seen.append(hw.set_mode(m))
        finally: hw.set_mode(orig)
        return "cycled " + ",".join(seen) + f", restored {orig}"
    with daemon_ctl.hands_off(): res.append(_check("power mode (all supported)", mode))
    def sens():
        s = hw.sensors(); assert 10 < s["cpu_temp"] < 110 and 10 < s["gpu_temp"] < 110, s; return str(s)
    res.append(_check("sensors via firmware", sens))
    def fans():
        try:
            hw.fan_manual(60, 60); ok = n = 0; t0 = time.monotonic()
            while time.monotonic() - t0 < 25:                 # ONE write, no refresh
                time.sleep(1)
                if time.monotonic() - t0 > 8:
                    s = hw.sensors(); n += 1; ok += s["cpu_fan_rpm"] > 2000 and s["gpu_fan_rpm"] > 2000
            pct = 100 * ok // n
            assert pct >= 85, f"fans held 60% in only {pct}% of samples"
            return f"one write held both fans >2000 rpm in {pct}% of samples for 25 s"
        finally:
            hw.fan_auto()
            assert hw.fan_state()["behavior_raw"] == "0x41", "fans not back on Auto"
    other = hw.other_fan_controllers()
    if other and not skip_fans:
        print(f"SKIP  fan manual test: {', '.join(other)} is active and rewrites the fans (stop it first: sudo systemctl stop {other[0]})")
    elif not skip_fans:
        with daemon_ctl.hands_off(): res.append(_check("fan manual 60% (one write must hold) + restore Auto", fans))
    res.append(_check("lcd overdrive", lambda: _toggle(hw.get_overdrive, hw.set_overdrive, lambda v: not v)))
    res.append(_check("boot animation/sound", lambda: _toggle(hw.get_boot_sound, hw.set_boot_sound, lambda v: not v)))
    res.append(_check("battery limit 80%", lambda: _toggle(hw.get_battery_limit, hw.set_battery_limit, lambda v: not v)))
    res.append(_check("usb charging level", lambda: _toggle(hw.get_usb_charge, hw.set_usb_charge, lambda v: 10 if v != 10 else 20)))
    res.append(_check("keyboard backlight timeout", lambda: _toggle(hw.get_kb_timeout, hw.set_kb_timeout, lambda v: not v)))
    res.append(_check("gpu mode read", lambda: hw.get_gpu_mode()))
    res.append(_check("rear light read", lambda: str(hw.rear_get())))
    def active(unit, user=False):
        r = subprocess.run(["systemctl"] + (["--user"] if user else []) + ["is-active", unit], capture_output=True, text=True).stdout.strip()
        assert r == "active", f"{unit} is {r}"; return "active"
    res.append(_check("daemon service running", lambda: active("truesense.service")))
    res.append(_check("Fn/PREDATOR key service running", lambda: active("truesense-keys.service", True)))
    def kbd():
        from . import keyboard; b = keyboard.get_brightness(); assert 0 <= b <= 50; return f"brightness {b}/50 read back from the keyboard"
    res.append(_check("keyboard lighting interface", kbd))
    def outp():
        from . import audio; st = audio.output_status(); assert st["active"] in ("speakers", "headphones"); return str(st)
    res.append(_check("audio output profile", outp))
    def voice():
        from . import audio
        if not audio.PLUGIN: raise RuntimeError("RNNoise plugin missing")
        return str(audio.pv_modes())
    res.append(_check("Purified Voice filters configured", voice))
    print(f"\n{sum(res)}/{len(res)} passed"); return all(res)
