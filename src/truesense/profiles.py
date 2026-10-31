"""Named profiles: a saved bundle of settings applied in one go (CLI, PREDATOR key, GUI).

Files are plain TOML in ~/.config/truesense/profiles/ (yours) and <prefix>/share/truesense/profiles/ (shipped; yours win). Sections, all optional:

  description = "..."
  [power]    mode = "turbo"                                   quiet balanced performance turbo eco     auto = "off" | "native" | "smart"
  [fan]      mode = "smart" | "auto" | "max" | "manual"       manual also needs cpu = 60, gpu = 60 (20..100)     curve = "truesense" (smart fan curve name)
  [keyboard] on = true  effect = "wave" color = "00aec7" speed = 5 brightness = 30   or   profile = "<lighting profile>"   timeout = true
  [rear]     on = true  effect = "breathing" color = "ff0000" speed = 3 brightness = 100
  [battery]  limit = true   usb_charge = 30                   (0 10 20 30)
  [display]  overdrive = true
  [boot]     sound = false
  [audio]    output = "speakers" | "headphones"   voice_headset = "conference"   voice_array = "off"   mic_muted = false   dts_mode = "movie"
Applying a profile never changes the GPU mode (that needs a reboot) and never touches undervolt.
"""
import glob, os, tomllib
from . import audio, backend as hw, config, ipc, keyboard, userstate
from .device import HwError

USER_DIR = os.path.expanduser("~/.config/truesense/profiles")
SYS_DIRS = [os.environ["TRUESENSE_PROFILES_DIR"]] if os.environ.get("TRUESENSE_PROFILES_DIR") else ["/usr/local/share/truesense/profiles", "/usr/share/truesense/profiles"]


def _path(name):
    if not name or not all(c.isalnum() or c in "-_" for c in name): raise HwError("profile names: letters, digits, - and _")
    for d in [USER_DIR] + SYS_DIRS:
        if os.path.exists(f"{d}/{name}.toml"): return f"{d}/{name}.toml"
    return None


def names():
    seen = set()
    for d in [USER_DIR] + SYS_DIRS: seen |= {os.path.basename(p)[:-5] for p in glob.glob(d + "/*.toml")}
    return sorted(seen)


def load(name):
    p = _path(name)
    if not p: raise HwError(f"no profile '{name}' (truesense profile list)")
    with open(p, "rb") as f:
        try: return tomllib.load(f)
        except tomllib.TOMLDecodeError as e: raise HwError(f"{p}: {e}")


def source(name):
    p = _path(name); return "yours" if p and p.startswith(USER_DIR) else "shipped" if p else None


def save(name, data):
    if not name or not all(c.isalnum() or c in "-_" for c in name): raise HwError("profile names: letters, digits, - and _")
    os.makedirs(USER_DIR, exist_ok=True)
    with open(f"{USER_DIR}/{name}.toml", "w") as f: f.write(config.dumps(data))
    return f"{USER_DIR}/{name}.toml"


def delete(name):
    p = _path(name)
    if not p: raise HwError(f"no profile '{name}'")
    if not p.startswith(USER_DIR): raise HwError(f"'{name}' is a shipped profile; save your own copy with the same name to override it")
    os.remove(p)


def capture(description=""):
    """Current state as a profile (what can be read back; keyboard colours cannot, so only its brightness and timeout are captured)."""
    d = {"description": description or "captured"}
    d["power"] = {"mode": hw.get_mode()}
    fan = hw.fan_state()
    try: smart = bool(ipc.call("state")["settings"]["smart_fan"])
    except HwError: smart = False
    d["fan"] = {"mode": "smart" if smart else fan["mode"]}
    try: d["fan"]["curve"] = ipc.call("fan_curve", action="get")["name"]
    except HwError: pass
    try: d["power"]["auto"] = ipc.call("state")["settings"].get("auto_mode", "off")
    except HwError: pass
    if d["fan"]["mode"] == "manual": d["fan"].update(cpu=fan["duty_register_pct"]["cpu"], gpu=fan["duty_register_pct"]["gpu"])
    r = hw.rear_get(); d["rear"] = {"on": r["brightness"] > 0}
    if r["brightness"] > 0 and r["mode"] in hw.REAR_MODES: d["rear"].update(effect=r["mode"], color=r["color"], speed=r["speed"], brightness=r["brightness"])
    d["battery"] = {"limit": hw.get_battery_limit(), "usb_charge": hw.get_usb_charge()}
    d["display"] = {"overdrive": bool(hw.get_overdrive())}
    d["boot"] = {"sound": hw.get_boot_sound()}
    d["keyboard"] = {"timeout": bool(hw.get_kb_timeout())}
    try: d["audio"] = {"output": audio.output_status()["active"], "voice_headset": audio.voice_modes()["headset"], "voice_array": audio.voice_modes()["array"]}
    except HwError: pass
    return d


def _fan_manual(cpu, gpu):
    from . import control
    control.fan("manual", cpu, gpu)


def next_name():
    ns = names()
    if not ns: raise HwError("no profiles saved")
    cur = userstate.get("profile"); return ns[(ns.index(cur) + 1) % len(ns)] if cur in ns else ns[0]


def apply(name, cfg=None):
    from . import actions                                              # actions imports this module: import lazily
    cfg = cfg or config.load(); d = load(name); done, failed = 0, []

    def step(label, fn):
        nonlocal done
        try: fn(); done += 1
        except Exception as e: failed.append(f"{label}: {e}")

    p = d.get("power", {})
    if "auto" in p: step("power auto", lambda: ipc.call("set", key="auto_mode", value=p["auto"]))
    if "mode" in p: step("power", lambda: actions.run(f"power:{p['mode']}", cfg))
    f = d.get("fan", {})
    if "curve" in f:
        from . import curves
        step("fan curve", lambda: ipc.call("fan_curve", action="use", name=f["curve"], data=None if f["curve"] in curves.BUILTIN else curves.load(f["curve"])))
    if f.get("mode") == "manual": step("fan", lambda: _fan_manual(int(f["cpu"]), int(f["gpu"])))
    elif "mode" in f: step("fan", lambda: actions.run(f"fan:{f['mode']}", cfg))
    k = d.get("keyboard", {})
    if "profile" in k: step("keyboard profile", lambda: actions.run(f"keyboard-profile:{k['profile']}", cfg))
    elif "effect" in k: step("keyboard effect", lambda: keyboard.set_effect(k["effect"], k.get("color", "00aec7"), int(k.get("speed", 5)), int(k.get("brightness", keyboard.get_brightness() or 30))))
    elif "on" in k: step("keyboard", lambda: actions.run("keyboard:on" if k["on"] else "keyboard:off", cfg))
    if "timeout" in k: step("keyboard timeout", lambda: hw.set_kb_timeout(bool(k["timeout"])))
    r = d.get("rear", {})
    if "effect" in r: step("rear light", lambda: hw.rear_set(r["effect"], r.get("color", "ffffff"), int(r.get("speed", 3)), int(r.get("brightness", 100)), int(r.get("direction", 1))))
    elif "on" in r: step("rear light", lambda: actions.run("rear:on" if r["on"] else "rear:off", cfg))
    b = d.get("battery", {})
    if "limit" in b: step("battery limit", lambda: hw.set_battery_limit(bool(b["limit"])))
    if "usb_charge" in b: step("usb charge", lambda: hw.set_usb_charge(int(b["usb_charge"])))
    if "overdrive" in d.get("display", {}): step("overdrive", lambda: hw.set_overdrive(bool(d["display"]["overdrive"])))
    if "sound" in d.get("boot", {}): step("boot sound", lambda: hw.set_boot_sound(bool(d["boot"]["sound"])))
    a = d.get("audio", {})
    if "output" in a: step("audio output", lambda: audio.set_output(a["output"]))
    if "voice_headset" in a: step("voice (headset)", lambda: audio.voice_set("headset", a["voice_headset"]))
    if "voice_array" in a: step("voice (array)", lambda: audio.voice_set("array", a["voice_array"]))
    if "mic_muted" in a: step("microphone", lambda: audio.mic_set_mute(bool(a["mic_muted"])))
    if "dts_mode" in a: step("DTS mode", lambda: audio.dts_mode(a["dts_mode"]))
    userstate.set("profile", name)
    msg = f"Profile '{name}' applied ({done} settings" + (f", {len(failed)} FAILED: {'; '.join(failed)}" if failed else "") + ")"
    return msg
