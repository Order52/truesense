"""Actions: the one vocabulary shared by the PREDATOR key, saved profiles and `truesense keys test`.

An action is `name` or `name:argument`. Each returns a short message (shown on screen by the caller). Unknown names are rejected when configuring, never at key-press time.

  none
  power-next | power-prev | power:<mode> | power-cycle:<mode,mode,...>     eco quiet balanced performance turbo (a manual choice holds for [profile] hold_minutes)
  fan:smart | fan:auto | fan:max | fan:<percent> | fan-max-toggle | smart-fan-toggle
  profile:<name> | profile-next                                              saved profiles (see `truesense profile`)
  output-toggle | output-cycle | output:speakers | output:headphones
  mic-toggle | mic:mute | mic:unmute | voice-next
  touchpad-toggle | touchpad:on | touchpad:off
  keyboard:on | keyboard:off | keyboard:toggle | keyboard-effect-next | keyboard-profile:<name>
  rear:on | rear:off | rear:toggle | rear-effect-next
  battery-limit-toggle | overdrive-toggle
  look:<name> | auto:<off|native|smart> | auto-next                           lighting looks (truesense look), the mode control
  exec:<shell command>      runs as you, detached      |      notify:<text>
"""
import os, shlex, subprocess
from . import audio, backend as hw, config, control, ipc, keyboard, keyprofiles, osd, profiles, touchpad, userstate
from .device import HwError

KB_EFFECTS = ["static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"]
REAR_EFFECTS = ["static", "breathing", "neon", "wave", "twinkling"]
FIXED = {"auto-next", "none", "power-next", "power-prev", "fan-max-toggle", "smart-fan-toggle", "profile-next", "output-toggle", "output-cycle", "mic-toggle", "voice-next",
         "touchpad-toggle", "keyboard-effect-next", "rear-effect-next", "battery-limit-toggle", "overdrive-toggle"}
WITH_ARG = {"look", "auto", "power", "power-cycle", "fan", "profile", "output", "mic", "touchpad", "keyboard", "keyboard-profile", "rear", "exec", "notify"}


def split(spec):
    name, _, arg = spec.strip().partition(":"); return name.strip(), arg.strip()


def validate(spec):
    """Return None if the action is valid, else a message. Used when configuring keys and profiles."""
    name, arg = split(spec)
    if name in FIXED: return None if not arg else f"'{name}' takes no argument"
    if name not in WITH_ARG: return f"unknown action '{name}'"
    if not arg: return f"'{name}' needs an argument (e.g. {name}:...)"
    if name == "auto" and arg not in ("off", "native", "smart"): return "auto: off | native | smart"
    if name == "look":
        from . import looks
        if arg not in looks.names(): return f"no saved look '{arg}' (truesense look list)"
    if name == "power" and arg not in hw.MODES: return f"power mode must be one of {', '.join(hw.MODES)}"
    if name == "power-cycle" and any(m not in hw.MODES for m in arg.split(",")): return "power-cycle needs a comma list of power modes"
    if name == "fan" and arg not in ("smart", "auto", "max") and not (arg.isdigit() and 20 <= int(arg) <= 100): return "fan: smart | auto | max | 20..100"
    if name == "output" and arg not in ("speakers", "headphones"): return "output: speakers | headphones"
    if name == "mic" and arg not in ("mute", "unmute"): return "mic: mute | unmute"
    if name == "touchpad" and arg not in ("on", "off"): return "touchpad: on | off"
    if name in ("keyboard", "rear") and arg not in ("on", "off", "toggle"): return f"{name}: on | off | toggle"
    return None


def _hold(cfg):
    try: ipc.call("hold", minutes=cfg["profile"]["hold_minutes"])
    except HwError: pass                                   # no daemon: nothing to hold against


def _daemon_set(key, value):
    try: ipc.call("set", key=key, value=value); return True
    except HwError: return False


def _smart_on():
    try: return bool(ipc.call("state")["settings"]["smart_fan"])
    except HwError: return False


def _touch(action):
    try: return ipc.call("touchpad", action=action)
    except HwError:
        if os.geteuid() == 0: return touchpad.set_enabled({"on": True, "off": False, "toggle": not touchpad.enabled()}[action])
        raise


def run(spec, cfg=None):
    cfg = cfg or config.load(); name, arg = split(spec); err = validate(spec)
    if err: raise HwError(err)
    cycle = [m for m in cfg["keys"]["power_cycle"] if m in hw.supported_modes()] or ["eco", "balanced", "turbo"]

    if name == "none": return ""
    if name in ("power-next", "power-prev", "power-cycle"):
        order = [m for m in arg.split(",")] if name == "power-cycle" else cycle
        cur = hw.get_mode(); i = order.index(cur) if cur in order else -1
        new = control.power(order[(i + (-1 if name == "power-prev" else 1)) % len(order)], cfg["profile"]["hold_minutes"]); return f"Power mode: {new}"
    if name == "power": new = control.power(arg, cfg["profile"]["hold_minutes"]); return f"Power mode: {new}"
    if name == "fan":
        if arg == "smart": control.fan("smart"); return "Fans: smart curve"
        if arg == "auto": control.fan("auto"); return "Fans: firmware auto"
        if arg == "max": control.fan("max"); return "Fans: MAX"
        control.fan("manual", int(arg)); return f"Fans: {arg} %"
    if name == "look":
        from . import looks
        looks.apply(looks.load(arg)); return f"Look: {arg}"
    if name == "auto":
        ipc.call("set", key="auto_mode", value=arg); return f"Mode control: {arg}"
    if name == "auto-next":
        cur = ipc.call("state")["settings"].get("auto_mode", "off"); nxt = {"off": "native", "native": "smart", "smart": "off"}.get(cur, "smart")
        ipc.call("set", key="auto_mode", value=nxt); return f"Mode control: {nxt}"
    if name == "fan-max-toggle":
        if hw.fan_state()["mode"] == "max": control.fan("smart"); return "Fans: smart curve"
        control.fan("max"); return "Fans: MAX"
    if name == "smart-fan-toggle":
        on = not _smart_on()
        if not _daemon_set("smart_fan", on): raise HwError("the TrueSense daemon is not running")
        return "Fans: smart curve" if on else "Fans: smart curve OFF"
    if name == "profile": return profiles.apply(arg, cfg)
    if name == "profile-next": return profiles.apply(profiles.next_name(), cfg)
    if name == "output-toggle": audio.output_toggle(); return ""
    if name == "output-cycle": audio.output_cycle(); return ""
    if name == "output": audio.set_output(arg); return f"Audio output: {audio.LABEL[arg]}"
    if name == "mic-toggle": m = audio.mic_toggle(); return "Microphones: MUTED" if m else "Microphones: live"
    if name == "mic": audio.mic_set_mute(arg == "mute"); return "Microphones: MUTED" if arg == "mute" else "Microphones: live"
    if name == "voice-next": dev, m = audio.voice_next(); return f"Purified Voice ({dev}): {audio.MODE_LABEL[m]}"
    if name == "touchpad-toggle": return "Touchpad: " + ("on" if _touch("toggle") else "off")
    if name == "touchpad": return "Touchpad: " + ("on" if _touch(arg) else "off")
    if name == "keyboard":
        cur = userstate.get("keyboard_on", True); new = (not cur) if arg == "toggle" else arg == "on"
        if new:
            if userstate.get("kb_restore"): keyboard.on()
            else: keyprofiles.load(userstate.get("keyboard_profile", "default"), None) if keyprofiles.exists(userstate.get("keyboard_profile", "default")) else keyboard.restore_default(None)
        else: keyboard.off()
        userstate.set("keyboard_on", new); return "Keyboard light: " + ("on" if new else "off")
    if name == "keyboard-effect-next":
        i = (userstate.get("kb_effect", -1) + 1) % len(KB_EFFECTS); userstate.set("kb_effect", i); b = keyboard.get_brightness() or 30
        keyboard.set_effect(KB_EFFECTS[i], "00aec7", 5, max(b, 30 if KB_EFFECTS[i] == "neon" else b)); userstate.set("keyboard_on", True); return f"Keyboard effect: {KB_EFFECTS[i]}"
    if name == "keyboard-profile": keyprofiles.load(arg, None); userstate.set("keyboard_profile", arg); userstate.set("keyboard_on", True); return f"Keyboard profile: {arg}"
    if name == "rear":
        cur = hw.rear_get(); on = cur["brightness"] > 0; new = (not on) if arg == "toggle" else arg == "on"
        if on and not new: userstate.set("rear_last", {k: cur[k] for k in ("mode", "color", "speed", "brightness", "direction")})
        last = userstate.get("rear_last") or {"mode": "static", "color": "00aec7", "speed": 3, "brightness": 100, "direction": 1}
        zs = userstate.get("rear_zones")                      # keep the three part colours of the static effect through on/off
        def zfor(mode): return zs if (mode == "static" and isinstance(zs, list) and len(zs) == 3) else None
        if new: m = last["mode"] if last["mode"] in hw.REAR_MODES else "static"; hw.rear_set(m, last["color"], last["speed"], last["brightness"] or 100, last["direction"], zfor(m))
        else: m = cur["mode"] if cur["mode"] in hw.REAR_MODES else "static"; hw.rear_set(m, cur["color"], cur["speed"], 0, cur["direction"], zfor(m))
        return "Rear light: " + ("on" if new else "off")
    if name == "rear-effect-next":
        i = (userstate.get("rear_effect", -1) + 1) % len(REAR_EFFECTS); userstate.set("rear_effect", i); cur = hw.rear_get()
        hw.rear_set(REAR_EFFECTS[i], cur["color"], cur["speed"], cur["brightness"] or 100, cur["direction"]); return f"Rear effect: {REAR_EFFECTS[i]}"
    if name == "battery-limit-toggle": v = hw.set_battery_limit(not hw.get_battery_limit()); return "Battery 80 % limit: " + ("on" if v else "off")
    if name == "overdrive-toggle": v = hw.set_overdrive(not hw.get_overdrive()); return "LCD overdrive: " + ("on" if v else "off")
    if name == "notify": return arg
    if name == "exec":
        subprocess.Popen(arg, shell=True, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True); return ""
    raise HwError(f"unhandled action {name}")        # unreachable: validate() covers every name


def catalogue():
    return __doc__.split("\n\n", 1)[1].strip("\n")
