import argparse, json, os, sys
from . import backend as hw, fancurve, selftest

ONOFF = {"on": True, "off": False}


def _show(d): print(json.dumps(d, indent=2) if isinstance(d, (dict, list)) else d)


def status():
    out = {}
    for k, f in (("power_mode", hw.get_mode), ("supported_modes", hw.supported_modes), ("sensors", hw.sensors), ("fans", hw.fan_state),
                 ("lcd_overdrive", hw.get_overdrive), ("battery_limit_80", hw.get_battery_limit), ("usb_charge_stop", hw.get_usb_charge),
                 ("boot_sound", hw.get_boot_sound), ("kb_backlight_timeout", hw.get_kb_timeout), ("gpu_mode", hw.get_gpu_mode), ("rear", hw.rear_get)):
        try: out[k] = f()
        except hw.HwError as e: out[k] = f"ERROR: {e}"
    _show(out)


def main(argv=None):
    ap = argparse.ArgumentParser(prog="rpredatorsense", description="Acer Predator Helios 16 (PH16-71) control")
    sp = ap.add_subparsers(dest="cmd", required=True)
    sp.add_parser("status"); sp.add_parser("sensors")
    p = sp.add_parser("mode"); p.add_argument("value", nargs="?", choices=list(hw.MODES))
    p = sp.add_parser("fan"); p.add_argument("action", nargs="?", choices=["auto", "max", "set", "curve", "status"], default="status")
    p.add_argument("cpu", nargs="?", type=int); p.add_argument("gpu", nargs="?", type=int); p.add_argument("--curve"); p.add_argument("--delay", type=float, default=2.0)
    for n in ("overdrive", "battery", "boot-sound", "kb-timeout"):
        p = sp.add_parser(n); p.add_argument("value", nargs="?", choices=list(ONOFF))
    p = sp.add_parser("usb-charge"); p.add_argument("value", nargs="?", type=int, choices=hw.USB_LEVELS)
    p = sp.add_parser("gpu"); p.add_argument("value", nargs="?", choices=list(hw.GPU_MODES))
    p = sp.add_parser("rear"); p.add_argument("mode", nargs="?", choices=list(hw.REAR_MODES) + ["off", "get"]); p.add_argument("--color", default="ffffff")
    p.add_argument("--speed", type=int, default=3); p.add_argument("--brightness", type=int, default=100)
    p = sp.add_parser("keyboard", help="keyboard lighting"); p.add_argument("action", choices=["effect", "brightness", "off", "restore", "fill", "keys", "gradient", "rainbow", "profile", "list-keys", "get"]); p.add_argument("value", nargs="*")
    p.add_argument("--color", default="ffffff"); p.add_argument("--base", default="000000"); p.add_argument("--speed", type=int, default=5); p.add_argument("--brightness", type=int, default=30)
    p = sp.add_parser("mic", help="microphone: toggle | mode off|conference|personal | status"); p.add_argument("action", choices=["toggle", "mode", "status"]); p.add_argument("value", nargs="?"); p.add_argument("--target")
    p = sp.add_parser("output", help="speakers / headphones (works while the aux jack is plugged in)"); p.add_argument("value", nargs="?", choices=["speakers", "headphones", "toggle", "cycle", "status"])
    p = sp.add_parser("voice", help="Purified Voice per input: voice [status|apply|headset MODE|array MODE]"); p.add_argument("action", nargs="?", default="status", choices=["status", "apply", "headset", "array", "watch"]); p.add_argument("mode", nargs="?", choices=["off", "conference", "personal"])
    p = sp.add_parser("speaker", help="speaker noise reduction"); p.add_argument("value", choices=["on", "off"]); p.add_argument("--target")
    p = sp.add_parser("cpu", help="power limits / undervolt"); p.add_argument("action", choices=["info", "limits", "nvidia-limit", "uv-get", "uv-set", "uv-reset"]); p.add_argument("value", nargs="*", type=int)
    p.add_argument("--plane", type=int, default=0, choices=[0, 2], help="0 core, 2 cache"); p.add_argument("--confirm", action="store_true")
    sp.add_parser("keys", help="run the Fn/PREDATOR key service (user service)")
    sp.add_parser("daemon", help="run the fan/governor/profile daemon (systemd service)")
    p = sp.add_parser("selftest"); p.add_argument("--skip-fans", action="store_true")
    a = ap.parse_args(argv)
    try:
        if a.cmd == "status": status()
        elif a.cmd == "sensors": _show(hw.sensors())
        elif a.cmd == "mode":
            if a.value:
                from . import daemon_ctl; res = hw.set_mode(a.value); daemon_ctl.hold_profile(30); _show(res)
            else: _show({"mode": hw.get_mode(), "supported": hw.supported_modes()})
        elif a.cmd == "fan":
            if a.action == "auto": hw.fan_auto(); _show(hw.fan_state())
            elif a.action == "max": hw.fan_max(); _show(hw.fan_state())
            elif a.action == "set":
                if a.cpu is None: sys.exit("usage: fan set CPU% [GPU%]")
                hw.fan_manual(a.cpu, a.gpu if a.gpu is not None else a.cpu); _show(hw.fan_state())
            elif a.action == "curve": fancurve.run(fancurve.parse_curve(a.curve) if a.curve else None, a.delay)
            else: _show({**hw.fan_state(), **hw.sensors()})
        elif a.cmd in ("overdrive", "battery", "boot-sound", "kb-timeout"):
            g, s = {"overdrive": (hw.get_overdrive, hw.set_overdrive), "battery": (hw.get_battery_limit, hw.set_battery_limit),
                    "boot-sound": (hw.get_boot_sound, hw.set_boot_sound), "kb-timeout": (hw.get_kb_timeout, hw.set_kb_timeout)}[a.cmd]
            _show("on" if (s(ONOFF[a.value]) if a.value else g()) else "off")
        elif a.cmd == "usb-charge": _show(hw.set_usb_charge(a.value) if a.value is not None else hw.get_usb_charge())
        elif a.cmd == "gpu":
            if a.value: print("GPU mode change takes effect after reboot.")
            _show(hw.set_gpu_mode(a.value) if a.value else hw.get_gpu_mode())
        elif a.cmd == "rear":
            if a.mode in (None, "get"): _show(hw.rear_get())
            elif a.mode == "off": _show(hw.rear_set("static", brightness=0))
            else: _show(hw.rear_set(a.mode, a.color, a.speed, a.brightness))
        elif a.cmd == "keyboard":
            from . import keyboard as kb
            from . import keymap as km, keyprofiles
            V = a.value; B = a.brightness if "--brightness" in (argv or sys.argv) else None   # default: keep the current (Fn-set) brightness
            if a.action == "get": _show({"brightness": kb.get_brightness(), "max": kb.MAX_BRIGHT})
            elif a.action == "list-keys": _show({"keys": sorted(km.NAMES), "groups": sorted(km.GROUPS)})
            elif a.action == "fill": kb.per_key(km.to_list(km.canvas(km.hexcolor(V[0]))), B)
            elif a.action == "gradient": kb.per_key(km.to_list(km.gradient(km.hexcolor(V[0]), km.hexcolor(V[1]))), B)
            elif a.action == "rainbow": kb.per_key(km.to_list(km.rainbow()), B)
            elif a.action == "keys":                       # keys "wasd=ff0000" "esc=00ff00" [--base 101010]
                cv = km.canvas(km.hexcolor(a.base))
                for item in V:
                    k_, _, col = item.partition("=")
                    for c in km.cells_for(k_): cv[c] = km.hexcolor(col)
                kb.per_key(km.to_list(cv), B)
            elif a.action == "profile": keyprofiles.cli(V, B)
            elif a.action == "off": kb.set_effect("off")
            elif a.action == "restore": kb.restore_default(B)
            elif a.action == "brightness": kb.set_brightness(int(V[0]))
            elif a.action == "effect": kb.set_effect(V[0], a.color, a.speed, a.brightness)
            if a.action in ("effect", "off", "restore", "brightness", "fill", "keys", "gradient", "rainbow"): print("sent")
        elif a.cmd == "mic":
            from . import audio
            if a.action == "toggle": print(audio.mic_toggle())
            elif a.action == "mode": print(audio.set_mic(a.value, a.target))
            else: _show(audio.status())
        elif a.cmd == "output":
            from . import audio
            if a.value in (None, "status"): _show(audio.output_status())
            elif a.value == "cycle":
                lab, icon = audio.cycle_output(); print("output:", lab)      # the instant notification was shown before the switch
            elif a.value == "toggle":
                tgt = "headphones" if audio.output_status()["active"] == "speakers" else "speakers"
                audio.announce("Laptop speakers" if tgt == "speakers" else "Headphones (3.5 mm jack)")
                print("output:", audio.set_output(tgt))
            else:
                audio.announce("Laptop speakers" if a.value == "speakers" else "Headphones (3.5 mm jack)")
                print("output:", audio.set_output(a.value))
        elif a.cmd == "voice":
            from . import audio, config
            if a.action == "status": _show({"modes": audio.pv_modes(), "plugged": audio.amixer_on("Headset Mic Jack"), "default_source": audio._run(["pactl", "get-default-source"]).stdout.strip()})
            elif a.action == "apply": _show(audio.apply_mic_policy())
            elif a.action == "watch": audio.watch_mic(config.load(os.path.expanduser("~/.config/rpredatorsense/config.toml")))
            else: print(a.action, "->", audio.pv_set(a.action, a.mode or "off"))
        elif a.cmd == "speaker":
            from . import audio; print(audio.set_speaker(a.value == "on", target=a.target))
        elif a.cmd == "cpu":
            from . import cpu
            if a.action == "info": _show(cpu.info())
            elif a.action == "limits": _show(cpu.set_limits(*a.value) if len(a.value) == 2 else cpu.info())
            elif a.action == "nvidia-limit": print(cpu.set_nvidia_limit(a.value[0]))
            elif a.action == "uv-get": _show({"plane": a.plane, "offset_mv": cpu.uv_get(a.plane)})
            elif a.action == "uv-set": _show({"plane": a.plane, "offset_mv": cpu.uv_set(a.plane, a.value[0], a.confirm)})
            else: _show(cpu.uv_reset())
        elif a.cmd == "keys":
            from . import fnkeys, config; fnkeys.main(config.load(os.path.expanduser("~/.config/rpredatorsense/config.toml")))
        elif a.cmd == "daemon":
            from . import daemon; daemon.main()
        elif a.cmd == "selftest": sys.exit(0 if selftest.run(a.skip_fans) else 1)
    except hw.HwError as e:
        sys.exit(f"error: {e}")


if __name__ == "__main__":
    main()
