"""Fn keys + PREDATOR key service (runs as the logged-in user; hidraw nodes are group plugdev).

Reports on the vendor page 0x88 (report id 4, handoff extras section 4): 04 80 FF Fn+F10, 04 7D FF Fn+F11, 04 7C FF Fn+F12,
04 81 FF PREDATOR press, 04 00 00 release. Everything else (volume, rfkill, F8=F15, Fn+S) is handled by the kernel/compositor.
"""
import glob, json, os, re, select, subprocess, threading, time
from . import backend as hw, keyboard, daemon_ctl

STATE = os.path.expanduser("~/.cache/rpredatorsense-kb")
LONG_S, DOUBLE_S, STEP = 0.8, 0.45, 2          # STEP: keyboard brightness per Fn+F11/F12 press (0..50 range => 25 levels); config [buttons] kb_step
ACTIONS = ("mode-cycle", "fan-max-toggle", "touchpad", "mic-toggle", "output-toggle", "none")

def find_node():
    for h in sorted(glob.glob("/sys/class/hidraw/hidraw*")):
        try:
            if "000004F2:00000117" not in open(h + "/device/uevent").read().upper(): continue
            d = open(h + "/device/report_descriptor", "rb").read()
        except OSError: continue
        if b"\x06\x88\x00" in d or b"\x05\x88" in d: return "/dev/" + os.path.basename(h)
    return None

def sh(cmd): return subprocess.run(cmd, capture_output=True, text=True)

def osd(text):
    for cmd in (["noctalia", "msg", "osd-show", "gauge", text], ["notify-send", "-t", "1700", "rpredatorsense", text]):
        try: subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL); return
        except OSError: continue

def _touchpad_inhibit_file():
    try: txt = open("/proc/bus/input/devices").read()
    except OSError: return None
    for blk in txt.split("\n\n"):
        if "Touchpad" in blk and "SYNA" in blk.upper():
            m = re.search(r"event\d+", "".join(l for l in blk.splitlines() if l.startswith("H: Handlers=")))
            if m: return f"/sys/class/input/{m.group(0)}/device/inhibited"
    return None

def toggle_touchpad():
    f = _touchpad_inhibit_file()
    if not f: return "touchpad not found"
    try: cur = open(f).read().strip()
    except OSError: return "touchpad state unreadable"
    new = "0" if cur == "1" else "1"
    try: open(f, "w").write(new)
    except OSError: return "touchpad toggle denied (install the udev rule: sudo tools/install.sh)"
    return "touchpad " + ("on" if open(f).read().strip() == "0" else "off")      # report the READ-BACK state

def run_action(a):
    if a == "mode-cycle":
        order = [m for m in ("eco", "quiet", "balanced", "performance", "turbo") if m in hw.supported_modes()]
        new = order[(order.index(hw.get_mode()) + 1) % len(order)]; hw.set_mode(new); daemon_ctl.hold_profile(30); return f"power mode: {new} (held 30 min)"
    if a == "fan-max-toggle":
        if hw.fan_state()["behavior_raw"] == "0x82": hw.fan_auto(); daemon_ctl.set_fan_mode("smart"); return "fans: auto (smart curve)"
        daemon_ctl.set_fan_mode("manual"); time.sleep(1.3); hw.fan_max(); return "fans: MAX"
    if a == "touchpad": return toggle_touchpad()
    if a == "output-toggle":
        from . import audio; new = "headphones" if audio.output_status()["active"] == "speakers" else "speakers"
        audio.announce("Laptop speakers" if new == "speakers" else "Headphones (3.5 mm jack)"); audio.set_output(new); return ""
    if a == "mic-toggle":
        from . import audio; return audio.mic_toggle()
    return ""

def kb_brightness(delta):
    try: cur = int(open(STATE).read())
    except Exception: cur = 25
    new = max(0, min(keyboard.MAX_BRIGHT, cur + delta))
    if new == cur: return f"keyboard brightness {new}/{keyboard.MAX_BRIGHT} (limit)"
    keyboard.set_brightness(new)
    os.makedirs(os.path.dirname(STATE), exist_ok=True); open(STATE, "w").write(str(new)); return f"keyboard brightness {new}/{keyboard.MAX_BRIGHT}"

def main(cfg=None):
    global STEP
    b = {"tap": "mode-cycle", "long": "fan-max-toggle", "double": "output-toggle", "fn_esc": "none", "kb_step": STEP, "kb_handle": False, **(cfg or {}).get("buttons", {})}   # kb_handle: the keyboard FIRMWARE already does Fn+F11/F12 cleanly (verified); handling it too makes them fight
    STEP = max(1, int(b["kb_step"]))
    node = find_node()
    if not node: raise SystemExit("Fn-key HID interface (usage page 0x88) not found")
    print("listening on", node, b, flush=True)
    down = None; last_tap = 0.0; timer = None
    def act(name):
        try: msg = run_action(b[name]); print(name, "->", msg, flush=True); msg and osd(msg)
        except Exception as e: print("action failed:", e, flush=True)
    with open(node, "rb", buffering=0) as f:
        while True:
            r = f.read(8)
            if len(r) < 3 or r[0] != 0x04: continue
            code = bytes(r[1:3]); now = time.monotonic()
            try:
                if code == b"\x80\xff": print(toggle_touchpad(), flush=True)
                elif code in (b"\x7d\xff", b"\x7c\xff") and b["kb_handle"]:
                    delta = -STEP if code == b"\x7d\xff" else STEP
                    while select.select([f], [], [], 0.04)[0]:                 # firmware needs ~50 ms per write: merge queued presses into one
                        r2 = f.read(8)
                        if len(r2) >= 3 and r2[0] == 0x04:
                            c2 = bytes(r2[1:3])
                            if c2 == b"\x7d\xff": delta -= STEP
                            elif c2 == b"\x7c\xff": delta += STEP
                    print(kb_brightness(delta), flush=True)
                elif code == b"\x8a\xff": act("fn_esc")                    # Fn+Esc: new vendor code found by capture (also sends KEY_KPPLUSMINUS); default action none
                elif code == b"\x81\xff": down = now
                elif code == b"\x00\x00" and down is not None:
                    held, down = now - down, None
                    if held >= LONG_S: act("long")
                    elif b["double"] != "none" and now - last_tap < DOUBLE_S:
                        timer and timer.cancel(); last_tap = 0.0; act("double")
                    elif b["double"] != "none": last_tap = now; timer = threading.Timer(DOUBLE_S, act, ("tap",)); timer.start()
                    else: act("tap")
            except Exception as e: print("error:", e, flush=True)
