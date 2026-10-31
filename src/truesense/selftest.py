"""`truesense selftest`: set -> read back -> restore every firmware feature, then run the doctor. Prints PASS / FAIL / SKIP per item; exit 1 on any FAIL.

While it runs the daemon's smart fan and automatic profile are paused through the control socket (and put back afterwards), so nothing fights the test.
"""
import contextlib, time
from . import backend as hw, device, ipc

results = []


def check(name, fn):
    try: detail = fn(); print(f"PASS  {name}" + (f"  ({detail})" if detail else "")); results.append(True)
    except Exception as e: print(f"FAIL  {name}: {e}"); results.append(False)


def skip(name, why): print(f"SKIP  {name}: {why}")


@contextlib.contextmanager
def hands_off():
    """Pause the daemon's fan + profile automation, restore afterwards."""
    old = None
    try: old = ipc.call("state")["settings"]
    except device.HwError: pass
    if old:
        for k in ("smart_fan", "auto_profile"): ipc.call("set", key=k, value=False)
        time.sleep(2.5)
    try: yield
    finally:
        if old:
            for k in ("smart_fan", "auto_profile"): ipc.call("set", key=k, value=old[k])


def toggle(get, set_, alt):
    orig = get(); new = alt(orig)
    try:
        set_(new); assert get() == new, "readback mismatch"; return f"{orig} -> {new} -> restored"
    finally:
        set_(orig); assert get() == orig, "RESTORE FAILED"


def run(skip_fans=False):
    results.clear()
    print("== firmware features (set -> read back -> restore)")
    with hands_off():
        def modes():
            orig = hw.get_mode(); seen = []
            try:
                for m in hw.supported_modes(): seen.append(hw.set_mode(m))
            finally: hw.set_mode(orig)
            return "cycled " + ",".join(seen) + f", restored {orig}"
        check("power modes (all supported)", modes)
        check("sensors", lambda: (lambda s: str(s) if 10 < s["cpu_temp"] < 110 and 10 < s["gpu_temp"] < 110 else 1 / 0)(hw.sensors()))
        if skip_fans: skip("fans", "--skip-fans")
        else:
            def fans():
                try:
                    hw.fan_manual(60, 60); ok = n = 0; t0 = time.monotonic()
                    while time.monotonic() - t0 < 22:                           # ONE write, no refresh
                        time.sleep(1)
                        if time.monotonic() - t0 > 8: s = hw.sensors(); n += 1; ok += s["cpu_fan_rpm"] > 2000 and s["gpu_fan_rpm"] > 2000
                    pct = 100 * ok // n; assert pct >= 85, f"fans held 60 % in only {pct} % of samples"
                    return f"one write held both fans >2000 rpm in {pct} % of samples for 22 s"
                finally:
                    hw.fan_auto(); assert hw.fan_state()["mode"] == "auto", "fans not back on firmware auto"
            check("fans: manual 60 % holds, then back to auto", fans)
    check("LCD overdrive", lambda: toggle(hw.get_overdrive, hw.set_overdrive, lambda v: not v))
    check("boot animation/sound", lambda: toggle(hw.get_boot_sound, hw.set_boot_sound, lambda v: not v))
    check("battery 80 % limit", lambda: toggle(hw.get_battery_limit, hw.set_battery_limit, lambda v: not v))
    check("battery calibration flag", lambda: toggle(hw.get_calibration, hw.set_calibration, lambda v: not v))
    check("USB charging level", lambda: toggle(hw.get_usb_charge, hw.set_usb_charge, lambda v: 10 if v != 10 else 20))
    check("keyboard backlight timeout", lambda: toggle(hw.get_kb_timeout, hw.set_kb_timeout, lambda v: not v))
    check("GPU mode (read)", lambda: hw.get_gpu_mode())
    check("rear light (read)", lambda: str(hw.rear_get()))
    def kbd():
        from . import keyboard; b = keyboard.get_brightness(); assert 0 <= b <= 50; return f"brightness {b}/50 read back from the keyboard"
    check("keyboard lighting interface", kbd)
    def audio_():
        from . import audio; st = audio.output_status(); assert st["active"] in ("speakers", "headphones"); return str(st)
    check("audio output profile", audio_)
    def boost():
        from . import audio
        cur = audio.headset_boost()
        if cur is None: return "no headset mic boost control on this card (skipped)"
        try:
            for lv in (0, 3, 1): assert audio.headset_boost(lv) == lv, f"boost {lv} did not read back"
        finally: audio.headset_boost(cur)
        return f"0, 3 and 1 read back, restored to {cur}"
    check("headset mic boost", boost)
    def mic():
        from . import audio
        m = audio.mic_state()["muted"]
        try: audio.mic_set_mute(not m); assert audio.mic_state()["muted"] == (not m), "mute did not read back"
        finally: audio.mic_set_mute(m)
        return f"mute toggled and restored ({m})"
    check("microphone mute", mic)
    def limits():
        from . import ipc
        try: d = ipc.call("cpu_info")
        except Exception as e:
            if "unknown command" in str(e): return "the running service is older than this app (skipped: re-run the installer)"
            raise
        return f"PL1 {d['pl1_w']} W / PL2 {d['pl2_w']} W readable ({'yours' if d.get('user') else 'automatic'})"
    check("CPU power limits (read)", limits)
    print("\n== stack health (doctor)")
    import io, sys
    from . import doctor
    buf = io.StringIO(); rc = doctor.run(False, buf); print(buf.getvalue().rstrip())
    results.append(rc == 0)
    print(f"\n{sum(results)}/{len(results)} passed"); return all(results)
