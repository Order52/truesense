#!/usr/bin/env python3
"""Regenerates docs/images/*.png: the real window, driven against the built-in hardware simulator and an in-process service, so the pictures are reproducible and show
no personal data.   python3 tools/screenshots.py   (needs python-pyqt6; runs headless)"""
import os, sys, tempfile, time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
os.environ.update(TRUESENSE_SIM="1", QT_QPA_PLATFORM="offscreen", HOME=tempfile.mkdtemp(prefix="ts-shots-"), TRUESENSE_SOCKET="/nonexistent/ts.sock", TRUESENSE_OSD="none")
sys.path.insert(0, os.path.join(ROOT, "src"))
from PyQt6.QtWidgets import QApplication                                    # noqa: E402
from truesense import audio, config, daemon, device, gui, ipc, keyboard, keymap, looks, state, userstate   # noqa: E402

OUT = os.path.join(ROOT, "docs", "images"); os.makedirs(OUT, exist_ok=True)

# an in-process service on the simulated hardware
cfg = config.load("/nonexistent", None); cfg["user"] = "nobody"
st = state.State({"smart_fan": True, "auto_profile": True, "auto_mode": "smart", "fan_curve": "truesense", "fan_curve_data": None, "pl_user": None,
                  "smart_profile": "truesense", "smart_profile_data": None, "operating_follow": False, "profile_hold_until": 0}, tempfile.mkdtemp() + "/s.json")
daemon.RUN = tempfile.mkdtemp(); daemon.TMP_STATE = tempfile.mkdtemp() + "/ts"
brain = daemon.Brain(cfg, st); brain.say = lambda *a, **k: None; brain.last_ac = True; brain.read_cpu_load = lambda: None; brain.find_hwmon = lambda: None; brain.coretemp = None; brain.gpu_temp = 0
daemon.hw.on_ac = lambda: True; device.SIM.temps = (54, 58, 49); device.SIM.rpm = (2150, 2090); brain.cpu_load = 22
for _ in range(6): brain.step()
handlers = brain.handlers(); ipc.available = lambda *a, **k: True
ipc.call = lambda cmd, timeout=3, path=None, **a: handlers[cmd](**a)
cpu_mod = sys.modules["truesense.cpu"]; cpu_mod.info = lambda: {"pl1_w": 80.0, "pl2_w": 80.0, "rapl_enabled": "1", "msr_lock_bit": False, "nvidia_power_limit_w": None}
undervolt_mod = sys.modules["truesense.undervolt"]; cpu_mod.uv_get = lambda plane=0: 0
gu = sys.modules["truesense.gpuundervolt"]; gu.read = lambda: {"offset_mhz": 0, "range_mhz": [-1000, 1000]}
audio.output_status = lambda: {"active": "speakers", "jack_plugged": True}; audio.jack_status = lambda: {"headphones": True, "headset_mic": True, "raw": {}}
audio.mic_state = lambda: {"default": "truesense_pv_headset", "muted": False, "sources": {}, "volume": 100}; audio.voice_modes = lambda: {"headset": "conference", "array": "conference"}
audio.headset_boost = lambda level=None: 1; audio.dts_status = lambda: "DTS:            on"
keyboard.SIM["brightness"] = 30; device.SIM.rpm = (2150, 2090)

app = QApplication([]); app.setStyle("Fusion"); app.setStyleSheet(gui.STYLE)
w = gui.Window(); w.setFixedHeight(1400); w.show()
def spin(n=30):
    for _ in range(n): app.processEvents(); time.sleep(0.03)
def shot(name, widget=None):
    """Grab the page and trim the empty space below its last control."""
    spin(25); img = (widget or w).grab().toImage(); bg = img.pixel(2, img.height() - 3); last = 0
    for y in range(img.height() - 1, -1, -1):
        if any(img.pixel(x, y) != bg for x in range(8, img.width() - 8, 3)): last = y; break
    img.copy(0, 0, img.width(), min(img.height(), last + 28)).save(os.path.join(OUT, name + ".png")); print("wrote", name)
def page(i): return w.pages.widget(i).widget()

spin(60)
w.power.btns[2].setChecked(True); shot("home", page(0))
w.fan.btns[3].click(); spin(15); w.fan_link.setChecked(False); w.fan_cpu.setValue(55); w.fan_gpu.setValue(70); shot("home-manual-fans", page(0)); w.fan.btns[0].click(); spin(10)
w.go("lighting"); w.light_tabs.btns[0].click(); w.kb_fx.setCurrentIndex(w.kb_fx.findText("Wave")); shot("lighting-keyboard", page(1))
w.go("keys")
cells = ["00aec7"] * 126 + ["000000"] * 2
for name, col in (("w", "ff3b30"), ("a", "ff3b30"), ("s", "ff3b30"), ("d", "ff3b30")): cells[keymap.NAMES[name][0]] = col
for n in ("up", "down", "left", "right"): cells[keymap.NAMES[n][0]] = "34c759"
for i in range(1, 13): cells[keymap.NAMES[f"f{i}"][0]] = "%02x%02x%02x" % keymap.hsv((i - 1) / 14)
w.board.set_cells(cells); w.board.select_group("wasd"); shot("per-key", page(1))
w.light_tabs.btns[2].click(); w.rear_fx.set_index(0); shot("lighting-rear", page(1))
w.light_tabs.btns[3].click(); shot("lighting-profiles", page(1))
w.go("curves"); w.load_curve_in_editor("truesense"); w.editor.marks = {"cpu": 54, "gpu": 49}; w.editor.update(); shot("fan-curves", page(5))
w.go("smart"); shot("smart-auto", page(6)); w.go("predator"); w.kp_edit["double"].set_spec("exec:sh '/home/you/backup.sh'"); w.load_keyprofile("gaming"); shot("predator-key", page(7))
w.go("undervolt"); w.uv_tabs.btns[0].click(); shot("undervolt-cpu", page(8)); w.uv_tabs.btns[1].click(); shot("undervolt-gpu", page(8))
w.go("battery"); shot("battery", page(2)); w.go("audio"); shot("audio", page(3)); w.go("settings"); shot("settings", page(4))
w.close(); print("done")
