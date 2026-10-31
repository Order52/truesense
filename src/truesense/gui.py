"""Optional window for TrueSense (Qt 6 via PyQt6). A thin layer over the same functions the CLI uses; nothing else depends on it.

Run: truesense gui        Needs python-pyqt6 (Arch: `pacman -S python-pyqt6`); runs natively on Wayland (Hyprland) and X11.
"""
import sys, threading
from . import actions, audio, backend as hw, config, control, ipc, keyboard, keymap, profiles, userstate
from .device import HwError

try:
    from PyQt6.QtCore import Qt, QTimer, pyqtSignal, QObject, QRectF
    from PyQt6.QtNetwork import QLocalServer, QLocalSocket
    from PyQt6.QtSvg import QSvgRenderer
    from PyQt6.QtCore import QByteArray, QSize
    from PyQt6.QtGui import QColor, QPainter, QFont, QPen, QIcon, QPixmap
    from PyQt6.QtWidgets import (QApplication, QCheckBox, QColorDialog, QComboBox, QFormLayout, QGroupBox, QHBoxLayout, QLabel, QLineEdit, QMainWindow,
                                 QPushButton, QScrollArea, QSlider, QListWidget, QStackedWidget, QVBoxLayout, QWidget, QFrame, QButtonGroup, QSizePolicy, QDoubleSpinBox, QGridLayout)
except ImportError as _e:
    Qt = None; _IMPORT_ERROR = _e


# ---- vector icons (24x24 stroke icons, drawn at any size and colour; nothing to ship) -------------------------------------------------------
_P = {
    "eco": "M6 18c0-8 5-13 13-13 0 8-5 13-13 13zM6 18l8-8",
    "quiet": "M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z",
    "balanced": "M12 4v16M6 20h12M5 8h14M5 8l-3 7a3 3 0 0 0 6 0zM19 8l-3 7a3 3 0 0 0 6 0z",
    "performance": "M4 17a8 8 0 1 1 16 0M12 17l4-6M7 17h.01M17 17h.01",
    "turbo": "M12 3c1 4 5 5 5 10a5 5 0 0 1-10 0c0-2 1-3 2-4 0 2 1 3 2 3 0-3-1-5 1-9z",
    "smart": "M12 3l2 5 5 2-5 2-2 5-2-5-5-2 5-2zM19 16l.8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8z",
    "auto": "M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16zM12 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4z",
    "max": "M12 12c0-4 1-7 4-8 1 3 0 6-4 8zM12 12c4 0 7 1 8 4-3 1-6 0-8-4zM12 12c0 4-1 7-4 8-1-3 0-6 4-8zM12 12c-4 0-7-1-8-4 3-1 6 0 8 4z",
    "manual": "M4 8h9M17 8h3M4 16h3M11 16h9M15 5v6M9 13v6",
    "fan": "M12 12c0-4 1-7 4-8 1 3 0 6-4 8zM12 12c4 0 7 1 8 4-3 1-6 0-8-4zM12 12c0 4-1 7-4 8-1-3 0-6 4-8zM12 12c-4 0-7-1-8-4 3-1 6 0 8 4z",
    "gpu": "M2 7h20v10H2zM6 17v3M18 17v3M12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z",
    "optimus": "M3 6h8v6H3zM13 12h8v6h-8zM11 9h2v6M7 12v3h4",
    "keyboard": "M2 6h20v12H2zM6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10",
    "bar": "M3 10h18v4H3zM6 7v-2M12 7V4M18 7V5M6 17v2M12 17v3M18 17v2",
    "battery": "M3 7h15v10H3zM18 10h3v4h-3zM6 10v4M9 10v4",
    "audio": "M4 10v4h4l5 4V6l-5 4zM16.5 9a4 4 0 0 1 0 6M19 6.5a8 8 0 0 1 0 11",
    "settings": "M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M19.1 4.9L17 7M7 17l-2.1 2.1",
    "palette": "M12 3a9 9 0 1 0 0 18c1.5 0 2-1 1.5-2-.6-1.2.2-2.5 1.6-2.5H17a4 4 0 0 0 4-4c0-5-4-9.5-9-9.5zM7.5 11h.01M10 7.5h.01M14.5 7.5h.01",
    "power": "M12 3v8M6.5 6.5a8 8 0 1 0 11 0",
    "back": "M15 5l-7 7 7 7",
}

def icon_pixmap(name, color="#dfe5ee", size=24):
    svg = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="{color}" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">'
           f'<path d="{_P.get(name, "")}"/></svg>').encode()
    r = QSvgRenderer(QByteArray(svg)); px = QPixmap(size * 2, size * 2); px.fill(Qt.GlobalColor.transparent)
    p = QPainter(px); r.render(p); p.end(); px.setDevicePixelRatio(2); return px

def make_icon(name, color="#dfe5ee", size=20): return QIcon(icon_pixmap(name, color, size))


class Switch(QCheckBox if Qt else object):
    """A pill toggle like PreySense's: a ring marks off, a bar marks on, and the thumb sits on the side that is chosen."""
    def __init__(self): super().__init__(); self.setCursor(Qt.CursorShape.PointingHandCursor); self.setFixedSize(52, 26)
    def sizeHint(self): return QSize(52, 26)
    def hitButton(self, pos): return self.rect().contains(pos)

    def paintEvent(self, _):
        p = QPainter(self); p.setRenderHint(QPainter.RenderHint.Antialiasing); on = self.isChecked(); r = QRectF(self.rect()).adjusted(1, 1, -1, -1)
        p.setBrush(QColor("#2a2e35")); p.setPen(QPen(QColor("#3d434c"), 1)); p.drawRoundedRect(r, 13, 13)
        thumb = QRectF(r.right() - 25, r.top() + 2, 23, r.height() - 4) if on else QRectF(r.left() + 2, r.top() + 2, 23, r.height() - 4)
        p.setBrush(QColor("#7b93b6" if on else "#4a505a")); p.setPen(Qt.PenStyle.NoPen); p.drawRoundedRect(thumb, 10, 10)
        mark = QColor("#ffffff" if on else "#c9ced6"); p.setPen(QPen(mark, 1.8)); p.setBrush(Qt.BrushStyle.NoBrush)
        if on:
            p.drawLine(int(thumb.center().x()), int(thumb.center().y() - 5), int(thumb.center().x()), int(thumb.center().y() + 5)); p.setPen(QPen(QColor("#8b929c"), 1.6)); p.drawEllipse(QRectF(r.left() + 9, r.center().y() - 4, 8, 8))
        else:
            p.drawEllipse(QRectF(thumb.center().x() - 4, thumb.center().y() - 4, 8, 8)); p.setPen(QPen(QColor("#8b929c"), 1.8)); p.drawLine(int(r.right() - 13), int(r.center().y() - 5), int(r.right() - 13), int(r.center().y() + 5))


class TileButton(QPushButton if Qt else object):
    """A big choice tile: icon above, label below; the chosen one is outlined (or filled with its own colour, like the performance modes)."""
    def __init__(self, label, icon, fill=None):
        super().__init__(); self.label, self.icon_name, self.fill = label, icon, fill
        self.setCheckable(True); self.setCursor(Qt.CursorShape.PointingHandCursor); self.setMinimumHeight(66); self.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed)

    def paintEvent(self, _):
        p = QPainter(self); p.setRenderHint(QPainter.RenderHint.Antialiasing); r = QRectF(self.rect()).adjusted(1, 1, -1, -1); on = self.isChecked(); hot = self.underMouse()
        if on and self.fill: bg, fg, border = QColor(self.fill), "#06121f", QColor(self.fill)
        elif on: bg, fg, border = QColor("#243247"), ACCENT, QColor(ACCENT)
        else: bg, fg, border = QColor("#272b32" if hot else "#202328"), "#aab3c0", QColor("#1f2733")
        p.setBrush(bg); p.setPen(QPen(border, 1.4)); p.drawRoundedRect(r, 12, 12)
        px = icon_pixmap(self.icon_name, fg, 22); p.drawPixmap(int(r.center().x() - 11), int(r.top() + 10), px)
        f = QFont(self.font()); f.setBold(on); p.setFont(f); p.setPen(QColor(fg)); p.drawText(QRectF(r.left(), r.top() + 36, r.width(), r.height() - 38), Qt.AlignmentFlag.AlignCenter, self.label)


ACCENT = "#58a6ff"
STYLE = """
* { font-family: "Inter", "Noto Sans", "Segoe UI", sans-serif; font-size: 14px; }
QMainWindow, QWidget { background: #17181c; color: #e8edf3; }
#side { background: #121316; border-right: 1px solid #151a22; }
#brand { font-size: 17px; font-weight: 800; letter-spacing: 3px; color: #58a6ff; }
#model { color: #5d6877; font-size: 11px; }
QListWidget { background: transparent; border: none; outline: 0; }
QListWidget::item { padding: 11px 14px; border-radius: 10px; margin: 2px 0; color: #8d97a6; }
QListWidget::item:selected { background: #10252b; color: #58a6ff; }
QListWidget::item:hover:!selected { background: #11161d; color: #cfd6e0; }
#card { background: transparent; border: none; }
#cardtitle { font-size: 15px; font-weight: 700; color: #e8edf3; }
#tile { background: #1d1f24; border: 1px solid #1b222c; border-radius: 16px; }
#tilevalue { font-size: 26px; font-weight: 700; color: #ffffff; }
#tilelabel { font-size: 11px; letter-spacing: 1px; color: #6c7787; }
#hint { color: #6c7787; font-size: 12px; }
#h1 { font-size: 20px; font-weight: 700; }
#stats { color: #aab3c0; font-size: 13px; }
QPushButton { background: #171d26; border: 1px solid #222b37; border-radius: 10px; padding: 8px 16px; color: #dfe5ee; }
QPushButton:hover { background: #1d2530; border-color: #2c3847; }
QPushButton:pressed { background: #10252b; }
QWidget#segwrap { background: #202328; border: 1px solid #2e3239; border-radius: 10px; }
QPushButton#seg { background: transparent; border: none; border-radius: 8px; padding: 7px 14px; margin: 0; color: #8b929c; }
QPushButton#seg:hover { color: #dfe5ee; }
QPushButton#seg:checked { background: #363c46; color: #ffffff; font-weight: 700; }
QPushButton#tilebtn { background: #202328; border: 1px solid #1f2733; border-radius: 12px; padding: 8px 4px; color: #aab3c0; font-size: 13px; }
QPushButton#tilebtn:hover { background: #272b32; }
QPushButton#tilebtn:checked { background: #243247; color: #58a6ff; border: 1px solid #58a6ff; font-weight: 700; }
QPushButton#primary { background: #58a6ff; color: #031418; border: none; font-weight: 700; }
QPushButton#primary:hover { background: #4aeaff; }
QComboBox { background: #1b1d22; border: 1px solid #222b37; border-radius: 10px; padding: 7px 12px; min-height: 20px; }
QComboBox::drop-down { border: none; width: 24px; }
QComboBox QAbstractItemView { background: #1d1f24; border: 1px solid #222b37; selection-background-color: #243247; outline: 0; }
QLineEdit { background: #1b1d22; border: 1px solid #222b37; border-radius: 10px; padding: 7px 12px; }
QLineEdit:focus { border-color: #58a6ff; }
QCheckBox { spacing: 12px; background: transparent; }
QSlider::groove:horizontal { height: 5px; background: #2a2e35; border-radius: 2px; }
QSlider::sub-page:horizontal { background: #6f7f96; border-radius: 2px; }
QSlider::handle:horizontal { background: #aab6c8; border: 2px solid #3a404a; width: 14px; height: 14px; margin: -7px 0; border-radius: 9px; }
QSlider::handle:horizontal:hover { background: #d3dcea; }
QScrollArea { border: none; background: transparent; }
QScrollBar:vertical { background: transparent; width: 8px; } QScrollBar::handle:vertical { background: #232b36; border-radius: 4px; min-height: 30px; }
QScrollBar::add-line, QScrollBar::sub-line { height: 0; }
QStatusBar { background: #121316; color: #6c7787; border-top: 1px solid #151a22; }
QLabel { background: transparent; }
"""

FAN_MODES = ["Smart", "Auto", "Max", "Manual"]
KB_EFFECTS = ["static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"]
KEY_GROUPS = ["all", "wasd", "arrows", "fkeys", "numrow", "letters", "numpad", "esc", "space", "enter", "predator", "lshift"]
VOICE = ["off", "conference", "personal"]
GESTURES = [("tap", "Tap"), ("double", "Double tap"), ("triple", "Triple tap"), ("long", "Long press"), ("fn_esc", "Fn + Esc")]
START = [("last", "Last used"), ("default", "Stock"), ("off", "Off"), ("none", "Leave")]
JACK = [("auto", "Auto"), ("headphones", "Headphones"), ("headset", "Headset")]
JACK_HINT = {"auto": "Uses what the codec detects when you plug something in.",
             "headphones": "Output only: the headset microphone is never used.",
             "headset": "Always use the headset microphone, even if the codec has not detected it (try this if it is missing after boot)."}


PAGES = {"home": 0, "keys": 1, "audio": 2, "settings": 3}


class Segmented(QWidget if Qt else object):
    """Choice pills (PreySense's "Dark | Light"): the chosen one is raised and bold. tiles=True: separate icon tiles for the big mode buttons."""
    picked = pyqtSignal(int) if Qt else None

    def __init__(self, labels, parent=None, tiles=False, colors=None, icons=None):
        super().__init__(parent); h = QHBoxLayout(self); h.setContentsMargins(0, 0, 0, 0); h.setSpacing(8 if tiles else 0); self.group = QButtonGroup(self); self.group.setExclusive(True); self.btns = []
        if not tiles: self.setObjectName("segwrap"); self.setAttribute(Qt.WidgetAttribute.WA_StyledBackground, True); h.setContentsMargins(3, 3, 3, 3)
        for i, t in enumerate(labels):
            if tiles: b = TileButton(t, (icons or [""] * len(labels))[i], colors[i] if colors else None)
            else: b = QPushButton(t); b.setObjectName("seg"); b.setCheckable(True); b.setCursor(Qt.CursorShape.PointingHandCursor)
            self.group.addButton(b, i); h.addWidget(b); self.btns.append(b)
        self.group.idClicked.connect(self.picked.emit)

    def set_index(self, i):
        if 0 <= i < len(self.btns): self.btns[i].setChecked(True)
        else:
            self.group.setExclusive(False); [b.setChecked(False) for b in self.btns]; self.group.setExclusive(True)


class Bus(QObject if Qt else object):
    message = pyqtSignal(str) if Qt else None
    refreshed = pyqtSignal(dict) if Qt else None


class ColorButton(QPushButton if Qt else object):
    """A colour swatch; the colour dialog reports changes live while you drag in it."""
    changed = pyqtSignal(str) if Qt else None

    def __init__(self, hexcolor="00aec7", parent=None):
        super().__init__(parent); self.hex = hexcolor; self.setFixedSize(56, 30); self._paint(); self.clicked.connect(self._open)

    def _paint(self): self.setStyleSheet(f"background:#{self.hex}; border:2px solid #3a4452; border-radius:8px;")

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
        super().__init__(); self.setWindowTitle("TrueSense"); geo = QApplication.primaryScreen().availableGeometry(); self.setFixedSize(500, min(900, geo.height() - 60))   # a fixed size makes tiling WMs (Hyprland, sway) float it instead of stretching it
        self.bus = Bus(); self.bus.message.connect(self._msg); self.bus.refreshed.connect(self._apply); self.busy = {}; self.loading = False
        self.cfg = config.load(); self.src = ""
        self.pages = QStackedWidget(); self.setCentralWidget(self.pages); self.statusBar().showMessage("Ready"); self.names = []
        for build in (self.p_home, self.p_keys, self.p_audio, self.p_settings): build()
        self.pages.currentChanged.connect(lambda i: self.full_refresh("audio" if i == 2 else "home"))
        self.timer = QTimer(self); self.timer.timeout.connect(self.tick); self.timer.start(2000); self.full_refresh(); self.tick()

    # ---- plumbing --------------------------------------------------------------------------------------------------------------------
    def _msg(self, t): self.statusBar().showMessage(t, 6000)

    def do(self, fn, ok="done", refresh=True):
        """Run a backend call off the UI thread; show its result or its error. Only calls that change things the page does not already show re-read everything."""
        def work():
            try: r = fn(); self.bus.message.emit(ok(r) if callable(ok) else ok)
            except Exception as e: self.bus.message.emit(f"Error: {e}")
            if refresh: self.full_refresh()
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

    def page(self, title, heading):
        w = QWidget(); lay = QVBoxLayout(w); lay.setContentsMargins(22, 16, 22, 18); lay.setSpacing(12)
        sc = QScrollArea(); sc.setWidgetResizable(True); sc.setFrameShape(QFrame.Shape.NoFrame); sc.setHorizontalScrollBarPolicy(Qt.ScrollBarPolicy.ScrollBarAlwaysOff); sc.setWidget(w)
        self.pages.addWidget(sc); h = QHBoxLayout()
        if self.pages.count() > 1:
            back = self.btn(" Back", lambda: self.pages.setCurrentIndex(0)); back.setIcon(make_icon("back")); h.addWidget(back)
        t_ = QLabel(heading); t_.setObjectName("brand" if self.pages.count() == 1 else "h1"); h.addWidget(t_); h.addStretch(); self.head = h; lay.addLayout(h); return lay

    def card(self, lay, title, icon="", right=False):
        """A flat section like PreySense / G-Helper: icon and title on the left, a live value on the right."""
        c = QFrame(); c.setObjectName("card"); v = QVBoxLayout(c); v.setContentsMargins(0, 8, 0, 4); v.setSpacing(10)
        h = QHBoxLayout(); h.setSpacing(8)
        if icon:
            il = QLabel(); il.setPixmap(icon_pixmap(icon, "#dfe5ee", 20)); il.setFixedSize(22, 22); h.addWidget(il)
        t_ = QLabel(title); t_.setObjectName("cardtitle"); h.addWidget(t_); h.addStretch()
        v.right = None
        if right: v.right = QLabel(""); v.right.setObjectName("stats"); v.right.setMinimumWidth(190); v.right.setAlignment(Qt.AlignmentFlag.AlignRight | Qt.AlignmentFlag.AlignVCenter); h.addWidget(v.right)
        v.addLayout(h); lay.addWidget(c); return v

    @staticmethod
    def hint(text):
        l = QLabel(text); l.setObjectName("hint"); l.setWordWrap(True); return l

    def tile(self, label):
        f = QFrame(); f.setObjectName("tile"); v = QVBoxLayout(f); v.setContentsMargins(18, 14, 18, 14); v.setSpacing(2)
        val = QLabel("–"); val.setObjectName("tilevalue"); lab = QLabel(label.upper()); lab.setObjectName("tilelabel"); v.addWidget(val); v.addWidget(lab); f.val = val; return f

    def line(self, v, text, *widgets):
        h = QHBoxLayout(); h.setSpacing(10); lab = QLabel(text); h.addWidget(lab); h.addStretch()
        for w in widgets: h.addWidget(w)
        v.addLayout(h); return lab

    def switch(self, v, text, cb, sub=None):
        c = Switch(); c.toggled.connect(lambda x: None if self.loading else cb(x)); self.line(v, text, c)
        if sub: v.addWidget(self.hint(sub))
        return c

    def seg(self, v, text, labels, cb):
        s = Segmented(labels); s.picked.connect(lambda i: None if self.loading else cb(i)); self.line(v, text, s); return s

    def slider(self, v, text, lo, hi, step, cb, final=None, suffix=""):
        """Like PreySense: the name on the left, the current value on the right, a full-width slider underneath."""
        s = QSlider(Qt.Orientation.Horizontal); s.setRange(lo, hi); s.setSingleStep(step); s.setPageStep(step)
        val = QLabel(f"{s.value()}{suffix}"); val.setObjectName("stats"); s.vlabel = val
        s.valueChanged.connect(lambda x: val.setText(f"{x}{suffix}")); s.valueChanged.connect(lambda x: None if self.loading else cb(x))
        if final: s.sliderReleased.connect(lambda: final(s.value()))
        h = QHBoxLayout(); h.addWidget(QLabel(text)); h.addStretch(); h.addWidget(val); v.addLayout(h); v.addWidget(s); return s

    def btn(self, text, cb, primary=False):
        b = QPushButton(text); b.setCursor(Qt.CursorShape.PointingHandCursor)
        if primary: b.setObjectName("primary")
        b.clicked.connect(lambda *_: cb()); return b

    # ---- Home (everything you reach for daily, on one compact page) ------------------------------------------------------------------
    def p_home(self):
        lay = self.page("Home", "TrueSense")
        v = self.card(lay, "Performance Mode", "performance", True); self.stat_cpu = v.right
        try: self.modes = hw.supported_modes()
        except Exception: self.modes = list(hw.MODES)
        order = [m for m in ("eco", "quiet", "balanced", "performance", "turbo") if m in self.modes] or self.modes; self.modes = order
        cols = {"eco": "#5de0a0", "quiet": "#5db8ff", "balanced": "#58a6ff", "performance": "#ffb347", "turbo": "#ff5d6c"}
        self.power = Segmented([m.capitalize() for m in order], tiles=True, icons=[m if m in _P else "balanced" for m in order], colors=[cols.get(m, "#58a6ff") for m in order]); v.addWidget(self.power)
        self.power.picked.connect(lambda i: None if self.loading else self.do(lambda: control.power(order[i], 30), lambda r: f"Power mode: {r} (held 30 min)", refresh=False))
        self.auto = self.switch(v, "Choose automatically", lambda x: self.do(lambda: ipc.call("set", key="auto_profile", value=x), "Saved", refresh=False), "By power source and load. A mode you pick holds for 30 minutes.")
        v = self.card(lay, "Fans", "fan", True); self.stat_fan = v.right
        self.fan = Segmented(FAN_MODES, tiles=True, icons=["smart", "auto", "max", "manual"]); self.fan.picked.connect(lambda i: None if self.loading else self.fan_apply(i)); v.addWidget(self.fan)
        self.fan_pct = self.slider(v, "Manual speed", 20, 100, 5, lambda x: None, lambda x: self.fan_apply(3), suffix=" %")
        v = self.card(lay, "GPU Mode", "gpu", True); self.stat_gpu = v.right
        self.gpu = Segmented([m.capitalize() for m in hw.GPU_MODES], tiles=True, icons=["optimus", "gpu", "auto"]); v.addWidget(self.gpu)
        self.gpu.picked.connect(lambda i: None if self.loading else self.do(lambda: hw.set_gpu_mode(list(hw.GPU_MODES)[i]), lambda r: f"GPU mode: {r} (reboot to apply)", refresh=False)); v.addWidget(self.hint("Takes effect after a reboot."))
        v = self.card(lay, "Keyboard", "keyboard")
        self.kb_on = self.switch(v, "Backlight", lambda x: self.do(lambda: actions.run("keyboard:on" if x else "keyboard:off"), lambda r: r))
        self.kb_level = self.slider(v, "Brightness", 0, 50, 5, lambda x: self.live("kbl", lambda: keyboard.set_brightness(x)))
        self.kb_fx = QComboBox(); self.kb_fx.addItems([e.capitalize() for e in KB_EFFECTS]); self.kb_color = ColorButton("00aec7")
        self.kb_fx.activated.connect(lambda i: self.do(lambda: keyboard.set_effect(KB_EFFECTS[i], self.kb_color.hex, self.kb_speed.value() or 5, max(self.kb_level.value(), 50 if KB_EFFECTS[i] == "neon" else 10)), "Effect applied", refresh=False))
        self.kb_color.changed.connect(lambda h_: self.live("kbc", lambda: keyboard.set_color(h_)))
        self.line(v, "Effect and colour", self.kb_fx, self.kb_color)
        self.kb_speed = self.slider(v, "Effect speed", 1, 11, 1, lambda x: self.live("kbs", lambda: keyboard.set_speed(x)))
        v = self.card(lay, "Rear Light Bar", "bar")
        self.rear_on = self.switch(v, "Light", lambda x: self.do(lambda: actions.run("rear:on" if x else "rear:off"), lambda r: r, refresh=False))
        self.rear_fx = self.seg(v, "Effect", [m.capitalize() for m in hw.REAR_MODES], lambda i: self.rear_apply())
        self.zc = [ColorButton("00aec7") for _ in range(3)]; self.rear_color = ColorButton("00aec7")
        for b in self.zc + [self.rear_color]: b.changed.connect(lambda h_: self.rear_apply())
        self.line(v, "Static: left · middle · right", *self.zc); self.line(v, "Colour (other effects)", self.rear_color)
        self.rear_speed = self.slider(v, "Speed", 1, 5, 1, lambda x: self.rear_apply()); self.rear_bright = self.slider(v, "Brightness", 0, 100, 10, lambda x: self.rear_apply(), suffix=" %")
        v = self.card(lay, "Battery and Screen", "battery", True); self.stat_bat = v.right
        self.limit = self.switch(v, "80 % charge limit", lambda x: self.do(lambda: hw.set_battery_limit(x), lambda r: f"80 % limit: {'on' if r else 'off'}", refresh=False))
        self.od = self.switch(v, "LCD overdrive", lambda x: self.do(lambda: hw.set_overdrive(x), lambda r: f"Overdrive: {'on' if r else 'off'}", refresh=False))
        self.usb = self.seg(v, "USB charging while off", ["Off", "10 %", "20 %", "30 %"], lambda i: self.do(lambda: hw.set_usb_charge(hw.USB_LEVELS[i]), lambda r: f"USB charge stops at {r} %", refresh=False))
        g = QGridLayout(); g.setSpacing(8)
        for n, (label, ic, cb) in enumerate((("Per-key colours", "palette", lambda: self.pages.setCurrentIndex(1)), ("Audio", "audio", lambda: self.pages.setCurrentIndex(2)),
                                              ("Settings", "settings", lambda: self.pages.setCurrentIndex(3)), ("Quit", "power", self.close))):
            b_ = self.btn(label, cb); b_.setIcon(make_icon(ic)); b_.setIconSize(QSize(18, 18)); b_.setMinimumHeight(42); g.addWidget(b_, n // 2, n % 2)
        lay.addLayout(g); lay.addStretch()

    def fan_apply(self, i):
        mode = ("smart", "auto", "max", "manual")[i]; p = self.fan_pct.value()
        self.do(lambda: control.fan(mode, p if mode == "manual" else None), lambda r: {"smart": "Fans: smart", "auto": "Fans: firmware auto", "max": "Fans: MAX", "manual": f"Fans: manual {p} %"}[mode], refresh=False)

    def rear_apply(self):
        def go():
            mode = list(hw.REAR_MODES)[max(0, self.rear_fx.group.checkedId())]; sp, br = self.rear_speed.value() or 3, self.rear_bright.value()
            if mode == "static": hw.rear_set("static", speed=sp, brightness=br, zones=[b.hex for b in self.zc])
            else: hw.rear_set(mode, self.rear_color.hex, sp, br)
        if not self.loading: self.live("rear", go)


    # ---- Per-key -----------------------------------------------------------------------------------------------------------------------
    def p_keys(self):
        lay = self.page("Per-key", "Per-key colours"); v = self.card(lay, "Keyboard")
        v.addWidget(self.hint("Click or drag to choose keys (Ctrl adds more), then pick a colour. Only the chosen keys change."))
        self.kv = KeyboardView(); self.kv.selection_changed.connect(self.sel_changed); hk = QHBoxLayout(); hk.addWidget(self.kv); hk.addStretch(); v.addLayout(hk)
        for chunk in (KEY_GROUPS[:6], KEY_GROUPS[6:]):
            h = QHBoxLayout(); h.setSpacing(6)
            for g in chunk: h.addWidget(self.btn(g, lambda g=g: self.kv.select_group(g)))
            h.addStretch(); v.addLayout(h)
        self.pk_color = ColorButton("ff0000"); self.pk_color.changed.connect(lambda h_: self.paint_sel()); self.sel_lbl = QLabel("no keys chosen"); self.sel_lbl.setObjectName("hint")
        self.line(v, "Colour of the chosen keys", self.pk_color, self.btn("Paint", self.paint_sel, True), self.btn("Black", lambda: (self.pk_color.set_hex("000000"), self.paint_sel())))
        v.addWidget(self.sel_lbl)
        v = self.card(lay, "Presets")
        self.line(v, "Whole keyboard", self.btn("Rainbow", lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.rainbow()), None), "Rainbow")),
                  self.btn("Fill colour", lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.canvas(keymap.hexcolor(self.pk_color.hex))), None), "Filled")),
                  self.btn("Stock", lambda: self.do(lambda: keyboard.restore_default(None), "Stock look")))
        lay.addStretch()

    def sel_changed(self):
        names = sorted({self.kv.labels[c] for c in self.kv.sel if c in self.kv.labels}); self.sel_lbl.setText(f"{len(self.kv.sel)} key(s): " + ", ".join(names[:10]) + ("…" if len(names) > 10 else ""))

    def paint_sel(self):
        if not self.kv.sel or self.loading: return
        hexc = self.pk_color.hex; ch = {c: hexc for c in self.kv.sel}
        for c in self.kv.sel: self.kv.cells[c] = hexc
        self.kv.update(); self.live("paint", lambda: keyboard.paint_cells(ch))

    # ---- Audio -------------------------------------------------------------------------------------------------------------------------
    def p_audio(self):
        lay = self.page("Audio", "Audio"); v = self.card(lay, "Output")
        self.out = self.seg(v, "Play sound on", ["Speakers", "Headphones"], lambda i: self.do(lambda: audio.set_output(("speakers", "headphones")[i]), f"Output: {('speakers', 'headphones')[i]}"))
        v.addWidget(self.hint("This laptop plays on the speakers or the headphone jack, not both at once."))
        self.dts = self.switch(v, "Acer DTS sound", lambda x: self.do(lambda: audio.dts_power(x), "DTS " + ("on" if x else "off")), "Through AcerUltraX.")
        v = self.card(lay, "3.5 mm jack")
        self.jack = self.seg(v, "I plugged in", [l for _, l in JACK], self.jack_pick); self.jack_hint = self.hint(""); v.addWidget(self.jack_hint)
        self.jack_state = QLabel("…"); self.jack_state.setObjectName("hint"); v.addWidget(self.jack_state)
        self.boost = self.slider(v, "Headset mic boost", 0, 3, 1, lambda x: None, lambda x: self.do(lambda: audio.headset_boost(x), lambda r: f"Boost: {r}"))
        v = self.card(lay, "Microphone")
        self.mute = self.switch(v, "Mute every microphone", lambda x: self.do(lambda: audio.mic_set_mute(x), "Microphones muted" if x else "Microphones live"))
        v.addWidget(self.hint("Purified Voice: AI noise reduction. Conference is gentle, Personal is strict."))
        self.v_head = self.seg(v, "Headset microphone", [x.capitalize() for x in VOICE], lambda i: self.do(lambda: (audio.voice_set("headset", VOICE[i]), audio.apply_mic_policy(False))[0], f"Headset mic: {VOICE[i]}"))
        self.v_arr = self.seg(v, "Built-in microphones", [x.capitalize() for x in VOICE], lambda i: self.do(lambda: (audio.voice_set("array", VOICE[i]), audio.apply_mic_policy(False))[0], f"Built-in mics: {VOICE[i]}"))
        self.v_note = self.switch(v, "Notify when the microphone changes", lambda x: self.do(lambda: config.write_user({"audio": {"notify": x}}), "Saved")); lay.addStretch()

    def jack_pick(self, i):
        val = JACK[i][0]; self.jack_hint.setText(JACK_HINT[val])
        self.do(lambda: (config.write_user({"audio": {"jack": val}}), audio.apply_mic_policy(False))[0], f"3.5 mm jack: {val}")

    # ---- Settings ----------------------------------------------------------------------------------------------------------------------
    def p_settings(self):
        lay = self.page("Settings", "Settings"); v = self.card(lay, "Hardware extras")
        self.kb_timeout = self.switch(v, "Keyboard light off after 30 s without typing", lambda x: self.do(lambda: hw.set_kb_timeout(x), "Saved", refresh=False))
        self.boot = self.switch(v, "Boot animation and sound", lambda x: self.do(lambda: hw.set_boot_sound(x), lambda r: f"Boot sound: {'on' if r else 'off'}", refresh=False))
        self.calib = self.switch(v, "Battery calibration", lambda x: self.do(lambda: hw.set_calibration(x), lambda r: f"Calibration: {'on' if r else 'off'}", refresh=False), "Needs AC power and takes hours. Switch off to cancel.")
        v = self.card(lay, "CPU power limits")
        v.addWidget(self.hint("Sustained (PL1) and burst (PL2) watts. The daemon also sets these itself per mode; use this to try a value. Range 25 to 120 W."))
        self.pl1, self.pl2 = QDoubleSpinBox(), QDoubleSpinBox()
        for s_, val in ((self.pl1, 65), (self.pl2, 80)): s_.setRange(25, 120); s_.setDecimals(0); s_.setSuffix(" W"); s_.setValue(val)
        self.line(v, "PL1 · PL2", self.pl1, self.pl2, self.btn("Apply", lambda: self.do(lambda: ipc.call("cpu_limits", pl1=self.pl1.value(), pl2=self.pl2.value()), "CPU limits applied", refresh=False)))
        v = self.card(lay, "PREDATOR key")
        v.addWidget(self.hint("What each press does. Examples: power-next, power:turbo, profile:gaming, output-toggle, mic-toggle, exec:firefox. See `truesense keys actions`."))
        self.key_edits = {}
        for g, label in GESTURES:
            e = QLineEdit(self.cfg["keys"][g]); e.setMinimumWidth(240); self.line(v, label, e, self.btn("Save", lambda g=g, e=e: self.save_key(g, e)), self.btn("Test", lambda e=e: self.do(lambda: actions.run(e.text()) or "ok", lambda r: str(r))))
            self.key_edits[g] = e
        self.line(v, "Saved changes apply after", self.btn("Restart key service", lambda: self.do(lambda: __import__("subprocess").run(["systemctl", "--user", "restart", "truesense-keys"], check=False), "Key service restarted")))
        v = self.card(lay, "At login"); v.addWidget(self.hint("Lighting is saved on every change. Choose what comes back when you log in."))
        s = self.cfg["startup"]; keys = [k for k, _ in START]
        for key, title in (("keyboard", "Keyboard"), ("rear", "Rear light")):
            sg = Segmented([l for _, l in START]); sg.set_index(keys.index(s[key]) if s[key] in keys else 0)
            sg.picked.connect(lambda i, key=key: self.do(lambda: config.write_user({"startup": {key: keys[i]}}), f"{key} at login: {keys[i]}")); self.line(v, title, sg)
        from . import startup
        self.line(v, "Try it now", self.btn("Apply", lambda: self.do(lambda: startup.apply(), lambda r: "; ".join(r))))
        lay.addStretch()

    def save_key(self, g, e):
        err = actions.validate(e.text())
        if err: self.statusBar().showMessage(err, 8000); return
        self.do(lambda: config.write_user({"keys": {g: e.text()}}), f"{g}: saved")

    # ---- refresh -----------------------------------------------------------------------------------------------------------------------
    def tick(self):
        if not self.isVisible() or self.isMinimized(): return
        try:
            s = hw.sensors(); self.stat_cpu.setText(f"CPU: {s['cpu_temp']} °C · {s['cpu_fan_rpm']} RPM"); self.stat_gpu.setText(f"GPU: {s['gpu_temp']} °C · {s['gpu_fan_rpm']} RPM")
            self.stat_fan.setText(f"{max(s['cpu_fan_rpm'], s['gpu_fan_rpm'])} RPM"); self.stat_bat.setText(self.src)
        except HwError: pass

    def full_refresh(self, scope=None):
        """Read the hardware off the UI thread. The audio page (pactl, amixer, DTS) is only read while it is shown, so the main page stays quick."""
        scope = scope or ("audio" if self.pages.currentIndex() == 2 else "home")
        def work():
            d = {}
            def get(k, fn):
                try: d[k] = fn()
                except Exception: pass
            if scope == "audio":
                get("out", audio.output_status); get("jack", audio.jack_status); get("mic", audio.mic_state); get("boost", audio.headset_boost); get("voice", audio.voice_modes)
                get("jackset", lambda: config.load()["audio"]["jack"]); get("dts", lambda: "DTS:            on" in audio.dts_status())
            else:
                get("state", lambda: ipc.call("state")); get("mode", hw.get_mode); get("fan", hw.fan_state); get("rear", hw.rear_get); get("kb", keyboard.get_brightness)
                get("kbtimeout", hw.get_kb_timeout); get("limit", hw.get_battery_limit); get("calib", hw.get_calibration); get("usb", hw.get_usb_charge); get("od", hw.get_overdrive)
                get("boot", hw.get_boot_sound); get("gpu", hw.get_gpu_mode); get("look", keyboard.last); get("cells", keyboard.current_cells); get("ac", hw.on_ac)
            self.bus.refreshed.emit(d)
        threading.Thread(target=work, daemon=True).start()

    def _apply(self, d):
        self.loading = True
        try:
            if "state" in d:
                s = d["state"]["settings"]; self.auto.setChecked(bool(s["auto_profile"]))
                if d.get("fan", {}).get("mode") == "manual": self.fan_pct.setValue(max(20, d["fan"]["duty_register_pct"]["cpu"]))
                self.fan.set_index(0 if s["smart_fan"] else {"auto": 1, "max": 2, "manual": 3}.get(d.get("fan", {}).get("mode"), 1))
            if "ac" in d: self.src = "AC" if d["ac"] else "Battery"
            if d.get("mode") in self.modes: self.power.set_index(self.modes.index(d["mode"]))
            if "rear" in d:
                r = d["rear"]; self.rear_on.setChecked(r["brightness"] > 0); self.rear_bright.setValue(r["brightness"]); self.rear_speed.setValue(r["speed"])
                self.rear_fx.set_index(list(hw.REAR_MODES).index(r["mode"]) if r["mode"] in hw.REAR_MODES else -1); self.rear_color.set_hex(r["color"])
                zs = r.get("zones") or userstate.get("rear_zones")
                for b, z in zip(self.zc, zs if isinstance(zs, list) and len(zs) == 3 else [r["color"]] * 3): b.set_hex(z)
            if "kb" in d: self.kb_level.setValue(d["kb"]); self.kb_on.setChecked(d["kb"] > 0)
            look = d.get("look") or {}
            if look.get("mode") == "effect" and look.get("name") in KB_EFFECTS: self.kb_fx.setCurrentIndex(KB_EFFECTS.index(look["name"])); self.kb_color.set_hex(look.get("color", "00aec7")); self.kb_speed.setValue(look.get("speed", 5))
            if d.get("kbtimeout") is not None: self.kb_timeout.setChecked(bool(d["kbtimeout"]))
            if "cells" in d and not self.kv.sel: self.kv.cells = ["%02x%02x%02x" % tuple(c) for c in d["cells"]]; self.kv.update()
            for k, w in (("limit", self.limit), ("calib", self.calib), ("od", self.od), ("boot", self.boot), ("dts", self.dts)):
                if d.get(k) is not None: w.setChecked(bool(d[k]))
            if "usb" in d: self.usb.set_index(hw.USB_LEVELS.index(d["usb"]) if d["usb"] in hw.USB_LEVELS else -1)
            if "gpu" in d: self.gpu.set_index(list(hw.GPU_MODES).index(d["gpu"]) if d["gpu"] in hw.GPU_MODES else -1)
            if "out" in d: self.out.set_index({"speakers": 0, "headphones": 1}.get(d["out"]["active"], -1))
            if "jackset" in d:
                keys = [k for k, _ in JACK]; self.jack.set_index(keys.index(d["jackset"]) if d["jackset"] in keys else 0); self.jack_hint.setText(JACK_HINT.get(d["jackset"], ""))
            if "jack" in d: j = d["jack"]; self.jack_state.setText("Detected now: " + ("headset with microphone" if j["headset_mic"] else "headphones" if j["headphones"] else "nothing plugged in"))
            if "mic" in d: self.mute.setChecked(bool(d["mic"]["muted"]))
            if d.get("boost") is not None: self.boost.setValue(d["boost"])
            if "voice" in d:
                v = d["voice"]; self.v_head.set_index(VOICE.index(v["headset"]) if v.get("headset") in VOICE else 0); self.v_arr.set_index(VOICE.index(v["array"]) if v.get("array") in VOICE else 0)
            if "voice" in d: self.v_note.setChecked(bool(config.load()["audio"]["notify"]))
        finally: self.loading = False


def _icon():
    """A small drawn app icon (no image files to ship): a cyan ring with a T."""
    pm = QPixmap(128, 128); pm.fill(Qt.GlobalColor.transparent); p = QPainter(pm); p.setRenderHint(QPainter.RenderHint.Antialiasing)
    p.setBrush(QColor("#17181c")); p.setPen(QPen(QColor(ACCENT), 9)); p.drawEllipse(10, 10, 108, 108)
    f = QFont(); f.setBold(True); f.setPixelSize(70); p.setFont(f); p.setPen(QColor(ACCENT)); p.drawText(pm.rect(), Qt.AlignmentFlag.AlignCenter, "T"); p.end(); return QIcon(pm)


def main(page=None):
    if Qt is None:
        print(f"The window needs python-pyqt6 ({_IMPORT_ERROR}).  Arch: sudo pacman -S python-pyqt6.  Everything works from the command line without it.")
        return 1
    __import__("os").environ.setdefault("QT_LOGGING_RULES", "qt.qpa.wayland.textinput=false")
    app = QApplication(sys.argv[:1]); app.setApplicationName("TrueSense"); app.setWindowIcon(_icon()); app.setDesktopFileName("truesense"); app.setStyle("Fusion"); app.setStyleSheet(STYLE)
    name = "truesense-gui-" + str(__import__("os").getuid())
    probe = QLocalSocket(); probe.connectToServer(name)
    if probe.waitForConnected(150):                                   # already open: bring that window forward instead of starting a second one
        probe.write((page or "home").encode()); probe.flush(); probe.waitForBytesWritten(300); return 0
    QLocalServer.removeServer(name); server = QLocalServer(); server.listen(name)
    w = Window(); w.show()
    if page in PAGES: w.pages.setCurrentIndex(PAGES[page])
    def handoff():
        c = server.nextPendingConnection()
        if c and c.waitForReadyRead(300):
            p_ = bytes(c.readAll()).decode()
            if p_ in PAGES: w.pages.setCurrentIndex(PAGES[p_])
        w.showNormal(); w.raise_(); w.activateWindow()
    server.newConnection.connect(handoff)
    return app.exec()
