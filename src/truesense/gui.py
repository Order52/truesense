"""Optional window for TrueSense (Qt 6 via PyQt6). A thin layer over the same functions the CLI uses; nothing else depends on it.

Run: truesense gui        Needs python-pyqt6 (Arch: `pacman -S python-pyqt6`); runs natively on Wayland (Hyprland) and X11.
"""
import sys, threading
from . import actions, audio, backend as hw, config, ipc, keyboard, keymap, profiles
from .device import HwError

try:
    from PyQt6.QtCore import Qt, QTimer, pyqtSignal, QObject, QRectF
    from PyQt6.QtGui import QColor, QPainter, QFont, QPen
    from PyQt6.QtWidgets import (QApplication, QCheckBox, QColorDialog, QComboBox, QFormLayout, QGroupBox, QHBoxLayout, QLabel, QLineEdit, QMainWindow,
                                 QPushButton, QScrollArea, QSlider, QSpinBox, QListWidget, QStackedWidget, QVBoxLayout, QWidget, QFrame)
except ImportError as _e:
    Qt = None; _IMPORT_ERROR = _e

FAN_MODES = ["Smart curve", "Firmware auto", "Max", "Manual"]
KB_EFFECTS = ["static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"]
KEY_GROUPS = ["all", "wasd", "arrows", "fkeys", "numrow", "letters", "numpad", "esc", "space", "enter", "predator", "lshift"]
VOICE = ["off", "conference", "personal"]
GESTURES = [("tap", "Tap"), ("double", "Double tap"), ("triple", "Triple tap"), ("long", "Long press"), ("fn_esc", "Fn + Esc")]
START = [("last", "Last used"), ("default", "Stock look"), ("off", "Off"), ("none", "Leave as the firmware has it")]


STYLE = """
* { font-size: 13px; }
QMainWindow, QWidget { background: #14171c; color: #e6e9ee; }
QListWidget { background: #0f1115; border: none; padding: 10px 6px; outline: 0; }
QListWidget::item { padding: 10px 12px; border-radius: 8px; margin: 2px 0; color: #aab1bd; }
QListWidget::item:selected { background: #1d2a30; color: #3fd0e6; }
QListWidget::item:hover:!selected { background: #1a1e25; }
QGroupBox { border: 1px solid #262b34; border-radius: 12px; margin-top: 14px; padding: 16px 14px 10px 14px; background: #181c22; font-weight: 600; }
QGroupBox::title { subcontrol-origin: margin; left: 14px; padding: 0 6px; color: #3fd0e6; }
QPushButton { background: #232a33; border: 1px solid #2d3540; border-radius: 8px; padding: 6px 14px; }
QPushButton:hover { background: #2b3440; } QPushButton:pressed { background: #1d2a30; }
QComboBox, QLineEdit { background: #10131a; border: 1px solid #2d3540; border-radius: 8px; padding: 5px 10px; min-height: 20px; }
QComboBox QAbstractItemView { background: #181c22; selection-background-color: #1d2a30; border: 1px solid #2d3540; }
QCheckBox { spacing: 10px; } QCheckBox::indicator { width: 36px; height: 20px; border-radius: 10px; background: #2d3540; }
QCheckBox::indicator:checked { background: #00aec7; }
QSlider::groove:horizontal { height: 6px; background: #2d3540; border-radius: 3px; }
QSlider::sub-page:horizontal { background: #00aec7; border-radius: 3px; }
QSlider::handle:horizontal { background: #e6e9ee; width: 16px; height: 16px; margin: -5px 0; border-radius: 8px; }
QScrollArea { border: none; } QStatusBar { background: #0f1115; color: #8a93a1; }
QLabel { background: transparent; }
"""


class Bus(QObject if Qt else object):
    message = pyqtSignal(str) if Qt else None
    refreshed = pyqtSignal(dict) if Qt else None


class ColorButton(QPushButton if Qt else object):
    """A colour swatch; the colour dialog reports changes live while you drag in it."""
    changed = pyqtSignal(str) if Qt else None

    def __init__(self, hexcolor="00aec7", parent=None):
        super().__init__(parent); self.hex = hexcolor; self.setFixedWidth(64); self._paint(); self.clicked.connect(self._open)

    def _paint(self): self.setStyleSheet(f"background:#{self.hex}; border:1px solid #666; min-height:20px;")

    def set_hex(self, h): self.hex = h; self._paint()

    def _open(self):
        d = QColorDialog(QColor("#" + self.hex), self); d.setOption(QColorDialog.ColorDialogOption.DontUseNativeDialog)
        d.currentColorChanged.connect(lambda c: self._set(c)); d.setModal(False); d.show()

    def _set(self, c):
        if c.isValid(): self.hex = "%02x%02x%02x" % (c.red(), c.green(), c.blue()); self._paint(); self.changed.emit(self.hex)


class KeyboardView(QWidget if Qt else object):
    """The keyboard as a grid (column = cell // 6, row = cell % 6): click or drag to choose keys, Ctrl adds to the choice."""
    selection_changed = pyqtSignal() if Qt else None
    KW, KH = 30, 32

    def __init__(self):
        super().__init__(); self.cells = ["00aec7"] * 126 + ["000000"] * 2; self.sel = set(); self.labels = {}
        for name, cs in keymap.NAMES.items():
            for c in cs: self.labels[c] = name
        self.setFixedSize(22 * self.KW + 2, 6 * self.KH + 2); self.setMouseTracking(False)

    def _cell_at(self, pos):
        c, r = int(pos.x() // self.KW), int(pos.y() // self.KH)
        return c * 6 + r if 0 <= c < 22 and 0 <= r < 6 else None

    def mousePressEvent(self, e):
        c = self._cell_at(e.position())
        if c is None: return
        if not (e.modifiers() & Qt.KeyboardModifier.ControlModifier): self.sel = set()
        self.sel.add(c); self.update(); self.selection_changed.emit()

    def mouseMoveEvent(self, e):
        c = self._cell_at(e.position())
        if c is not None and c not in self.sel: self.sel.add(c); self.update(); self.selection_changed.emit()

    def select_group(self, name):
        self.sel = set(keymap.cells_for(name)); self.update(); self.selection_changed.emit()

    def paintEvent(self, _):
        p = QPainter(self); f = QFont(); f.setPointSize(7); p.setFont(f)
        for c in range(126):
            col, row = divmod(c, 6); rect = QRectF(col * self.KW + 1, row * self.KH + 1, self.KW - 2, self.KH - 2)
            if c not in self.labels: continue
            color = QColor("#" + self.cells[c]); p.setBrush(color); p.setPen(QPen(QColor("#ffffff" if c in self.sel else "#444444"), 3 if c in self.sel else 1))
            p.drawRoundedRect(rect, 4, 4)
            lum = 0.3 * color.red() + 0.59 * color.green() + 0.11 * color.blue(); p.setPen(QColor("#000" if lum > 140 else "#fff"))
            p.drawText(rect, Qt.AlignmentFlag.AlignCenter, self.labels[c][:5])


class Window(QMainWindow if Qt else object):
    def __init__(self):
        super().__init__(); self.setWindowTitle("TrueSense"); self.resize(860, 640)
        self.bus = Bus(); self.bus.message.connect(self._msg); self.bus.refreshed.connect(self._apply); self.busy = {}; self.loading = False
        self.cfg = config.load(); self.nav = QListWidget(); self.nav.setFixedWidth(170); self.pages = QStackedWidget()
        root = QWidget(); h = QHBoxLayout(root); h.setContentsMargins(0, 0, 0, 0); h.setSpacing(0); h.addWidget(self.nav); h.addWidget(self.pages, 1); self.setCentralWidget(root)
        self.nav.currentRowChanged.connect(self.pages.setCurrentIndex); self.statusBar().showMessage("Ready")
        for build in (self.t_overview, self.t_lighting, self.t_keys, self.t_battery, self.t_audio, self.t_keybind, self.t_startup): build()
        self.nav.setCurrentRow(0)
        self.timer = QTimer(self); self.timer.timeout.connect(self.tick); self.timer.start(2000)
        self.full_refresh(); self.tick()

    # ---- plumbing ------------------------------------------------------------------------------------------------------------------
    def _msg(self, t): self.statusBar().showMessage(t, 6000)

    def do(self, fn, ok="done"):
        """Run a backend call off the UI thread; show its result or its error."""
        def work():
            try: r = fn(); self.bus.message.emit(ok(r) if callable(ok) else ok)
            except Exception as e: self.bus.message.emit(f"Error: {e}")
            self.full_refresh()
        threading.Thread(target=work, daemon=True).start()

    def live(self, key, fn):
        """For colours and sliders while dragging: one write at a time, the newest value always wins."""
        if self.busy.get(key): self.busy[key] = "again"; return
        self.busy[key] = True
        def work():
            try: fn()
            except Exception as e: self.bus.message.emit(f"Error: {e}")
            again = self.busy.get(key) == "again"; self.busy[key] = None
            if again: self.live(key, fn)
        threading.Thread(target=work, daemon=True).start()

    def tab(self, title):
        w = QWidget(); lay = QVBoxLayout(w); lay.setContentsMargins(24, 20, 24, 20); lay.setSpacing(14)
        sc = QScrollArea(); sc.setWidgetResizable(True); sc.setFrameShape(QFrame.Shape.NoFrame); sc.setHorizontalScrollBarPolicy(Qt.ScrollBarPolicy.ScrollBarAlwaysOff); sc.setWidget(w); self.pages.addWidget(sc); self.nav.addItem(title); return lay

    @staticmethod
    def box(lay, title):
        g = QGroupBox(title); f = QFormLayout(g); lay.addWidget(g); return f

    def check(self, f, text, cb):
        c = QCheckBox(text); c.toggled.connect(lambda v: None if self.loading else cb(v)); f.addRow(c); return c

    def combo(self, f, label, items, cb):
        c = QComboBox(); [c.addItem(x) for x in items]; c.currentIndexChanged.connect(lambda i: None if self.loading else cb(i)); f.addRow(label, c); return c

    def slider(self, f, label, lo, hi, step, cb, final=None):
        s = QSlider(Qt.Orientation.Horizontal); s.setRange(lo, hi); s.setSingleStep(step); s.setPageStep(step * 2)
        s.valueChanged.connect(lambda v: None if self.loading else cb(v))
        if final: s.sliderReleased.connect(lambda: final(s.value()))
        f.addRow(label, s); return s

    def button(self, f, label, text, cb):
        b = QPushButton(text); b.clicked.connect(lambda *_: cb()); f.addRow(label, b); return b

    def row(self, f, label, *widgets):
        h = QHBoxLayout(); [h.addWidget(w) for w in widgets]; h.addStretch(); f.addRow(label, h)

    # ---- Overview ------------------------------------------------------------------------------------------------------------------
    def t_overview(self):
        lay = self.tab("Overview"); f = self.box(lay, "Live")
        self.live_lbl = QLabel("…"); self.daemon_lbl = QLabel("…"); f.addRow("Sensors", self.live_lbl); f.addRow("Daemon", self.daemon_lbl)
        f = self.box(lay, "Profile (power, fans, lighting, battery, audio)")
        self.profile_cb = QComboBox(); self.profile_cb.addItems(profiles.names()); b = QPushButton("Apply"); b.clicked.connect(lambda: self.do(lambda: profiles.apply(self.profile_cb.currentText()), lambda r: str(r)))
        self.row(f, "Profile", self.profile_cb, b)
        f = self.box(lay, "Power mode")
        try: self.modes = hw.supported_modes()
        except Exception: self.modes = list(hw.MODES)
        self.power_cb = self.combo(f, "Mode", self.modes, lambda i: self.do(lambda: (hw.set_mode(self.modes[i]), ipc.call("hold", minutes=30))[0], lambda r: f"Power mode: {r} (held 30 min)"))
        self.auto_chk = self.check(f, "Automatic profile (by power source and load)", lambda v: self.do(lambda: ipc.call("set", key="auto_profile", value=v), "Saved"))
        f = self.box(lay, "Fans")
        self.fan_cb = self.combo(f, "Control", FAN_MODES, lambda i: self.fan_apply(i))
        self.fan_pct = self.slider(f, "Manual speed (%)", 20, 100, 5, lambda v: None, lambda v: self.fan_apply(3))
        lay.addStretch()

    def fan_apply(self, i):
        def go():
            if i == 0: ipc.call("set", key="smart_fan", value=True); return "Fans: smart curve"
            ipc.call("set", key="smart_fan", value=False); import time; time.sleep(1.2)
            if i == 1: hw.fan_auto(); return "Fans: firmware auto"
            if i == 2: hw.fan_max(); return "Fans: MAX"
            p = self.fan_pct.value(); hw.fan_manual(p, p); return f"Fans: manual {p} %"
        self.do(go, lambda r: r)

    # ---- Lighting ------------------------------------------------------------------------------------------------------------------
    def t_lighting(self):
        lay = self.tab("Lighting"); f = self.box(lay, "Keyboard  (Fn+F11 / Fn+F12 also change brightness)")
        self.kb_on = self.check(f, "Backlight on", lambda v: self.do(lambda: actions.run("keyboard:on" if v else "keyboard:off"), lambda r: r))
        self.kb_fx = QComboBox(); self.kb_fx.addItems(KB_EFFECTS); self.kb_color = ColorButton("00aec7"); b = QPushButton("Apply effect")
        b.clicked.connect(lambda: self.do(lambda: keyboard.set_effect(self.kb_fx.currentText(), self.kb_color.hex, 5, max(self.kb_level.value(), 50 if self.kb_fx.currentText() == "neon" else 1)), "Effect applied"))
        self.row(f, "Effect", self.kb_fx, self.kb_color, b)
        self.kb_color.changed.connect(lambda h: self.live("kbc", lambda: keyboard.set_color(h)))
        self.kb_level = self.slider(f, "Brightness (0 to 50)", 0, 50, 5, lambda v: self.live("kbl", lambda: keyboard.set_brightness(v)))
        b1 = QPushButton("Rainbow"); b1.clicked.connect(lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.rainbow()), None), "Rainbow"))
        b2 = QPushButton("Fill with colour"); b2.clicked.connect(lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.canvas(keymap.hexcolor(self.kb_color.hex))), None), "Filled"))
        b3 = QPushButton("Stock look"); b3.clicked.connect(lambda: self.do(lambda: keyboard.restore_default(None), "Restored"))
        self.row(f, "Per-key presets", b1, b2, b3)
        self.kb_timeout = self.check(f, "Backlight off after 30 s without typing", lambda v: self.do(lambda: hw.set_kb_timeout(v), "Saved"))
        f = self.box(lay, "Rear light bar (three parts)")
        self.rear_on = self.check(f, "On", lambda v: self.do(lambda: actions.run("rear:on" if v else "rear:off"), lambda r: r))
        self.rear_fx = self.combo(f, "Effect", list(hw.REAR_MODES), lambda i: self.rear_apply())
        self.zc = [ColorButton("00aec7") for _ in range(3)]; self.rear_color = ColorButton("00aec7")
        for b in self.zc + [self.rear_color]: b.changed.connect(lambda h: self.rear_apply())
        self.row(f, "Static: left / middle / right", *self.zc); self.row(f, "Colour (other effects)", self.rear_color)
        self.rear_speed = self.slider(f, "Speed (1 to 5)", 1, 5, 1, lambda v: self.rear_apply()); self.rear_bright = self.slider(f, "Brightness (%)", 0, 100, 10, lambda v: self.rear_apply())
        lay.addStretch()

    def rear_apply(self):
        def go():
            mode = list(hw.REAR_MODES)[self.rear_fx.currentIndex()]; sp, br = self.rear_speed.value() or 3, self.rear_bright.value()
            if mode == "static": hw.rear_set("static", speed=sp, brightness=br, zones=[b.hex for b in self.zc])
            else: hw.rear_set(mode, self.rear_color.hex, sp, br)
        if not self.loading: self.live("rear", go)

    # ---- Per-key -------------------------------------------------------------------------------------------------------------------
    def t_keys(self):
        lay = self.tab("Per-key colours")
        lay.addWidget(QLabel("Click or drag to choose keys (Ctrl adds), then pick a colour. Only the chosen keys change."))
        self.kv = KeyboardView(); lay.addWidget(self.kv); self.kv.selection_changed.connect(self.sel_changed)
        for chunk in (KEY_GROUPS[:6], KEY_GROUPS[6:]):
            h = QHBoxLayout()
            for g in chunk:
                b = QPushButton(g); b.clicked.connect(lambda _, g=g: self.kv.select_group(g)); h.addWidget(b)
            h.addStretch(); lay.addLayout(h)
        h = QHBoxLayout(); self.pk_color = ColorButton("ff0000"); self.sel_lbl = QLabel("no keys chosen")
        b = QPushButton("Paint chosen keys"); b.clicked.connect(self.paint_sel); self.pk_color.changed.connect(lambda h_: self.paint_sel())
        bk = QPushButton("Black"); bk.clicked.connect(lambda: (self.pk_color.set_hex("000000"), self.paint_sel()))
        for w in (QLabel("Colour"), self.pk_color, b, bk, self.sel_lbl): h.addWidget(w)
        h.addStretch(); lay.addLayout(h); lay.addStretch()

    def sel_changed(self):
        n = len(self.kv.sel); names = sorted({self.kv.labels[c] for c in self.kv.sel if c in self.kv.labels}); self.sel_lbl.setText(f"{n} key(s): " + ", ".join(names[:8]) + ("…" if len(names) > 8 else ""))

    def paint_sel(self):
        if not self.kv.sel or self.loading: return
        hexc = self.pk_color.hex; ch = {c: hexc for c in self.kv.sel}
        for c in self.kv.sel: self.kv.cells[c] = hexc
        self.kv.update(); self.live("paint", lambda: keyboard.paint_cells(ch))

    # ---- Battery and display -------------------------------------------------------------------------------------------------------
    def t_battery(self):
        lay = self.tab("Battery & Display"); f = self.box(lay, "Battery")
        self.limit = self.check(f, "80 % charge limit", lambda v: self.do(lambda: hw.set_battery_limit(v), lambda r: f"80 % limit: {'on' if r else 'off'}"))
        self.calib = self.check(f, "Calibration (needs AC power, takes hours; untick to cancel)", lambda v: self.do(lambda: hw.set_calibration(v), lambda r: f"Calibration: {'on' if r else 'off'}"))
        self.usb = self.combo(f, "USB charging while off", ["Off", "Stop at 10 %", "Stop at 20 %", "Stop at 30 %"], lambda i: self.do(lambda: hw.set_usb_charge(hw.USB_LEVELS[i]), lambda r: f"USB charge level: {r}"))
        f = self.box(lay, "Display and boot")
        self.od = self.check(f, "LCD overdrive", lambda v: self.do(lambda: hw.set_overdrive(v), lambda r: f"Overdrive: {'on' if r else 'off'}"))
        self.boot = self.check(f, "Boot animation and sound", lambda v: self.do(lambda: hw.set_boot_sound(v), lambda r: f"Boot sound: {'on' if r else 'off'}"))
        f = self.box(lay, "GPU mode (takes effect after a reboot)")
        self.gpu = self.combo(f, "Mode", list(hw.GPU_MODES), lambda i: self.do(lambda: hw.set_gpu_mode(list(hw.GPU_MODES)[i]), lambda r: f"GPU mode: {r} (reboot to apply)"))
        lay.addStretch()

    # ---- Audio ---------------------------------------------------------------------------------------------------------------------
    def t_audio(self):
        lay = self.tab("Audio"); f = self.box(lay, "Output (speakers and the headphone jack cannot play at once on this laptop)")
        self.out_lbl = QLabel("…"); f.addRow("Now", self.out_lbl)
        b1 = QPushButton("Speakers"); b2 = QPushButton("Headphones")
        b1.clicked.connect(lambda: self.do(lambda: audio.set_output("speakers"), "Output: speakers")); b2.clicked.connect(lambda: self.do(lambda: audio.set_output("headphones"), "Output: headphones"))
        self.row(f, "Switch to", b1, b2)
        self.dts = self.check(f, "Acer DTS sound (AcerUltraX)", lambda v: self.do(lambda: audio.dts_power(v), "DTS " + ("on" if v else "off")))
        f = self.box(lay, "Microphone")
        self.mute = self.check(f, "Mute every microphone", lambda v: self.do(lambda: audio.mic_set_mute(v), lambda r: "Microphones muted" if v else "Microphones live"))
        self.jack_lbl = QLabel("…"); f.addRow("3.5 mm jack", self.jack_lbl)
        self.boost = self.slider(f, "Headset mic boost (0 to 3)", 0, 3, 1, lambda v: None, lambda v: self.do(lambda: audio.headset_boost(v), lambda r: f"Boost: {r}"))
        f = self.box(lay, "Purified Voice (AI noise reduction; conference is gentle, personal is strict)")
        self.v_head = self.combo(f, "Headset microphone", VOICE, lambda i: self.do(lambda: (audio.voice_set("headset", VOICE[i]), audio.apply_mic_policy(False))[0], lambda r: f"Headset mic: {VOICE[i]}"))
        self.v_arr = self.combo(f, "Built-in microphone array", VOICE, lambda i: self.do(lambda: (audio.voice_set("array", VOICE[i]), audio.apply_mic_policy(False))[0], lambda r: f"Mic array: {VOICE[i]}"))
        self.v_note = self.check(f, "Notify when the microphone mode changes", lambda v: self.do(lambda: config.write_user({"audio": {"notify": v}}), "Saved"))
        lay.addStretch()

    # ---- PREDATOR key --------------------------------------------------------------------------------------------------------------
    def t_keybind(self):
        lay = self.tab("PREDATOR key"); f = self.box(lay, "What each press does (see `truesense keys actions`; saved at once, the key service picks it up on restart)")
        self.key_edits = {}
        for g, label in GESTURES:
            e = QLineEdit(self.cfg["keys"][g]); b = QPushButton("Save"); t = QPushButton("Test")
            b.clicked.connect(lambda _, g=g, e=e: self.save_key(g, e)); t.clicked.connect(lambda _, e=e: self.do(lambda: actions.run(e.text()) or "ok", lambda r: str(r)))
            self.row(f, label, e, b, t); self.key_edits[g] = e
        b = QPushButton("Restart key service"); b.clicked.connect(lambda: self.do(lambda: __import__("subprocess").run(["systemctl", "--user", "restart", "truesense-keys"], check=False) and "Key service restarted", lambda r: "Key service restarted"))
        f.addRow(b); lay.addStretch()

    def save_key(self, g, e):
        err = actions.validate(e.text())
        if err: self.statusBar().showMessage(err, 8000); return
        self.do(lambda: config.write_user({"keys": {g: e.text()}}), f"{g}: saved")

    # ---- Startup -------------------------------------------------------------------------------------------------------------------
    def t_startup(self):
        lay = self.tab("At login"); f = self.box(lay, "What comes back at login (lighting is saved automatically on every change)")
        s = self.cfg["startup"]; self.st = {}
        for key, title in (("keyboard", "Keyboard"), ("rear", "Rear light")):
            c = QComboBox(); [c.addItem(label, val) for val, label in START]; c.setCurrentIndex(max(0, [v for v, _ in START].index(s[key]) if s[key] in dict(START) else 0))
            c.currentIndexChanged.connect(lambda i, k=key, c=c: self.do(lambda: config.write_user({"startup": {k: c.itemData(i)}}), f"{k} at login: {c.itemData(i)}")); f.addRow(title, c)
        c = QComboBox(); c.addItem("(none)", ""); [c.addItem(n, n) for n in profiles.names()]; c.setCurrentIndex(max(0, c.findData(s["profile"])))
        c.currentIndexChanged.connect(lambda i, c=c: self.do(lambda: config.write_user({"startup": {"profile": c.itemData(i)}}), f"profile at login: {c.itemData(i) or 'none'}")); f.addRow("Profile first", c)
        from . import startup
        self.button(f, "Try it now", "Apply now", lambda: self.do(lambda: startup.apply(), lambda r: "; ".join(r)))
        lay.addStretch()

    # ---- refresh ---------------------------------------------------------------------------------------------------------------------
    def tick(self):
        try: s = hw.sensors(); self.live_lbl.setText(f"CPU {s['cpu_temp']} °C   GPU {s['gpu_temp']} °C   board {s['sys_temp']} °C   fans {s['cpu_fan_rpm']} / {s['gpu_fan_rpm']} rpm")
        except HwError as e: self.live_lbl.setText(str(e))
        try: st = ipc.call("state"); self.daemon_lbl.setText(f"running, activity: {st['live'].get('activity', '?')}")
        except HwError as e: self.daemon_lbl.setText(str(e))

    def full_refresh(self):
        def work():
            d = {}
            def get(k, fn):
                try: d[k] = fn()
                except Exception: pass
            get("state", lambda: ipc.call("state")); get("mode", hw.get_mode); get("fan", hw.fan_state); get("rear", hw.rear_get); get("kb", keyboard.get_brightness)
            get("kbtimeout", hw.get_kb_timeout); get("limit", hw.get_battery_limit); get("calib", hw.get_calibration); get("usb", hw.get_usb_charge); get("od", hw.get_overdrive)
            get("boot", hw.get_boot_sound); get("gpu", hw.get_gpu_mode); get("out", audio.output_status); get("jack", audio.jack_status); get("mic", audio.mic_state)
            get("boost", audio.headset_boost); get("voice", audio.voice_modes); get("look", keyboard.last); get("cells", keyboard.current_cells)
            get("dts", lambda: "DTS:            on" in audio.dts_status())
            self.bus.refreshed.emit(d)
        threading.Thread(target=work, daemon=True).start()

    def _apply(self, d):
        self.loading = True
        try:
            if "state" in d:
                s = d["state"]["settings"]; self.auto_chk.setChecked(bool(s["auto_profile"]))
                self.fan_cb.setCurrentIndex(0 if s["smart_fan"] else {"auto": 1, "max": 2, "manual": 3}.get(d.get("fan", {}).get("mode"), 1))
            if d.get("mode") in self.modes: self.power_cb.setCurrentIndex(self.modes.index(d["mode"]))
            if "rear" in d:
                r = d["rear"]; self.rear_on.setChecked(r["brightness"] > 0); self.rear_bright.setValue(r["brightness"]); self.rear_speed.setValue(r["speed"])
                if r["mode"] in hw.REAR_MODES: self.rear_fx.setCurrentIndex(list(hw.REAR_MODES).index(r["mode"]))
                self.rear_color.set_hex(r["color"]); zs = r.get("zones") or __import__("truesense.userstate", fromlist=["x"]).get("rear_zones")
                for b, z in zip(self.zc, zs if isinstance(zs, list) and len(zs) == 3 else [r["color"]] * 3): b.set_hex(z)
            if "kb" in d: self.kb_level.setValue(d["kb"]); self.kb_on.setChecked(d["kb"] > 0)
            look = d.get("look") or {}
            if look.get("mode") == "effect" and look.get("name") in KB_EFFECTS: self.kb_fx.setCurrentIndex(KB_EFFECTS.index(look["name"])); self.kb_color.set_hex(look.get("color", "00aec7"))
            if d.get("kbtimeout") is not None: self.kb_timeout.setChecked(bool(d["kbtimeout"]))
            if "cells" in d and not self.kv.sel: self.kv.cells = ["%02x%02x%02x" % tuple(c) for c in d["cells"]]; self.kv.update()
            for k, w in (("limit", self.limit), ("calib", self.calib), ("od", self.od), ("boot", self.boot), ("dts", self.dts)):
                if d.get(k) is not None: w.setChecked(bool(d[k]))
            if "usb" in d and d["usb"] in hw.USB_LEVELS: self.usb.setCurrentIndex(hw.USB_LEVELS.index(d["usb"]))
            if d.get("gpu") in hw.GPU_MODES: self.gpu.setCurrentIndex(list(hw.GPU_MODES).index(d["gpu"]))
            if "out" in d: self.out_lbl.setText(d["out"]["active"] + ("  (headphone jack in use)" if d["out"]["jack_plugged"] else ""))
            if "jack" in d: self.jack_lbl.setText(("headphones plugged" if d["jack"]["headphones"] else "nothing plugged") + (", headset microphone detected" if d["jack"]["headset_mic"] else ", no headset microphone"))
            if "mic" in d: self.mute.setChecked(bool(d["mic"]["muted"]))
            if d.get("boost") is not None: self.boost.setValue(d["boost"])
            if "voice" in d:
                v = d["voice"]; self.v_head.setCurrentIndex(VOICE.index(v["headset"]) if v.get("headset") in VOICE else 0); self.v_arr.setCurrentIndex(VOICE.index(v["array"]) if v.get("array") in VOICE else 0)
            self.v_note.setChecked(bool(config.load()["audio"]["notify"]))
        finally: self.loading = False


def main():
    if Qt is None:
        print(f"The window needs python-pyqt6 ({_IMPORT_ERROR}).  Arch: sudo pacman -S python-pyqt6.  Everything works from the command line without it.")
        return 1
    app = QApplication(sys.argv[:1]); app.setApplicationName("TrueSense"); app.setStyle("Fusion"); app.setStyleSheet(STYLE); w = Window(); w.show(); return app.exec()
