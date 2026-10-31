"""Audio: output switching, microphone, headset jack, Purified Voice (RNNoise on PipeWire) and the DTS sound (AcerUltraX wrapper).

Acer's Purified Voice runs a proprietary Realtek model that cannot run on Linux. This builds an open equivalent: one PipeWire filter chain per input
(headset mic, mic array) with RNNoise voice-activity gating. Modes: off | conference (gentle) | personal (strict; can clip very soft words).
"""
import fcntl, os, re, shutil, subprocess, time
from . import util
from . import config, osd
from .device import HwError

CONF_DIR = os.path.expanduser("~/.config/pipewire/pipewire.conf.d")
PLUGIN = next((p for p in ("/usr/lib/ladspa/librnnoise_ladspa.so", "/usr/lib64/ladspa/librnnoise_ladspa.so", "/usr/lib/x86_64-linux-gnu/ladspa/librnnoise_ladspa.so") if os.path.exists(p)), None)
MODES = {"conference": (50, 20, 0), "personal": (85, 150, 100)}                 # VAD threshold %, grace ms, retroactive grace ms
MODE_LABEL = {"off": "All Off", "personal": "Personal Call", "conference": "Conference Call"}
CARD_MATCH = "pci-0000_00_1f.3-platform-skl_hda_dsp_generic"
PROFILE = {"speakers": "HiFi (Mic1, Mic2, Speaker)", "headphones": "HiFi (Headphones, Mic1, Mic2)"}
PV_DEV = {"headset": ("Mic2__source", "Purified Voice (headset mic)"), "array": ("Mic1__source", "Purified Voice (mic array)")}


def _run(cmd):
    try: return subprocess.run(cmd, capture_output=True, text=True)
    except FileNotFoundError: return subprocess.CompletedProcess(cmd, 127, "", f"{cmd[0]}: not installed")

def _cfg(): return config.load()
def _say(text, icon="audio"): return osd.show(text, icon, _cfg())


# ---- ALSA helpers ---------------------------------------------------------------------------------------------------------------------
def card_index():
    for l in util.read("/proc/asound/cards").splitlines():
        if "sof-hda-dsp" in l: return l.split()[0]
    return "1"

def _amixer(*a): return _run(["amixer", "-c" + card_index(), *a]).stdout

def _ctl(iface, name):
    out = _amixer("cget", f"iface={iface},name={name}").strip().splitlines()
    return out[-1].strip().replace(": values=", "") if out else None

def jack_status():
    """What the codec sees on the 3.5 mm jack: headphones plugged, and whether a headset microphone was detected."""
    hp, mic = _ctl("CARD", "Headphone Jack"), _ctl("CARD", "Headset Mic Jack")
    return {"headphones": hp == "on", "headset_mic": mic == "on", "raw": {"Headphone Jack": hp, "Headset Mic Jack": mic}}

def jack_use(status=None):
    """Which microphone the 3.5 mm jack is treated as: the setting `[audio] jack` = auto (what the codec detects), headphones (never use the headset mic), headset (always use it)."""
    pref = _cfg()["audio"]["jack"]
    if pref == "headset": return "headset"
    if pref == "headphones": return "array"
    return "headset" if (status or jack_status())["headset_mic"] else "array"

def amixer_on(name): return _ctl("CARD", name) == "on"

def headset_boost(level=None):
    """Headset microphone boost (0..3, +10 dB per step). Without an argument: read."""
    name = "Headset Mic Boost Volume"
    if level is None:
        m = re.search(r"^\s*: values=(\d+)", _amixer("cget", f"iface=MIXER,name={name}"), re.M); return int(m.group(1)) if m else None      # the ": values=" line, not the "values=2" channel count in the header
    if not 0 <= level <= 3: raise HwError("headset mic boost is 0..3")
    _amixer("cset", f"iface=MIXER,name={name}", f"{level},{level}")
    if headset_boost() != level: raise HwError("boost readback mismatch")
    return level


# ---- output switching (speakers <-> headphones while the aux jack is plugged in) ----------------------------------------------------------
def _card():
    return next((l.split("\t")[1] for l in _run(["pactl", "list", "short", "cards"]).stdout.splitlines() if CARD_MATCH in l), None)

def output_status():
    card = _card()
    if not card: raise HwError("sound card not found")
    blk = _run(["pactl", "list", "cards"]).stdout.split("Name: " + card, 1)[1]
    prof = next((l.split(":", 1)[1].strip() for l in blk.splitlines() if "Active Profile:" in l), "?")
    return {"active": next((k for k, v in PROFILE.items() if v == prof), prof), "jack_plugged": jack_status()["headphones"]}

def set_output(which):
    """Speakers and headphones share one playback PCM, so ALSA offers them as two exclusive card profiles; flipping the profile rebinds the stream."""
    if which not in PROFILE: raise HwError("output: speakers | headphones")
    card = _card()
    if not card: raise HwError("sound card not found")
    with osd.Quiet():
        _run(["pactl", "set-card-profile", card, PROFILE[which]])
        want = "Speaker__sink" if which == "speakers" else "Headphones__sink"
        for _ in range(100):                                   # wait until the new sink exists (measured: ~0.04 s) instead of a fixed sleep
            if want in _run(["pactl", "list", "short", "sinks"]).stdout: break
            time.sleep(0.03)
        got = output_status()["active"]
    if got != which: raise HwError(f"profile readback {got} != {which}")
    return got

def _sinks(): return [l.split("\t")[1] for l in _run(["pactl", "list", "short", "sinks"]).stdout.splitlines() if len(l.split("\t")) > 1]
def _default_sink(): return _run(["pactl", "get-default-sink"]).stdout.strip()

def _sink_desc(name):
    blk = _run(["pactl", "list", "sinks"]).stdout.split("Name: " + name, 1)
    return next((l.split(":", 1)[1].strip() for l in (blk[1].splitlines() if len(blk) > 1 else []) if l.strip().startswith("Description:")), name)

LABEL = {"speakers": "Laptop speakers", "headphones": "Headphones (3.5 mm jack)"}
ICON = {"speakers": "speaker", "headphones": "audio"}

def output_toggle():
    cur = output_status()["active"]; new = "headphones" if cur == "speakers" else "speakers"
    cfg = _cfg(); noct = (os.environ.get("TRUESENSE_OSD") or cfg["osd"]["style"]) == "noctalia"
    if not noct: _say(f"Audio output: {LABEL[new]}", ICON[new])        # instant, before the ~1 s switch
    set_output(new)
    if noct: _say(LABEL[new], ICON[new])
    return new

def output_cycle():
    """F5-style round robin: laptop speakers -> headphones (only if the jack is plugged) -> other devices; flips the card profile when needed.
    The AcerUltraX virtual sink (if present) stays the default so the DTS processing keeps working; it follows the hardware output."""
    with open(f"/tmp/truesense-output-{os.getuid()}.lock", "w") as lk:
        fcntl.flock(lk, fcntl.LOCK_EX)                         # two quick presses must not interleave
        with osd.Quiet(): return _output_cycle()

def _output_cycle():
    sinks = [n for n in _sinks() if "hdmi" not in n.lower()]
    front = "acerultrax_sink" if "acerultrax_sink" in sinks else None
    others = [n for n in sinks if CARD_MATCH not in n and n != "acerultrax_sink"]
    stops = ["int:speakers"] + (["int:headphones"] if jack_status()["headphones"] else []) + others
    cur = _default_sink()
    if cur == "acerultrax_sink" or CARD_MATCH in cur: cur = "int:" + (output_status()["active"] if output_status()["active"] in PROFILE else "speakers")
    i = stops.index(cur) if cur in stops else -1
    nxt = stops[(i + 1) % len(stops)]
    if nxt.startswith("int:"): which = nxt[4:]; label, icon = LABEL[which], ICON[which]
    else: label, icon = _sink_desc(nxt), "audio"
    cfg = _cfg(); noct = (os.environ.get("TRUESENSE_OSD") or cfg["osd"]["style"]) == "noctalia"
    if not noct: _say(f"Audio output: {label}", icon)
    if nxt.startswith("int:"):
        set_output(which)
        hw = next((n for n in _sinks() if CARD_MATCH in n and ("Speaker" if which == "speakers" else "Headphones") in n), None)
        target = front or hw
    else: target = nxt
    if not target: raise HwError("no sink to switch to")
    _run(["pactl", "set-default-sink", target])
    for l in _run(["pactl", "list", "short", "sink-inputs"]).stdout.splitlines(): _run(["pactl", "move-sink-input", l.split()[0], target])
    if noct: _say(label, icon)
    return label


# ---- microphone ---------------------------------------------------------------------------------------------------------------------------
def _sources(): return [l.split("\t")[:2] for l in _run(["pactl", "list", "short", "sources"]).stdout.splitlines() if "monitor" not in l and len(l.split("\t")) > 1]

def mic_state():
    d = _run(["pactl", "get-default-source"]).stdout.strip()
    out = {"default": d, "muted": "yes" in _run(["pactl", "get-source-mute", d]).stdout, "sources": {}}
    for _id, name in _sources(): out["sources"][name.split("HiFi__")[-1]] = "yes" in _run(["pactl", "get-source-mute", name]).stdout
    v = re.search(r"(\d+)%", _run(["pactl", "get-source-volume", d]).stdout); out["volume"] = int(v.group(1)) if v else None
    return out

def mic_set_mute(muted, all_inputs=True):
    """Mute/unmute the default microphone, or (default) EVERY input: internal array, headset, USB, Bluetooth."""
    targets = [n for _i, n in _sources()] if all_inputs else [_run(["pactl", "get-default-source"]).stdout.strip()]
    for n in targets: _run(["pactl", "set-source-mute", n, "1" if muted else "0"])
    return muted

def mic_toggle(all_inputs=True):
    return mic_set_mute(not mic_state()["muted"], all_inputs)

def mic_volume(percent=None):
    if percent is None: return mic_state()["volume"]
    if not 0 <= percent <= 150: raise HwError("mic volume is 0..150 %")
    _run(["wpctl", "set-volume", "@DEFAULT_AUDIO_SOURCE@", f"{percent}%"]); return mic_state()["volume"]


# ---- Purified Voice, per input device (no PipeWire restart on plug/unplug) ---------------------------------------------------------------
def _conf_path(dev): return f"{CONF_DIR}/truesense-pv-{dev}.conf"
def _pv_source(dev): return f"alsa_input.{CARD_MATCH}.HiFi__{PV_DEV[dev][0]}"
def _pv_node(dev): return f"truesense_pv_{dev}"

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

def _reload(): _run(["systemctl", "--user", "restart", "pipewire.service"])

def voice_set(dev, mode):
    """Configure one input's filter (restarts PipeWire once, at configuration time only)."""
    if dev not in PV_DEV: raise HwError("device: headset | array")
    if mode == "off":
        if os.path.exists(_conf_path(dev)): os.remove(_conf_path(dev)); _reload(); _wait_node(dev, False)
        return "off"
    if mode not in MODES: raise HwError("mode: off | conference | personal")
    if not PLUGIN: raise HwError("RNNoise LADSPA plugin missing (Arch: sudo pacman -S noise-suppression-for-voice)")
    os.makedirs(CONF_DIR, exist_ok=True)
    new = _pv_conf(dev, mode)
    if not (os.path.exists(_conf_path(dev)) and util.read(_conf_path(dev)) == new): util.write(_conf_path(dev), new); _reload(); _wait_node(dev, True)
    return mode

def voice_modes():
    out = {}
    for dev in PV_DEV:
        p = _conf_path(dev)
        if not os.path.exists(p): out[dev] = "off"; continue
        m = re.search(r'"VAD Threshold \(%\)" = (\d+)', util.read(p)); thr = int(m.group(1)) if m else -1
        out[dev] = next((k for k, v in MODES.items() if v[0] == thr), "?")
    return out

def voice_enable(on):
    """on: restore the configured modes (config [audio] headset_mode / array_mode); off: both inputs unfiltered."""
    a = _cfg()["audio"]
    voice_set("headset", a["headset_mode"] if on else "off"); voice_set("array", a["array_mode"] if on else "off")
    return voice_modes()

def voice_next(dev=None):
    """Cycle the current input's mode: off -> conference -> personal -> off."""
    dev = dev or ("headset" if jack_status()["headset_mic"] else "array")
    cur = voice_modes()[dev]; new = {"off": "conference", "conference": "personal", "personal": "off"}.get(cur, "conference")
    voice_set(dev, new); return dev, new

def _source_names(): return {l.split("\t")[1] for l in _run(["pactl", "list", "short", "sources"]).stdout.splitlines() if "\t" in l}
def _node_exists(name): return name in _source_names()

def _wait_node(dev, present, timeout=8.0):
    """After a PipeWire reload the filter node takes a moment to appear (or vanish)."""
    end = time.time() + timeout
    while time.time() < end:
        if _node_exists(_pv_node(dev)) == present: return True
        time.sleep(0.2)
    return False

def apply_mic_policy(notify=True):
    """Point the default microphone at the right input: headset plugged -> headset filter (or raw headset mic); else the array filter (or raw array)."""
    dev = jack_use(); plugged = dev == "headset"; mode = voice_modes().get(dev, "off")
    want = _pv_node(dev) if mode != "off" and _node_exists(_pv_node(dev)) else _pv_source(dev)
    if _run(["pactl", "get-default-source"]).stdout.strip() != want:
        _run(["pactl", "set-default-source", want])
        for l in _run(["pactl", "list", "short", "source-outputs"]).stdout.splitlines(): _run(["pactl", "move-source-output", l.split()[0], want])
    if notify: _say(f"Microphone: {'Headset' if plugged else 'Mic array'} - {MODE_LABEL[mode]}", "mic")
    return {"device": dev, "mode": mode, "default_source": want}

def _signature():
    names = _source_names()
    return (jack_use(), tuple(sorted(n for n in (_pv_node(d) for d in PV_DEV) if n in names)), tuple(sorted(voice_modes().items())))

def watch_mic():
    """Re-pick the default microphone whenever something relevant CHANGES: the headset is plugged/unplugged, a filter appears/vanishes, or a mode changes.
    Edge-triggered, so it never fights a manual choice made in between (e.g. Shift+F5)."""
    last = None
    while True:
        try: sig = _signature()
        except Exception as e: print("mic watcher:", e, flush=True); time.sleep(3); continue
        if sig != last:
            time.sleep(0.8)                                                  # let PipeWire finish creating/removing nodes
            try: print(apply_mic_policy(_cfg()["audio"]["notify"] and (last is None or sig[0] != last[0])), flush=True)
            except Exception as e: print("mic policy failed:", e, flush=True)
            last = sig
        time.sleep(1.0)


# ---- speaker-side noise filter (off by default: it also gates music) -----------------------------------------------------------------------
SPK_CONF = f"{CONF_DIR}/truesense-pv-speaker.conf"

def speaker_filter(on, mode="conference"):
    if not on:
        if os.path.exists(SPK_CONF): os.remove(SPK_CONF); _reload()
        return False
    if not PLUGIN: raise HwError("RNNoise LADSPA plugin missing")
    thr, grace, retro = MODES[mode]
    os.makedirs(CONF_DIR, exist_ok=True)
    util.write(SPK_CONF, f'''context.modules = [
  {{ name = libpipewire-module-filter-chain
    args = {{
      node.description = "Purified Voice Speaker"
      filter.graph = {{ nodes = [ {{ type = ladspa name = rnnoise plugin = "{PLUGIN}" label = noise_suppressor_stereo
                    control = {{ "VAD Threshold (%)" = {thr} "VAD Grace Period (ms)" = {grace} "Retroactive VAD Grace (ms)" = {retro} }} }} ] }}
      capture.props = {{ media.class = Audio/Sink node.name = "truesense_pv_speaker" node.description = "Purified Voice Speaker" audio.position = [ FL FR ] }}
      playback.props = {{ node.passive = true }}
    }}
  }}
]
''')
    _reload(); return True

def speaker_filter_on(): return os.path.exists(SPK_CONF)


# ---- DTS sound (AcerUltraX) ---------------------------------------------------------------------------------------------------------------
def _sudo_home():
    """When run with sudo (installer/doctor) look in the invoking user's home, not root's."""
    import pwd
    try: return pwd.getpwnam(os.environ.get("SUDO_USER", "")).pw_dir
    except KeyError: return os.path.expanduser("~")

def _dts_bin():
    return shutil.which("acerultrax-cli") or next((p for p in (os.path.expanduser("~/.local/bin/acerultrax-cli"), _sudo_home() + "/.local/bin/acerultrax-cli", "/usr/local/bin/acerultrax-cli", "/usr/bin/acerultrax-cli") if os.path.exists(p)), None)

def dts(*args, timeout=30):
    b = _dts_bin()
    if not b: raise HwError("AcerUltraX is not installed (https://github.com/fawaz7/AcerUltraX); TrueSense only drives it, it does not bundle Acer's files")
    r = subprocess.run([b, *args], capture_output=True, text=True, timeout=timeout)
    if r.returncode: raise HwError((r.stderr or r.stdout).strip().splitlines()[-1] if (r.stderr or r.stdout).strip() else "acerultrax-cli failed")
    return r.stdout.strip()

def dts_status(): return dts("status")
def dts_mode(name): return dts("mode", name)
def dts_power(on): return dts("power", "on" if on else "off")
