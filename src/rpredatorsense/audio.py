"""Audio: mic mute (Fn+F8 = F15) and a "Purified Voice" equivalent (PipeWire filter-chain + RNNoise).

Acer's Purified Voice runs a proprietary Realtek neural model inside the Windows audio engine and cannot run on Linux.
This builds the open equivalent: a virtual source "Purified Voice Mic" (real mic -> RNNoise) and a virtual sink "Purified Voice Speaker".
Modes map to RNNoise voice-activity gating: off | conference (moderate) | personal (strict, focus on the near voice).
Needs the LADSPA plugin: pacman -S noise-suppression-for-voice.
"""
import os, re, subprocess
from .backend import HwError

CONF_DIR = os.path.expanduser("~/.config/pipewire/pipewire.conf.d")
MIC_CONF, SPK_CONF = f"{CONF_DIR}/rps-purified-voice-mic.conf", f"{CONF_DIR}/rps-purified-voice-speaker.conf"
PLUGIN = next((p for p in ("/usr/lib/ladspa/librnnoise_ladspa.so", "/usr/lib64/ladspa/librnnoise_ladspa.so") if os.path.exists(p)), None)
MODES = {"conference": (50, 20, 0), "personal": (85, 150, 100)}     # VAD threshold %, grace ms, retroactive grace ms


def _run(cmd):
    try: return subprocess.run(cmd, capture_output=True, text=True)
    except FileNotFoundError: return subprocess.CompletedProcess(cmd, 127, "", f"{cmd[0]}: not installed")

def mic_muted():
    return "[MUTED]" in _run(["wpctl", "get-volume", "@DEFAULT_AUDIO_SOURCE@"]).stdout

def mic_toggle():
    _run(["wpctl", "set-mute", "@DEFAULT_AUDIO_SOURCE@", "toggle"]); m = mic_muted()
    return "microphone muted" if m else "microphone live"

def _conf(kind, mode, target):
    thr, grace, retro = MODES[mode]; sink = kind == "speaker"
    name = "rps_purified_voice_" + kind
    tgt = f'\n                target.object = "{target}"' if target else ""
    if sink:
        cap = f'media.class = Audio/Sink\n                node.name = "{name}"\n                node.description = "Purified Voice Speaker"\n                audio.position = [ FL FR ]'
        play = f'node.passive = true{tgt}'
    else:
        cap = f'node.passive = true{tgt}'
        play = f'media.class = Audio/Source\n                node.name = "{name}"\n                node.description = "Purified Voice Mic"\n                audio.position = [ FL FR ]'
    label = "noise_suppressor_stereo"
    return f'''context.modules = [
  {{ name = libpipewire-module-filter-chain
    args = {{
      node.description = "Purified Voice {kind}"
      media.name = "Purified Voice {kind}"
      filter.graph = {{
        nodes = [ {{ type = ladspa name = rnnoise plugin = "{PLUGIN}" label = {label}
                    control = {{ "VAD Threshold (%)" = {thr} "VAD Grace Period (ms)" = {grace} "Retroactive VAD Grace (ms)" = {retro} }} }} ]
      }}
      capture.props = {{
                {cap}
      }}
      playback.props = {{
                {play}
      }}
    }}
  }}
]
'''

def _reload():
    _run(["systemctl", "--user", "restart", "pipewire.service"])

def default_target():
    """Source to clean: the headset mic (Mic2) when a headset is plugged in, else the internal mic array (Mic1)."""
    names = [l.split()[1] for l in _run(["pactl", "list", "short", "sources"]).stdout.splitlines() if len(l.split()) > 1 and "monitor" not in l]
    want = "Mic2__source" if jack_state() == "on" else "Mic1__source"
    return next((n for n in names if n.endswith(want)), None)

def set_mic(mode, target=None):
    if mode == "off":
        if os.path.exists(MIC_CONF): os.remove(MIC_CONF); _reload()
        return "off"
    if mode not in MODES: raise HwError("mic mode: off | conference | personal")
    if not PLUGIN: raise HwError("RNNoise LADSPA plugin missing: sudo pacman -S noise-suppression-for-voice")
    target = target or default_target()
    os.makedirs(CONF_DIR, exist_ok=True); open(MIC_CONF, "w").write(_conf("mic", mode, target)); _reload(); return f"{mode} (cleaning {target})"

def set_speaker(on, mode="conference", target=None):
    if not on:
        if os.path.exists(SPK_CONF): os.remove(SPK_CONF); _reload()
        return "off"
    if not PLUGIN: raise HwError("RNNoise LADSPA plugin missing: sudo pacman -S noise-suppression-for-voice")
    os.makedirs(CONF_DIR, exist_ok=True); open(SPK_CONF, "w").write(_conf("speaker", mode, target)); _reload(); return "on"

def jack_state():
    out = _run(["amixer", "-c1", "cget", "iface=CARD,name=Headset Mic Jack"]).stdout.strip().splitlines()
    return out[-1].strip().replace(": values=", "") if out else "unknown"

def status():
    return {"mic_muted": mic_muted(), "headset_mic_jack_plugged": jack_state(), "rnnoise_plugin": PLUGIN or "MISSING (pacman -S noise-suppression-for-voice)",
            "purified_mic": "on" if os.path.exists(MIC_CONF) else "off", "purified_speaker": "on" if os.path.exists(SPK_CONF) else "off"}


# ---- output switching (speakers <-> headphones while a jack is plugged in) --------------------------------------------------------
CARD_MATCH = "pci-0000_00_1f.3-platform-skl_hda_dsp_generic"
PROFILE = {"speakers": "HiFi (Mic1, Mic2, Speaker)", "headphones": "HiFi (Headphones, Mic1, Mic2)"}

def _card():
    return next((l.split("\t")[1] for l in _run(["pactl", "list", "short", "cards"]).stdout.splitlines() if CARD_MATCH in l), None)

def output_status():
    card = _card()
    if not card: raise HwError("sound card not found")
    prof = next((l.split(":", 1)[1].strip() for l in _run(["pactl", "list", "cards"]).stdout.split("Name: " + card, 1)[1].splitlines() if "Active Profile:" in l), "?")
    return {"active": next((k for k, v in PROFILE.items() if v == prof), prof), "jack_plugged": amixer_on("Headphone Jack")}

def amixer_on(name):
    out = _run(["amixer", "-c1", "cget", f"iface=CARD,name={name}"]).stdout.strip().splitlines()
    return bool(out) and out[-1].strip().endswith("values=on")

import contextlib, time as _time
_osd_depth = 0

@contextlib.contextmanager
def quiet_osd(settle=0.8):
    """Silence Noctalia's automatic OSDs while the card profile flips: PipeWire removes/recreates the mic and speaker nodes and
    re-announces their volumes, which Noctalia would turn into a pile of popups. Always re-enabled afterwards (OSDs are on by default)."""
    global _osd_depth
    _osd_depth += 1
    if _osd_depth == 1: _run(["noctalia", "msg", "osd-disable"])
    try: yield
    finally:
        if _osd_depth == 1:
            _time.sleep(settle)                    # let the late PipeWire events pass while still silent
            _run(["noctalia", "msg", "osd-enable"])
        _osd_depth -= 1

def announce(label, icon="headphones"):
    """Instant feedback at key press. Hyprland's own notification is drawn by the compositor immediately (no PipeWire events involved, not affected by
    osd-disable). RPS_OSD=noctalia keeps the old Noctalia popup (~1 s later); RPS_OSD=none shows nothing."""
    mode = os.environ.get("RPS_OSD", "native")
    if mode == "none": return
    if mode == "native" and _run(["hyprctl", "notify", "1", "1700", "rgb(00aec7)", f"Audio output: {label}"]).stdout.strip() == "ok": return
    if mode == "native" and _run(["notify-send", "-t", "1700", "rpredatorsense", f"Audio output: {label}"]).returncode == 0: return
    show_osd(icon, label)

def show_osd(icon, text):
    _run(["noctalia", "msg", "osd-show", icon, text])

def set_output(which):
    """Speakers while the aux jack is plugged in (the UCM profiles are mutually exclusive, so we switch the card profile)."""
    if which not in PROFILE: raise HwError("output: speakers | headphones")
    card = _card()
    if not card: raise HwError("sound card not found")
    with quiet_osd():
        _run(["pactl", "set-card-profile", card, PROFILE[which]])
        want = "Speaker__sink" if which == "speakers" else "Headphones__sink"
        for _ in range(100):                       # wait until the new sink exists (measured: ~0.04 s) instead of a fixed sleep
            if want in _run(["pactl", "list", "short", "sinks"]).stdout: break
            _time.sleep(0.03)
        got = output_status()["active"]
    if got != which: raise HwError(f"profile readback {got} != {which}")
    return got


def _default_sink(): return _run(["pactl", "get-default-sink"]).stdout.strip()

def _sinks(): return [l.split("\t")[1] for l in _run(["pactl", "list", "short", "sinks"]).stdout.splitlines() if len(l.split("\t")) > 1]

def _sink_desc(name):
    blk = _run(["pactl", "list", "sinks"]).stdout.split("Name: " + name, 1)
    return next((l.split(":", 1)[1].strip() for l in (blk[1].splitlines() if len(blk) > 1 else []) if l.strip().startswith("Description:")), name)

def cycle_output():
    """F5-style round robin: laptop speakers -> headphones (only if the jack is plugged) -> other devices; flips the card profile when needed.
    The AcerUltraX virtual sink (if present) stays the default so the DTS processing keeps working; it follows the hardware output."""
    import fcntl
    with open("/tmp/rpredatorsense-output.lock", "w") as lk:          # two quick presses must not interleave (one could re-enable popups mid-flip)
        fcntl.flock(lk, fcntl.LOCK_EX)
        with quiet_osd():
            return _cycle_output()

def _cycle_output():
    sinks = [n for n in _sinks() if "hdmi" not in n.lower()]
    front = "acerultrax_sink" if "acerultrax_sink" in sinks else None
    others = [n for n in sinks if CARD_MATCH not in n and n != "acerultrax_sink"]
    plugged = amixer_on("Headphone Jack")
    stops = ["int:speakers"] + (["int:headphones"] if plugged else []) + others
    cur = _default_sink()
    if cur == "acerultrax_sink" or CARD_MATCH in cur:
        active = output_status()["active"]; cur = "int:" + (active if active in PROFILE else "speakers")
    i = stops.index(cur) if cur in stops else -1
    nxt = stops[(i + 1) % len(stops)]
    if nxt.startswith("int:"):
        which = nxt[4:]
        label = "Laptop speakers" if which == "speakers" else "Headphones (3.5 mm jack)"
        icon = "device-speaker" if which == "speakers" else "headphones"
    else:
        label = _sink_desc(nxt); icon = "headphones"
    announce(label, icon)                          # instant, before any work
    if nxt.startswith("int:"):
        set_output(which)
        hw = next((n for n in _sinks() if CARD_MATCH in n and ("Speaker" if which == "speakers" else "Headphones") in n), None)
        target = front or hw
    else:
        target = nxt
    if not target: raise HwError("no sink to switch to")
    _run(["pactl", "set-default-sink", target])
    for l in _run(["pactl", "list", "short", "sink-inputs"]).stdout.splitlines():
        _run(["pactl", "move-sink-input", l.split()[0], target])
    return label, icon


# ---- Purified Voice, per input device (no PipeWire restart on plug/unplug) -----------------------------------------------------------
PV_DEV = {"headset": ("Mic2__source", "Purified Voice (headset mic)"), "array": ("Mic1__source", "Purified Voice (mic array)")}
MODE_LABEL = {"off": "All Off", "personal": "Personal Call", "conference": "Conference Call"}

def _pv_conf_path(dev): return f"{CONF_DIR}/rps-pv-{dev}.conf"
def _pv_source(dev): return f"alsa_input.{CARD_MATCH}.HiFi__{PV_DEV[dev][0]}"
def _pv_node(dev): return f"rps_pv_{dev}"

def _pv_conf(dev, mode):
    thr, grace, retro = MODES[mode]
    return f'''context.modules = [
  {{ name = libpipewire-module-filter-chain
    args = {{
      node.description = "{PV_DEV[dev][1]}"
      media.name = "{PV_DEV[dev][1]}"
      filter.graph = {{
        nodes = [ {{ type = ladspa name = rnnoise plugin = "{PLUGIN}" label = noise_suppressor_stereo
                    control = {{ "VAD Threshold (%)" = {thr} "VAD Grace Period (ms)" = {grace} "Retroactive VAD Grace (ms)" = {retro} }} }} ]
      }}
      capture.props = {{
                node.passive = true
                target.object = "{_pv_source(dev)}"
      }}
      playback.props = {{
                media.class = Audio/Source
                node.name = "{_pv_node(dev)}"
                node.description = "{PV_DEV[dev][1]}"
                audio.position = [ FL FR ]
      }}
    }}
  }}
]
'''

def pv_set(dev, mode):
    """Configure one input's filter (restarts PipeWire once, at configuration time only)."""
    if dev not in PV_DEV: raise HwError("device: headset | array")
    if mode == "off":
        if os.path.exists(_pv_conf_path(dev)): os.remove(_pv_conf_path(dev)); _reload()
        return "off"
    if mode not in MODES: raise HwError("mode: off | conference | personal")
    if not PLUGIN: raise HwError("RNNoise LADSPA plugin missing: sudo pacman -S noise-suppression-for-voice")
    os.makedirs(CONF_DIR, exist_ok=True); open(_pv_conf_path(dev), "w").write(_pv_conf(dev, mode)); _reload()
    return mode

def pv_modes():
    out = {}
    for dev in PV_DEV:
        p = _pv_conf_path(dev)
        if not os.path.exists(p): out[dev] = "off"; continue
        thr = int(re.search(r'"VAD Threshold \(%\)" = (\d+)', open(p).read()).group(1)); out[dev] = next((m for m, v in MODES.items() if v[0] == thr), "?")
    return out

def _node_exists(name): return any(l.split("\t")[1:2] == [name] for l in _run(["pactl", "list", "short", "sources"]).stdout.splitlines())

def _announce_mic(text):
    if _run(["hyprctl", "notify", "1", "2200", "rgb(00aec7)", text]).stdout.strip() != "ok":
        _run(["notify-send", "-t", "2200", "rpredatorsense", text])

def apply_mic_policy(notify=True, settings=None):
    """Point the default microphone at the right input: headset plugged -> headset filter (or raw headset mic); else the array filter (or raw array)."""
    plugged = amixer_on("Headset Mic Jack")
    dev = "headset" if plugged else "array"
    mode = (settings or pv_modes()).get(dev, "off")
    want = _pv_node(dev) if mode != "off" and _node_exists(_pv_node(dev)) else _pv_source(dev)
    if _run(["pactl", "get-default-source"]).stdout.strip() != want:
        _run(["pactl", "set-default-source", want])
        for l in _run(["pactl", "list", "short", "source-outputs"]).stdout.splitlines(): _run(["pactl", "move-source-output", l.split()[0], want])
    if notify: _announce_mic(f"Microphone: {'Headset' if plugged else 'Mic array'} - {MODE_LABEL[mode]}")
    return {"device": dev, "mode": mode, "default_source": want}

def watch_mic(cfg=None):
    """Edge-triggered: only reacts when the headset is plugged/unplugged (never fights a manual Shift+F5 choice in between)."""
    import time
    a = (cfg or {}).get("audio", {}); notify = a.get("notify", True)
    last = None
    while True:
        cur = amixer_on("Headset Mic Jack")
        if cur != last:
            if last is not None or a.get("apply_at_start", True):
                time.sleep(0.8)                                  # let PipeWire create/remove the headset source first
                try: print(apply_mic_policy(notify), flush=True)
                except Exception as e: print("mic policy failed:", e, flush=True)
            last = cur
        time.sleep(1.0)
