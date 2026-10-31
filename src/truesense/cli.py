"""truesense: command line for the Acer Predator Helios 16 (PH16-71).   truesense <area> <verb> ...   (every setting has on / off / toggle / status; --json for scripts)

Areas: status sensors power fan battery display boot gpu rear keyboard look startup audio keys profile cpu touchpad service config doctor selftest completion gui
"""
import argparse, json, os, subprocess, sys
from . import __version__
from .device import HwError

JSON = False


def emit(value, human=None):
    """Print a result: JSON for scripts (--json), otherwise the human form (or pretty JSON for dicts)."""
    if JSON: print(json.dumps(value, indent=2, default=str))
    elif human is not None: print(human)
    elif isinstance(value, (dict, list)): print(json.dumps(value, indent=2, default=str))
    elif isinstance(value, bool): print("on" if value else "off")
    elif value is not None: print(value)


def onoff_value(v): return "on" if v else "off" if v is not None else "unknown"


def tri(action, get, set_, label=None):
    """The on / off / toggle / status pattern shared by every boolean setting."""
    cur = get()
    if action == "status" or action is None: new = cur
    elif action == "toggle": new = set_(not cur)
    else: new = set_(action == "on")
    emit(new, f"{label + ': ' if label else ''}{onoff_value(new)}")
    return new


def _ipc():
    from . import ipc
    return ipc


def daemon_set(key, value):
    return _ipc().call("set", key=key, value=value)


# ---- builders ---------------------------------------------------------------------------------------------------------------------------
def add_onoff(sub, name, help_, extra=()):
    p = sub.add_parser(name, help=help_); p.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status", *extra]); return p


def build():
    from . import backend as hw
    ap = argparse.ArgumentParser(prog="truesense", description=__doc__.split("\n\n")[0], formatter_class=argparse.RawDescriptionHelpFormatter,
                                 epilog="Examples:\n  truesense status\n  truesense power set turbo\n  truesense fan set 60\n  truesense rear set breathing --color 00ff88\n  truesense keyboard effect wave --brightness 50\n"
                                        "  truesense keys set double output-toggle\n  truesense profile apply gaming\n  truesense doctor")
    ap.add_argument("--json", action="store_true", help="machine-readable output"); ap.add_argument("-V", "--version", action="version", version=f"truesense {__version__}")
    sub = ap.add_subparsers(dest="area", metavar="area")

    p = sub.add_parser("status", help="everything at a glance"); p = sub.add_parser("sensors", help="temperatures and fan RPM")
    # power
    p = sub.add_parser("power", help="power mode (quiet balanced performance turbo eco) and the automatic profile"); ps = p.add_subparsers(dest="verb")
    ps.add_parser("status"); ps.add_parser("list"); q = ps.add_parser("set"); q.add_argument("mode", choices=list(hw.MODES))
    q = ps.add_parser("next"); q = ps.add_parser("prev")
    q = ps.add_parser("auto", help="automatic power mode: off | native (PredatorSense-like: power source only) | smart (heat, load and battery aware); on = smart"); q.add_argument("action", nargs="?", default="status", choices=["off", "native", "smart", "on", "toggle", "status"])
    q = ps.add_parser("hold", help="keep the current power mode against the automatic profile"); q.add_argument("minutes", nargs="?", type=float, default=30)
    ps.add_parser("release", help="end a hold")
    # fan
    p = sub.add_parser("fan", help="fans: firmware auto, max, manual %%, smart curve"); fs = p.add_subparsers(dest="verb")
    fs.add_parser("status"); fs.add_parser("auto", help="firmware control (0 rpm when cool)"); fs.add_parser("max")
    q = fs.add_parser("set", help="manual duty, 20..100 %%"); q.add_argument("cpu", type=int); q.add_argument("gpu", type=int, nargs="?")
    q = fs.add_parser("curve", help="smart-fan curves: list | current | show NAME | use NAME | save NAME --cpu PTS --gpu PTS | delete NAME"); q.add_argument("action", nargs="?", default="current", choices=["list", "current", "show", "use", "save", "delete"])
    q.add_argument("name", nargs="?"); q.add_argument("--cpu", help="CPU points, e.g. 52:0,60:35,70:55,80:75,89:100 (temperature:percent, 0 = firmware auto)"); q.add_argument("--gpu", help="GPU points")
    for k_, d_ in (("hysteresis", None), ("down-rate", None), ("predict", None), ("couple", None), ("battery-shift", None)): q.add_argument("--" + k_, type=float, default=d_)
    q.add_argument("--from", dest="base", help="start from this curve and change only what you give")
    q = fs.add_parser("smart", help="the daemon's smart curve"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status"])
    # battery
    p = sub.add_parser("battery", help="80 %% charge limit, calibration, USB power-off charging"); bs = p.add_subparsers(dest="verb")
    q = bs.add_parser("limit"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status"])
    q = bs.add_parser("calibration"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status"])
    q = bs.add_parser("usb-charge", help="USB charging while powered off: stop level 0 (off) 10 20 30"); q.add_argument("level", nargs="?", default="status", choices=["status", "0", "10", "20", "30"])
    bs.add_parser("status")
    # simple settings
    p = sub.add_parser("display", help="LCD overdrive"); ds = p.add_subparsers(dest="verb"); q = ds.add_parser("overdrive"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status"])
    p = sub.add_parser("boot", help="boot animation and sound"); bs2 = p.add_subparsers(dest="verb"); q = bs2.add_parser("sound"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status"])
    p = sub.add_parser("gpu", help="GPU mode (MUX); a change needs a reboot"); gs = p.add_subparsers(dest="verb"); gs.add_parser("status"); q = gs.add_parser("set"); q.add_argument("mode", choices=list(hw.GPU_MODES))
    # rear
    p = sub.add_parser("rear", help="rear light bar"); rs = p.add_subparsers(dest="verb")
    rs.add_parser("status"); rs.add_parser("on"); rs.add_parser("off"); rs.add_parser("toggle")
    q = rs.add_parser("set"); q.add_argument("effect", choices=list(hw.REAR_MODES)); q.add_argument("--color", default="ffffff"); q.add_argument("--speed", type=int, default=3)
    q.add_argument("--brightness", type=int, default=100); q.add_argument("--direction", type=int, default=1, choices=[0, 1])
    q.add_argument("--zones", help="static only: one colour, or three (left,middle,right), e.g. ff0000,00ff00,0000ff")
    # look (saved lighting looks)
    p = sub.add_parser("look", help="saved lighting looks (keyboard effect/brightness/colours + rear light)"); ls = p.add_subparsers(dest="verb")
    ls.add_parser("list"); q = ls.add_parser("save", help="save what is on the lights now"); q.add_argument("name")
    for v_ in ("apply", "show", "delete"): q = ls.add_parser(v_); q.add_argument("name")
    # startup
    p = sub.add_parser("startup", help="what the lights and profile do at login"); ss = p.add_subparsers(dest="verb")
    ss.add_parser("show"); ss.add_parser("apply", help="do it now (the user service does this at login)")
    q = ss.add_parser("boot", help="the look applied at BOOT (before login) and after suspend: NAME | current | clear (no argument: show)"); q.add_argument("what", nargs="?")
    q = ss.add_parser("set"); q.add_argument("target", choices=["keyboard", "rear", "profile"]); q.add_argument("value", help="keyboard: last|default|none|off|profile:NAME   rear: last|default|none|off   profile: a profile name or none")
    # keyboard
    p = sub.add_parser("keyboard", help="keyboard lighting"); ks = p.add_subparsers(dest="verb")
    ks.add_parser("status"); ks.add_parser("on"); ks.add_parser("off"); ks.add_parser("toggle"); ks.add_parser("restore", help="back to the stock cyan per-key state"); ks.add_parser("list-keys")
    q = ks.add_parser("effect"); q.add_argument("name"); q.add_argument("--color", default="00aec7"); q.add_argument("--speed", type=int, default=5); q.add_argument("--brightness", type=int, default=None); q.add_argument("--direction", type=int, default=0)
    q = ks.add_parser("speed", help="animation speed of the running effect, 1 (slow) .. 11 (fast)"); q.add_argument("level", type=int)
    q = ks.add_parser("color", help="change the colour of the current effect (stays in the same mode)"); q.add_argument("color")
    q = ks.add_parser("paint", help='colour some keys and keep the rest, e.g. paint w=ff0000 arrows=00ff00 esc=ffffff'); q.add_argument("items", nargs="+"); q.add_argument("--brightness", type=int, default=None)
    ks.add_parser("layout", help="key names and positions (for other tools)")
    q = ks.add_parser("fill"); q.add_argument("color"); q.add_argument("--brightness", type=int, default=None)
    q = ks.add_parser("gradient"); q.add_argument("from_color"); q.add_argument("to_color"); q.add_argument("--brightness", type=int, default=None)
    q = ks.add_parser("rainbow"); q.add_argument("--brightness", type=int, default=None)
    q = ks.add_parser("keys", help='per-key colours, e.g. keys "wasd=ff0000" "esc=00ff00" --base 101010'); q.add_argument("items", nargs="+"); q.add_argument("--base", default="000000"); q.add_argument("--brightness", type=int, default=None)
    q = ks.add_parser("brightness", help="0..50 (also Fn+F11/F12)"); q.add_argument("level", nargs="?", type=int)
    q = ks.add_parser("timeout", help="backlight off after 30 s of no typing"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "toggle", "status"])
    q = ks.add_parser("operating", help="colour follows the power mode"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "status"])
    q = ks.add_parser("profile", help="saved lighting profiles"); q.add_argument("verb2", choices=["list", "load", "save", "delete"]); q.add_argument("name", nargs="?"); q.add_argument("spec", nargs="*", help="save: fill COLOR | gradient C1 C2 | rainbow")
    # audio
    p = sub.add_parser("audio", help="output, microphone, headset jack, Purified Voice, DTS"); au = p.add_subparsers(dest="verb")
    q = au.add_parser("output", help="speakers / headphones while the aux jack is plugged in"); q.add_argument("action", nargs="?", default="status", choices=["status", "speakers", "headphones", "toggle", "cycle"])
    q = au.add_parser("mic"); q.add_argument("action", nargs="?", default="status", choices=["status", "mute", "unmute", "toggle", "volume", "boost"]); q.add_argument("value", nargs="?", type=int); q.add_argument("--default-only", action="store_true", help="act on the default input only (default: every input)")
    q = au.add_parser("jack", help="what the codec sees on the 3.5 mm jack; `jack headphones|headset|auto` sets how the jack is used"); q.add_argument("use", nargs="?", choices=["auto", "headphones", "headset"])
    q = au.add_parser("voice", help="Purified Voice (AI mic noise reduction)"); q.add_argument("action", nargs="?", default="status", choices=["status", "set", "enable", "disable", "next", "apply", "watch", "notify"]); q.add_argument("a", nargs="?"); q.add_argument("b", nargs="?")
    q = au.add_parser("speaker-filter", help="noise filter on the output (off by default: it also gates music)"); q.add_argument("action", nargs="?", default="status", choices=["on", "off", "status"])
    q = au.add_parser("dts", help="Acer DTS sound through AcerUltraX"); q.add_argument("action", nargs="?", default="status", choices=["status", "on", "off", "mode", "profiles", "setup"]); q.add_argument("name", nargs="?")
    # keys
    p = sub.add_parser("keys", help="PREDATOR key and Fn+Esc: what each gesture does"); ys = p.add_subparsers(dest="verb")
    ys.add_parser("show"); ys.add_parser("actions"); ys.add_parser("run", help="run the key service in the foreground (what the systemd user service does)")
    q = ys.add_parser("set"); q.add_argument("gesture", choices=["tap", "double", "triple", "long", "fn_esc"]); q.add_argument("action", help="see: truesense keys actions")
    q = ys.add_parser("reset"); q.add_argument("gesture", nargs="?", choices=["tap", "double", "triple", "long", "fn_esc"])
    q = ys.add_parser("test"); q.add_argument("action", help="run an action now, as the key would")
    q = ys.add_parser("timing"); q.add_argument("--long-ms", type=int); q.add_argument("--multi-ms", type=int)
    # profile
    p = sub.add_parser("profile", help="named bundles of settings (gaming, quiet, battery-saver, yours)"); pr = p.add_subparsers(dest="verb")
    pr.add_parser("list"); pr.add_parser("next"); q = pr.add_parser("show"); q.add_argument("name"); q = pr.add_parser("apply"); q.add_argument("name")
    q = pr.add_parser("save", help="save the CURRENT state as a profile"); q.add_argument("name"); q.add_argument("--description", default="")
    q = pr.add_parser("delete"); q.add_argument("name")
    # cpu
    p = sub.add_parser("cpu", help="CPU power limits and the guarded undervolt"); cs = p.add_subparsers(dest="verb"); cs.add_parser("info"); q = cs.add_parser("limits"); q.add_argument("pl1", type=float, nargs="?"); q.add_argument("pl2", type=float, nargs="?")
    q = cs.add_parser("undervolt"); q.add_argument("action", choices=["get", "set", "reset"]); q.add_argument("mv", type=int, nargs="?"); q.add_argument("--plane", type=int, default=0, choices=[0, 2], help="0 core, 2 cache"); q.add_argument("--confirm", action="store_true")
    add_onoff(sub, "touchpad", "touchpad on/off (Fn+F10)")
    p = sub.add_parser("service", help="the daemon"); ss = p.add_subparsers(dest="verb"); ss.add_parser("status"); ss.add_parser("restart"); ss.add_parser("logs")
    p = sub.add_parser("config", help="configuration"); cf = p.add_subparsers(dest="verb"); cf.add_parser("show"); cf.add_parser("validate"); cf.add_parser("path")
    p = sub.add_parser("doctor", help="check the whole stack and say how to fix problems")
    p = sub.add_parser("selftest", help="set -> read back -> restore every firmware feature"); p.add_argument("--skip-fans", action="store_true")
    p = sub.add_parser("completion", help="shell completion script"); p.add_argument("shell", choices=["bash", "zsh", "fish"])
    g = sub.add_parser("gui", help="the small optional window"); g.add_argument("--page", choices=["home", "keys", "audio", "settings"], help="open on this page"); sub.add_parser("daemon", help="run the daemon (systemd does this)"); sub.add_parser("version")
    return ap


# ---- handlers ----------------------------------------------------------------------------------------------------------------------------
def h_status(a):
    from . import audio, backend as hw, ipc, touchpad
    out = {}
    def put(k, f):
        try: out[k] = f()
        except Exception as e: out[k] = f"ERROR: {e}"
    put("power_mode", hw.get_mode); put("sensors", hw.sensors); put("fans", hw.fan_state)
    put("daemon", lambda: ipc.call("state")); put("battery_limit", hw.get_battery_limit); put("usb_charge_stop", hw.get_usb_charge); put("overdrive", hw.get_overdrive)
    put("boot_sound", hw.get_boot_sound); put("kb_backlight_timeout", hw.get_kb_timeout); put("gpu_mode", hw.get_gpu_mode); put("rear", hw.rear_get)
    put("touchpad", touchpad.enabled); put("audio_output", audio.output_status); put("jack", lambda: audio.jack_status()["raw"]); put("voice", audio.voice_modes)
    emit(out)


def h_sensors(a):
    from . import backend as hw
    s = hw.sensors(); emit(s, f"CPU {s['cpu_temp']} C   GPU {s['gpu_temp']} C   board {s['sys_temp']} C   fans {s['cpu_fan_rpm']} / {s['gpu_fan_rpm']} rpm")


def h_power(a):
    from . import backend as hw, ipc
    v = a.verb or "status"
    if v == "status":
        d = {"mode": hw.get_mode(), "supported": hw.supported_modes()}
        try: st = ipc.call("state"); d.update(auto_profile=st["settings"]["auto_profile"], hold_left_s=st["hold_left_s"])
        except HwError: pass
        emit(d, f"power mode: {d['mode']}" + (f"   (automatic profile {'on' if d.get('auto_profile') else 'off'}" + (f", manual choice held for {d['hold_left_s']//60} more min" if d.get("hold_left_s") else "") + ")" if "auto_profile" in d else ""))
    elif v == "list": emit(hw.supported_modes(), "  ".join(hw.supported_modes()))
    elif v == "set":
        from . import control
        m = control.power(a.mode, 30)
        emit(m, f"power mode: {m}   (held 30 min against the automatic profile; `truesense power release` to end)")
    elif v in ("next", "prev"):
        from . import actions; emit(actions.run("power-" + v))
    elif v == "auto":
        cur = ipc.call("state")["settings"].get("auto_mode") or ("smart" if ipc.call("state")["settings"]["auto_profile"] else "off")
        want = {"on": "smart", "status": cur, "toggle": "off" if cur != "off" else "smart"}.get(a.action, a.action)
        if want != cur: daemon_set("auto_mode", want)
        emit(want, {"off": "automatic power mode: off (you choose)", "native": "automatic power mode: native (power source only, like PredatorSense: AC balanced, battery eco)",
                    "smart": "automatic power mode: smart (heat, load and battery aware)"}[want])
    elif v == "hold": ipc.call("hold", minutes=a.minutes); emit(f"holding the current power mode for {a.minutes:g} min")
    elif v == "release": ipc.call("hold", minutes=None); emit("hold released: the automatic profile is in charge again")


def h_fan(a):
    from . import backend as hw, ipc, control
    v = a.verb or "status"
    def off(): 
        try: daemon_set("smart_fan", False); __import__("time").sleep(1.2)
        except HwError: pass
    if v == "status":
        d = {**hw.fan_state(), **{k: x for k, x in hw.sensors().items() if "fan" in k}}
        try: d["smart"] = ipc.call("state")["settings"]["smart_fan"]
        except HwError: d["smart"] = None
        emit(d, f"fans: {d['mode']}   CPU {d['cpu_fan_rpm']} rpm   GPU {d['gpu_fan_rpm']} rpm   smart curve: {onoff_value(d['smart'])}")
    elif v == "auto": control.fan("auto"); emit("fans: firmware auto (they stop when cool)")
    elif v == "max": control.fan("max"); emit("fans: MAX  (`truesense fan smart on` hands them back)")
    elif v == "set": control.fan("manual", a.cpu, a.gpu); emit(f"fans: manual {a.cpu} % / {a.gpu if a.gpu is not None else a.cpu} %")
    elif v == "curve":
        from . import curves
        act = a.action
        if act == "list": r = curves.names(); cur = ipc.call("fan_curve", action="get")["name"] if ipc.available() else None; emit(r, "\n".join(("* " if n == cur else "  ") + n + ("  - " + curves.BUILTIN[n]["description"] if n in curves.BUILTIN else "") for n in r))
        elif act == "current": d = ipc.call("fan_curve", action="get"); emit(d, f"fan curve in use: {d['name']}\n  cpu {d['curve']['cpu']}\n  gpu {d['curve']['gpu']}")
        elif act == "show": c = curves.load(a.name or "truesense"); emit(c, f"{a.name or 'truesense'}\n  cpu {c['cpu']}\n  gpu {c['gpu']}\n  " + "  ".join(f"{k}={c[k]}" for k in curves.DEFAULTS))
        elif act == "use":
            if not a.name: raise HwError("which curve? (truesense fan curve list)")
            d = ipc.call("fan_curve", action="use", name=a.name, data=None if a.name in curves.BUILTIN else curves.load(a.name)); emit(d["name"], f"fan curve in use: {d['name']}")
        elif act == "save":
            if not a.name: raise HwError("save under which name?")
            base = curves.load(a.base) if a.base else (curves.load(a.name) if a.name in curves.names() and a.name not in curves.BUILTIN else {})
            c = {**{k: v_ for k, v_ in base.items() if k != "description"}}
            if a.cpu: c["cpu"] = curves.parse_points(a.cpu)
            if a.gpu: c["gpu"] = curves.parse_points(a.gpu)
            for k_ in curves.DEFAULTS:
                v_ = getattr(a, k_.replace("-", "_").replace("_", "_"), None)
                if v_ is not None: c[k_] = v_
            if "cpu" not in c or "gpu" not in c: raise HwError("give --cpu and --gpu points (or --from an existing curve)")
            saved = curves.save(a.name, c); emit(saved, f"saved fan curve '{a.name}' (use it: truesense fan curve use {a.name})")
        elif act == "delete": curves.delete(a.name or ""); emit(a.name, f"deleted fan curve '{a.name}'")
    elif v == "smart": tri(a.action, lambda: ipc.call("state")["settings"]["smart_fan"], lambda x: daemon_set("smart_fan", x)["smart_fan"], "smart fan curve")


def h_battery(a):
    from . import backend as hw
    v = a.verb or "status"
    if v == "status": emit({"limit_80": hw.get_battery_limit(), "calibration": hw.get_calibration(), "usb_charge_stop": hw.get_usb_charge()})
    elif v == "limit": tri(a.action, hw.get_battery_limit, hw.set_battery_limit, "80 % battery limit")
    elif v == "calibration": tri(a.action, hw.get_calibration, hw.set_calibration, "battery calibration (needs AC, takes hours)")
    elif v == "usb-charge":
        if a.level == "status": emit(hw.get_usb_charge(), f"USB charging while off, stop level: {hw.get_usb_charge()}")
        else: lv = hw.set_usb_charge(int(a.level)); emit(lv, f"USB charging while off, stop level: {lv}")


def h_display(a):
    from . import backend as hw
    tri(a.action, hw.get_overdrive, hw.set_overdrive, "LCD overdrive")

def h_boot(a):
    from . import backend as hw
    tri(a.action, hw.get_boot_sound, hw.set_boot_sound, "boot sound")

def h_gpu(a):
    from . import backend as hw
    if a.verb == "set": m = hw.set_gpu_mode(a.mode); emit(m, f"GPU mode: {m}   (takes effect after a reboot)")
    else: emit(hw.get_gpu_mode(), f"GPU mode: {hw.get_gpu_mode()}")


def h_rear(a):
    from . import actions, backend as hw
    v = a.verb or "status"
    if v == "status": emit(hw.rear_get())
    elif v == "set": emit(hw.rear_set(a.effect, a.color, a.speed, a.brightness, a.direction, a.zones.split(",") if a.zones else None))
    else: emit(actions.run("rear:" + v))


def h_look(a):
    from . import looks
    v = a.verb or "list"
    if v == "list": r = looks.names(); emit(r, "\n".join(r) or "(no saved looks; `truesense look save NAME`)")
    elif v == "save": l = looks.save(a.name); emit(l, f"saved look '{a.name}': {looks.describe(l)}")
    elif v == "show": l = looks.load(a.name); emit(l, f"{a.name}: {looks.describe(l)}")
    elif v == "apply": r = looks.apply(looks.load(a.name)); emit(r, "\n".join(r))
    elif v == "delete": looks.delete(a.name); emit(a.name, f"deleted look '{a.name}'")


def h_startup(a):
    from . import config, startup, profiles, keyprofiles, looks, ipc
    v = a.verb or "show"
    if v == "boot":
        cur = ipc.call("boot_look", action="get")
        if a.what is None: emit(cur, ("at boot and after suspend: " + looks.describe(cur)) if cur else "no look is applied at boot")
        elif a.what == "clear": ipc.call("boot_look", action="clear"); emit(None, "nothing is applied at boot any more")
        else:
            look = looks.capture() if a.what == "current" else looks.load(a.what)
            ipc.call("boot_look", action="set", look=look); emit(look, f"at boot and after suspend: {looks.describe(look)}")
    elif v == "show": emit(config.load()["startup"])
    elif v == "apply": r = startup.apply(); emit(r, "\n".join(r))
    elif v == "set":
        val = "" if (a.target == "profile" and a.value == "none") else a.value
        if a.target == "profile" and val and val not in profiles.names(): raise HwError(f"no profile {val!r}: {', '.join(profiles.names())}")
        if a.target == "keyboard" and val.startswith("profile:") and not keyprofiles.exists(val[8:]): raise HwError(f"no lighting profile {val[8:]!r}: {', '.join(keyprofiles.names())}")
        if val.startswith("look:") and val[5:] not in looks.names(): raise HwError(f"no look {val[5:]!r}: {', '.join(looks.names())}")
        cfg = config.load(); cfg["startup"][a.target] = val; bad = [p for p in config.validate(cfg) if "[startup]" in p]
        if bad: raise HwError(bad[0])
        config.write_user({"startup": {a.target: val}}); emit(val, f"at login, {a.target}: {val or 'none'}")


def h_keyboard(a):
    from . import actions, backend as hw, config, keyboard as kb, keymap as km, keyprofiles, userstate, ipc
    v = a.verb or "status"
    def done(msg="sent"): emit(msg)
    if v == "status": emit({"look": kb.last(), "brightness": kb.get_brightness(), "max": kb.MAX_BRIGHT, "backlight_timeout": hw.get_kb_timeout(), "lighting_profiles": keyprofiles.names()})
    elif v in ("on", "off", "toggle"): emit(actions.run("keyboard:" + v))
    elif v == "restore": kb.restore_default(None); done()
    elif v == "list-keys": emit({"keys": sorted(km.NAMES), "groups": sorted(km.GROUPS)})
    elif v == "effect":
        if a.name == "operatingmode": kb.set_operating_mode(a.color)
        else:
            b = a.brightness if a.brightness is not None else (kb.get_brightness() or 30)
            kb.set_effect(a.name, a.color, a.speed, 50 if (a.name == "neon" and a.brightness is None) else b, a.direction)
        done()
    elif v == "color": kb.set_color(a.color); done()
    elif v == "speed": kb.set_speed(a.level); done(f"keyboard effect speed: {a.level} / 11")
    elif v == "paint":
        items = {}
        for item in a.items:
            k_, _, col = item.partition("=")
            if not col: raise HwError(f"'{item}': use key=RRGGBB")
            items[k_] = col
        kb.paint(items, a.brightness); done(f"painted {len(items)} key(s)")
    elif v == "layout": emit({"keys": {n: [{"cell": c, "col": c // 6, "row": c % 6} for c in cs] for n, cs in km.NAMES.items()}, "groups": sorted(km.GROUPS)})
    elif v == "fill": kb.per_key(km.to_list(km.canvas(km.hexcolor(a.color))), a.brightness); done()
    elif v == "gradient": kb.per_key(km.to_list(km.gradient(km.hexcolor(a.from_color), km.hexcolor(a.to_color))), a.brightness); done()
    elif v == "rainbow": kb.per_key(km.to_list(km.rainbow()), a.brightness); done()
    elif v == "keys":
        cv = km.canvas(km.hexcolor(a.base))
        for item in a.items:
            k_, _, col = item.partition("=")
            if not col: raise HwError(f"'{item}': use key=RRGGBB")
            for c in km.cells_for(k_): cv[c] = km.hexcolor(col)
        kb.per_key(km.to_list(cv), a.brightness); done()
    elif v == "brightness":
        if a.level is None: emit(kb.get_brightness(), f"keyboard brightness: {kb.get_brightness()} / {kb.MAX_BRIGHT}")
        else: kb.set_brightness(a.level); emit(a.level, f"keyboard brightness: {a.level} / {kb.MAX_BRIGHT}")
    elif v == "timeout": tri(a.action, hw.get_kb_timeout, hw.set_kb_timeout, "backlight timeout (off after 30 s)")
    elif v == "operating": tri(a.action, lambda: ipc.call("state")["settings"]["operating_follow"], lambda x: daemon_set("operating_follow", x)["operating_follow"], "operating-mode colour follows the power mode")
    elif v == "profile": r = keyprofiles.command(a.verb2, a.name, a.spec); emit(r, "\n".join(r) or "(no lighting profiles)" if isinstance(r, list) else r)


def h_audio(a):
    from . import audio, config
    v = a.verb
    if not v: raise HwError("choose: output | mic | jack | voice | speaker-filter | dts   (truesense audio -h)")
    if v == "output":
        if a.action == "status": emit(audio.output_status())
        elif a.action == "toggle": audio.output_toggle(); emit(audio.output_status())
        elif a.action == "cycle": emit(audio.output_cycle(), None)
        else: audio._say(f"Audio output: {audio.LABEL[a.action]}", audio.ICON[a.action]); emit(audio.set_output(a.action), f"audio output: {a.action}")
    elif v == "mic":
        act = a.action
        if act == "status": emit(audio.mic_state())
        elif act in ("mute", "unmute"): audio.mic_set_mute(act == "mute", not a.default_only); emit(act + "d")
        elif act == "toggle": m = audio.mic_toggle(not a.default_only); emit(m, "microphones: MUTED" if m else "microphones: live")
        elif act == "volume":
            r = audio.mic_volume(a.value); emit(r, f"microphone volume: {r} %")
        elif act == "boost":
            r = audio.headset_boost(a.value); emit(r, f"headset mic boost: {r} (0..3, +10 dB per step)")
    elif v == "jack":
        if a.use:
            config.write_user({"audio": {"jack": a.use}}); audio.apply_mic_policy(False)
            emit(a.use, f"3.5 mm jack: {a.use}" + ("  (headset microphone is used even if the codec does not detect it)" if a.use == "headset" else ""))
        else: emit({**audio.jack_status(), "setting": config.load()["audio"]["jack"], "microphone_in_use": audio.jack_use()})
    elif v == "voice":
        act = a.action
        if act == "status": emit({"modes": audio.voice_modes(), "jack": audio.jack_status()["headset_mic"], "default_source": audio._run(["pactl", "get-default-source"]).stdout.strip(), "notify": config.load()["audio"]["notify"]})
        elif act == "set":
            if not a.a or not a.b: raise HwError("usage: truesense audio voice set headset|array off|conference|personal")
            emit(audio.voice_set(a.a, a.b), f"Purified Voice ({a.a}): {a.b}"); audio.apply_mic_policy(False)
        elif act == "enable": emit(audio.voice_enable(True)); audio.apply_mic_policy(False)
        elif act == "disable": emit(audio.voice_enable(False)); audio.apply_mic_policy(False)
        elif act == "next": dev, m = audio.voice_next(); audio.apply_mic_policy(False); emit(f"Purified Voice ({dev}): {m}")
        elif act == "apply": emit(audio.apply_mic_policy(True))
        elif act == "watch": audio.watch_mic()
        elif act == "notify":
            if a.a not in ("on", "off"): raise HwError("usage: truesense audio voice notify on|off")
            config.write_user({"audio": {"notify": a.a == "on"}}); emit(f"mic mode notification: {a.a}")
    elif v == "speaker-filter":
        if a.action == "status": emit(audio.speaker_filter_on(), f"speaker filter: {onoff_value(audio.speaker_filter_on())}")
        else: audio.speaker_filter(a.action == "on"); emit(f"speaker filter: {a.action}")
    elif v == "dts":
        act = a.action
        if act == "status": print(audio.dts_status())
        elif act in ("on", "off"): print(audio.dts_power(act == "on"))
        elif act == "mode":
            if not a.name: raise HwError("usage: truesense audio dts mode music|movie|voice|game1|game2|game3|off|plain|\"custom audio\"")
            print(audio.dts_mode(a.name))
        elif act == "profiles": print(audio.dts("profiles"))
        elif act == "setup": print(audio.dts("setup", timeout=1800))


def h_keys(a):
    from . import actions, config
    v = a.verb or "show"; cfg = config.load()
    if v == "show": emit(cfg["keys"], "\n".join(f"{g:<8} {cfg['keys'][g]}" for g in ("tap", "double", "triple", "long", "fn_esc")) + f"\n(long press after {cfg['keys']['long_ms']} ms; taps within {cfg['keys']['multi_ms']} ms count together)")
    elif v == "actions": print(actions.catalogue())
    elif v == "run": from . import fnkeys; fnkeys.run(cfg)
    elif v == "set":
        err = actions.validate(a.action)
        if err: raise HwError(err + "   (truesense keys actions)")
        config.write_user({"keys": {a.gesture: a.action}}); emit(f"{a.gesture} -> {a.action}   (the key service picks it up on its next restart: systemctl --user restart truesense-keys)")
    elif v == "reset":
        gs = [a.gesture] if a.gesture else ["tap", "double", "triple", "long", "fn_esc"]
        config.write_user({"keys": {g: config.DEFAULTS["keys"][g] for g in gs}}); emit("reset: " + ", ".join(gs))
    elif v == "test": msg = actions.run(a.action, cfg); emit(msg or "ok")
    elif v == "timing":
        upd = {k: x for k, x in (("long_ms", a.long_ms), ("multi_ms", a.multi_ms)) if x}
        if not upd: emit({k: cfg["keys"][k] for k in ("long_ms", "multi_ms")})
        else: config.write_user({"keys": upd}); emit(f"timing: {upd}")


def h_profile(a):
    from . import config, profiles
    v = a.verb or "list"
    if v == "list": emit([{"name": n, "source": profiles.source(n), "description": profiles.load(n).get("description", "")} for n in profiles.names()],
                          "\n".join(f"{n:<16} [{profiles.source(n):>7}] {profiles.load(n).get('description', '')}" for n in profiles.names()) or "(no profiles)")
    elif v == "show": d = profiles.load(a.name); emit(d, config.dumps(d))
    elif v == "apply": emit(profiles.apply(a.name))
    elif v == "next": emit(profiles.apply(profiles.next_name()))
    elif v == "save": emit(profiles.save(a.name, profiles.capture(a.description)), f"saved the current state as profile '{a.name}'")
    elif v == "delete": profiles.delete(a.name); emit(f"deleted {a.name}")


def h_cpu(a):
    from . import cpu
    v = a.verb or "info"
    if v == "info": emit(cpu.info())
    elif v == "limits":
        if a.pl1 is None: emit(cpu.info())
        else: emit(cpu.set_limits(a.pl1, a.pl2 if a.pl2 is not None else a.pl1))
    elif v == "undervolt":
        if a.action == "get": emit({"plane": a.plane, "offset_mv": cpu.uv_get(a.plane)})
        elif a.action == "set":
            if a.mv is None: raise HwError("usage: truesense cpu undervolt set -10 --confirm")
            emit({"plane": a.plane, "offset_mv": cpu.uv_set(a.plane, a.mv, a.confirm)})
        else: emit(cpu.uv_reset())


def h_touchpad(a):
    from . import touchpad
    def set_(on):
        try: return _ipc().call("touchpad", action="on" if on else "off")
        except HwError:
            if os.geteuid() == 0: return touchpad.set_enabled(on)
            raise
    tri(a.action, lambda: touchpad.enabled(), set_, "touchpad")


def h_service(a):
    v = a.verb or "status"
    if v == "status": subprocess.run(["systemctl", "--no-pager", "status", "truesense.service"])
    elif v == "restart": subprocess.run(["sudo", "systemctl", "restart", "truesense.service"])
    elif v == "logs": subprocess.run(["journalctl", "-u", "truesense.service", "-b", "--no-pager", "-n", "60"])


def h_config(a):
    from . import config
    v = a.verb or "show"
    if v == "path": emit([config.SYSTEM_PATH, config.USER_PATH])
    elif v == "validate":
        p = config.validate(config.load()); emit(p, "configuration valid" if not p else "\n".join(p)); return 1 if p else 0
    else: emit(config.load(), config.dumps(config.load()))


def h_completion(a):
    from . import completion
    print(completion.generate(build(), a.shell))


def main(argv=None):
    global JSON
    argv = list(sys.argv[1:] if argv is None else argv)
    JSON = "--json" in argv; argv = [x for x in argv if x != "--json"]        # --json works anywhere on the line
    ap = build(); a = ap.parse_args(argv); a.json = JSON
    if not a.area: ap.print_help(); return 0
    try:
        if a.area == "doctor":
            from . import doctor; return doctor.run(JSON)
        if a.area == "selftest":
            from . import selftest; return 0 if selftest.run(a.skip_fans) else 1
        if a.area == "gui":
            from . import gui; return gui.main(a.page) or 0
        if a.area == "daemon":
            from . import daemon; daemon.main(); return 0
        if a.area == "version": emit({"truesense": __version__}, f"truesense {__version__}"); return 0
        r = globals()["h_" + a.area.replace("-", "_")](a)
        return r or 0
    except HwError as e:
        print(f"error: {e}", file=sys.stderr); return 1
    except KeyboardInterrupt: return 130
    except BrokenPipeError: return 0
