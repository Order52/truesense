"""Optional window for TrueSense (Qt 6 via PyQt6). A thin layer over the same functions the CLI uses; nothing else depends on it.

Run: truesense gui        Needs python-pyqt6 (Arch: `pacman -S python-pyqt6`); runs natively on Wayland (Hyprland) and X11.
"""
import os, sys, threading, time
from . import actions, audio, backend as hw, config, control, curves, ipc, keyboard, keymap, looks, profiles, smartprofiles, userstate
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
        f = QFont(self.font()); f.setBold(on); f.setPixelSize(12); p.setFont(f); p.setPen(QColor(fg)); p.drawText(QRectF(r.left(), r.top() + 36, r.width(), r.height() - 38), Qt.AlignmentFlag.AlignCenter, self.label)


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
#sep { background: #272a30; max-height: 1px; min-height: 1px; border: none; }
#section { font-size: 11px; font-weight: 700; letter-spacing: 1.5px; color: #7d8590; padding-top: 6px; }
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
QPushButton#quiet { background: transparent; border: 1px solid #2e3239; padding: 5px 12px; }
QPushButton#primary { background: #58a6ff; color: #031418; border: none; font-weight: 700; }
QPushButton#primary:hover { background: #4aeaff; }
QComboBox { background: #1b1d22; border: 1px solid #222b37; border-radius: 10px; padding: 7px 12px; min-height: 20px; }
QComboBox::drop-down { border: none; width: 24px; }
QComboBox:disabled { color: #666c76; }
QSlider::handle:horizontal:disabled { background: #4a505a; border-color: #2e3239; }
QSlider::sub-page:horizontal:disabled { background: #3a404a; }
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

AUTO_KEYS = ["off", "native", "smart"]
AUTO_HINT = {"off": "You choose the mode. A mode you pick stays until you change it.",
             "native": "Like PredatorSense on Windows: Balanced on power, Eco on battery, nothing else.",
             "smart": "Switches by heat, load and battery: Performance and Turbo when gaming or heavy work (backs off if it overheats), Eco on battery."}
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


AUTOSTART = os.path.expanduser("~/.config/autostart/truesense.desktop")
PAGES = {"home": 0, "lighting": 1, "keys": 2, "battery": 3, "audio": 4, "settings": 5, "curves": 6, "smart": 7}


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
    boot_text = pyqtSignal(str) if Qt else None
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


class CurveEditor(QWidget if Qt else object):
    """Fan curve graph: drag a point to move it, double-click empty space to add one, right-click a point to remove it."""
    changed = pyqtSignal() if Qt else None
    T0, T1 = 30, 100

    def __init__(self):
        super().__init__(); self.setFixedSize(432, 250); self.setMouseTracking(True); self.curve = curves.load("truesense"); self.side = "cpu"; self.drag = None; self.locked = False; self.marks = {"cpu": None, "gpu": None}

    def set_curve(self, c): self.curve = c; self.update()
    def pts(self): return self.curve[self.side]
    def _xy(self, t, p): return 38 + (t - self.T0) / (self.T1 - self.T0) * (self.width() - 52), 14 + (1 - p / 100) * (self.height() - 44)
    def _tp(self, x, y): return self.T0 + (x - 38) / (self.width() - 52) * (self.T1 - self.T0), (1 - (y - 14) / (self.height() - 44)) * 100

    def paintEvent(self, _):
        p = QPainter(self); p.setRenderHint(QPainter.RenderHint.Antialiasing); p.fillRect(self.rect(), QColor("#1b1d22")); f = QFont(); f.setPixelSize(10); p.setFont(f)
        for pct in range(0, 101, 20):
            x0, y = self._xy(self.T0, pct); x1, _ = self._xy(self.T1, pct); p.setPen(QPen(QColor("#2b2f36"), 1)); p.drawLine(int(x0), int(y), int(x1), int(y)); p.setPen(QColor("#8b929c")); p.drawText(4, int(y) + 4, f"{pct}%")
        for t_ in range(self.T0, self.T1 + 1, 10):
            x, y0 = self._xy(t_, 0); _, y1 = self._xy(t_, 100); p.setPen(QPen(QColor("#2b2f36"), 1)); p.drawLine(int(x), int(y1), int(x), int(y0)); p.setPen(QColor("#8b929c")); p.drawText(int(x) - 10, int(y0) + 14, f"{t_}°")
        for side, col in (("gpu", "#6f7f96"), ("cpu", ACCENT)):
            pts = self.curve[side]; on = side == self.side; path = [self._xy(self.T0, pts[0][1])] + [self._xy(t_, d) for t_, d in pts] + [self._xy(self.T1, pts[-1][1])]
            p.setPen(QPen(QColor(col), 2.4 if on else 1.4)); [p.drawLine(int(a[0]), int(a[1]), int(b[0]), int(b[1])) for a, b in zip(path, path[1:])]
            if on:
                for t_, d in pts: x, y = self._xy(t_, d); p.setBrush(QColor("#5b6270" if self.locked else "#ffffff")); p.setPen(QPen(QColor(col), 2)); p.drawEllipse(QRectF(x - 5, y - 5, 10, 10))
            m = self.marks.get(side)
            if m: x, _ = self._xy(m, 0); _, y0 = self._xy(0, 100); p.setPen(QPen(QColor(col), 1, Qt.PenStyle.DashLine)); p.drawLine(int(x), int(y0), int(x), self.height() - 30)
        p.end()

    def _hit(self, pos):
        for i, (t_, d) in enumerate(self.pts()):
            x, y = self._xy(t_, d)
            if (x - pos.x()) ** 2 + (y - pos.y()) ** 2 < 144: return i
        return None

    def mousePressEvent(self, e):
        if self.locked: return
        i = self._hit(e.position())
        if e.button() == Qt.MouseButton.RightButton and i is not None and len(self.pts()) > 2: self.pts().pop(i); self.update(); self.changed.emit(); return
        self.drag = i

    def mouseMoveEvent(self, e):
        if self.locked or self.drag is None or not (e.buttons() & Qt.MouseButton.LeftButton): return
        pts = self.pts(); t_, d = self._tp(e.position().x(), e.position().y()); i = self.drag
        lo = pts[i - 1][0] + 1 if i > 0 else self.T0; hi = pts[i + 1][0] - 1 if i < len(pts) - 1 else self.T1 + 10
        d = 0 if d < 12 else max(20, min(100, int(5 * round(d / 5)))); t_ = int(max(lo, min(hi, round(t_))))
        d = max(d, pts[i - 1][1] if i > 0 else 0); d = min(d, pts[i + 1][1] if i < len(pts) - 1 else 100)       # the duty may only rise with temperature
        pts[i] = [t_, d]; self.update()

    def mouseReleaseEvent(self, e):
        if self.drag is not None: self.drag = None; self.changed.emit()

    def mouseDoubleClickEvent(self, e):
        if self.locked or self._hit(e.position()) is not None: return
        t_, d = self._tp(e.position().x(), e.position().y()); t_ = int(round(t_)); pts = self.pts()
        if any(abs(t_ - q[0]) < 2 for q in pts) or len(pts) >= 12: return
        d = max(20, min(100, int(5 * round(curves.interp(pts, t_) / 5)))) if curves.interp(pts, t_) >= 12 else 0; pts.append([t_, d]); pts.sort(); self.update(); self.changed.emit()


class Window(QMainWindow if Qt else object):
    def __init__(self):
        super().__init__(); self.setWindowTitle("TrueSense")
        geo = QApplication.primaryScreen().availableGeometry(); self.setFixedSize(480, min(760, geo.height() - 60))   # a fixed size makes tiling WMs (Hyprland, sway) float it
        self.bus = Bus(); self.bus.message.connect(self._msg); self.bus.boot_text.connect(lambda s: self.boot_lbl.setText(s)); self.bus.refreshed.connect(self._apply); self.busy = {}; self.pending = {}; self.loading = False; self.last_action = 0.0; self.refreshing = False; self.ticks = 0; self.rear_last = 100
        self.cfg = config.load(); self.src = ""
        self.pages = QStackedWidget(); self.setCentralWidget(self.pages); self.statusBar().showMessage("Ready")
        for build in (self.p_home, self.p_lighting, self.p_keys, self.p_battery, self.p_audio, self.p_settings, self.p_curves, self.p_smart): build()
        self.pages.currentChanged.connect(lambda i: self.full_refresh("audio" if i == PAGES["audio"] else "home"))
        self.timer = QTimer(self); self.timer.timeout.connect(self.tick); self.timer.start(1000); self.full_refresh(); self.tick()

    # ---- plumbing --------------------------------------------------------------------------------------------------------------------
    def say(self, t):
        try: self.bus.message.emit(t)
        except RuntimeError: pass                      # the window closed while a worker thread was finishing

    def say_refresh(self, d):
        try: self.bus.refreshed.emit(d)
        except RuntimeError: pass

    def say_boot(self, s):
        try: self.bus.boot_text.emit(s)
        except RuntimeError: pass

    def _msg(self, t):
        self.statusBar().setStyleSheet("QStatusBar { color: #ff7b72; }" if t.startswith("Error") else "")
        self.statusBar().showMessage(t, 9000 if t.startswith("Error") else 6000)

    def go(self, name): self.pages.setCurrentIndex(PAGES[name])

    def do(self, fn, ok="done", refresh=True):
        """Run a backend call off the UI thread; show its result or its error."""
        def work():
            try: r = fn(); self.say(ok(r) if callable(ok) else ok)
            except Exception as e: self.say(f"Error: {e}")
            if refresh: self.full_refresh()
        threading.Thread(target=work, daemon=True).start()

    def live(self, key, fn):
        """For colours and sliders while dragging: one write at a time, and the NEWEST request is always the one that ends up applied."""
        self.last_action = time.monotonic()
        if self.busy.get(key): self.pending[key] = fn; return
        self.busy[key] = True
        def work():
            try: fn()
            except Exception as e: self.say(f"Error: {e}")
            self.busy[key] = None; nxt = self.pending.pop(key, None)
            if nxt: self.live(key, nxt)
        threading.Thread(target=work, daemon=True).start()

    def page(self, heading, back=True):
        w = QWidget(); lay = QVBoxLayout(w); lay.setContentsMargins(24, 18, 24, 20); lay.setSpacing(14)
        sc = QScrollArea(); sc.setWidgetResizable(True); sc.setFrameShape(QFrame.Shape.NoFrame); sc.setHorizontalScrollBarPolicy(Qt.ScrollBarPolicy.ScrollBarAlwaysOff); sc.setWidget(w); self.pages.addWidget(sc)
        h = QHBoxLayout(); h.setSpacing(10)
        if back:
            b = self.btn(" Back", lambda: self.pages.setCurrentIndex(0)); b.setIcon(make_icon("back")); b.setObjectName("quiet"); h.addWidget(b)
        t_ = QLabel(heading); t_.setObjectName("brand" if not back else "h1"); h.addWidget(t_); h.addStretch(); lay.addLayout(h); return lay

    def card(self, lay, title, icon="", right=False):
        """A plain section: icon and title on the left, a live value on the right, rows underneath."""
        c = QFrame(); c.setObjectName("card"); v = QVBoxLayout(c); v.setContentsMargins(0, 4, 0, 2); v.setSpacing(10)
        h = QHBoxLayout(); h.setSpacing(8)
        if icon:
            il = QLabel(); il.setPixmap(icon_pixmap(icon, "#dfe5ee", 20)); il.setFixedSize(22, 22); h.addWidget(il)
        t_ = QLabel(title); t_.setObjectName("cardtitle"); h.addWidget(t_); h.addStretch(); v.right = None
        if right: v.right = QLabel(""); v.right.setObjectName("stats"); v.right.setMinimumWidth(190); v.right.setAlignment(Qt.AlignmentFlag.AlignRight | Qt.AlignmentFlag.AlignVCenter); h.addWidget(v.right)
        v.addLayout(h); lay.addWidget(c); return v

    @staticmethod
    def hint(text):
        l = QLabel(text); l.setObjectName("hint"); l.setWordWrap(True); return l

    @staticmethod
    def sep(v):
        f = QFrame(); f.setObjectName("sep"); v.addWidget(f)

    def line(self, v, text, *widgets, sub=None):
        """One settings row: the name on the left, the control on the right."""
        h = QHBoxLayout(); h.setSpacing(10); h.setContentsMargins(0, 2, 0, 2); lab = QLabel(text); lab.setWordWrap(True); h.addWidget(lab, 1)
        for w in widgets: h.addWidget(w)
        v.addLayout(h)
        if sub: v.addWidget(self.hint(sub))
        return lab

    def switch(self, v, text, cb, sub=None):
        c = Switch(); c.toggled.connect(lambda x: None if self.loading else cb(x)); self.line(v, text, c, sub=sub); return c

    def seg(self, v, text, labels, cb):
        s = Segmented(labels); s.picked.connect(lambda i: None if self.loading else cb(i)); self.line(v, text, s); return s

    def slider(self, v, text, lo, hi, step, cb, final=None, suffix=""):
        """The name on the left, the current value on the right, a full-width slider underneath."""
        s = QSlider(Qt.Orientation.Horizontal); s.setRange(lo, hi); s.setSingleStep(step); s.setPageStep(step)
        val = QLabel(f"{s.value()}{suffix}"); val.setObjectName("stats"); s.vlabel = val
        s.valueChanged.connect(lambda x: val.setText(f"{x}{suffix}")); s.valueChanged.connect(lambda x: None if self.loading else cb(x))
        if final: s.sliderReleased.connect(lambda: final(s.value()))
        lab = QLabel(text); lab.setWordWrap(True); h = QHBoxLayout(); h.addWidget(lab, 1); h.addWidget(val); v.addLayout(h); v.addWidget(s); return s

    def btn(self, text, cb, primary=False):
        b = QPushButton(text); b.setCursor(Qt.CursorShape.PointingHandCursor)
        if primary: b.setObjectName("primary")
        b.clicked.connect(lambda *_: cb()); return b

    # ---- Home: modes, GPU, fans, then where to go ---------------------------------------------------------------------------------------
    def p_home(self):
        lay = self.page("TrueSense", back=False)
        v = self.card(lay, "Performance Mode", "performance", True); self.stat_cpu = v.right
        try: self.modes = hw.supported_modes()
        except Exception: self.modes = list(hw.MODES)
        order = [m for m in ("eco", "quiet", "balanced", "performance", "turbo") if m in self.modes] or self.modes; self.modes = order
        cols = {"eco": "#5de0a0", "quiet": "#5db8ff", "balanced": "#58a6ff", "performance": "#ffb347", "turbo": "#ff5d6c"}
        self.power = Segmented([m.capitalize() for m in order], tiles=True, icons=[m if m in _P else "balanced" for m in order], colors=[cols.get(m, "#58a6ff") for m in order]); v.addWidget(self.power)
        self.power.picked.connect(lambda i: None if self.loading else self.do(lambda: control.power(order[i], 30), lambda r: f"Power mode: {r} (held 30 min)", refresh=False))
        self.auto = Segmented(["Manual", "Native auto", "Smart auto"]); self.auto.picked.connect(self.auto_picked); self.line(v, "Mode control", self.auto)
        self.auto_hint = self.hint(AUTO_HINT["off"]); v.addWidget(self.auto_hint)
        self.smart_box = QWidget(); sb = QVBoxLayout(self.smart_box); sb.setContentsMargins(0, 0, 0, 0); sb.setSpacing(8)
        self.smart_cb = QComboBox(); self.smart_cb.setMinimumWidth(150); self.smart_cb.activated.connect(self.smart_picked)
        self.line(sb, "Smart profile", self.smart_cb, self.btn("Edit", lambda: self.go("smart"))); self.smart_status = self.hint(""); sb.addWidget(self.smart_status); v.addWidget(self.smart_box); self.smart_box.setVisible(False)
        self.hold_box = QWidget(); hb = QHBoxLayout(self.hold_box); hb.setContentsMargins(0, 0, 0, 0); self.hold_lbl = self.hint("")
        hb.addWidget(self.hold_lbl, 1); hb.addWidget(self.btn("Resume auto", lambda: self.do(lambda: ipc.call("hold", minutes=None), "Auto is back in charge", refresh=False))); v.addWidget(self.hold_box); self.hold_box.setVisible(False)
        v = self.card(lay, "GPU Mode", "gpu", True); self.stat_gpu = v.right
        self.gpu = Segmented([m.capitalize() for m in hw.GPU_MODES], tiles=True, icons=["optimus", "gpu", "auto"]); v.addWidget(self.gpu)
        self.gpu.picked.connect(lambda i: None if self.loading else self.do(lambda: hw.set_gpu_mode(list(hw.GPU_MODES)[i]), lambda r: f"GPU mode: {r} (reboot to apply)", refresh=False))
        v = self.card(lay, "Fans", "fan", True); self.stat_fan = v.right
        self.fan = Segmented(FAN_MODES, tiles=True, icons=["smart", "auto", "max", "manual"]); self.fan.picked.connect(self.fan_picked); v.addWidget(self.fan)
        self.curve_box = QWidget(); cb = QVBoxLayout(self.curve_box); cb.setContentsMargins(0, 0, 0, 0)
        self.curve_cb = QComboBox(); self.curve_cb.setMinimumWidth(140); self.curve_cb.activated.connect(self.curve_picked)
        self.line(cb, "Smart curve", self.curve_cb, self.btn("Edit", lambda: self.go("curves"))); v.addWidget(self.curve_box)
        self.fan_hint = self.hint("Smart follows your fan curve. Auto is the firmware's own control, exactly like Auto in PredatorSense on Windows (fans stop when cool)."); v.addWidget(self.fan_hint)
        self.fan_box = QWidget(); fb = QVBoxLayout(self.fan_box); fb.setContentsMargins(0, 0, 0, 0); fb.setSpacing(8)      # only shown while Manual is chosen
        self.fan_pct = self.slider(fb, "Manual speed", 20, 100, 5, lambda x: self.live("fanpct", lambda: control.fan("manual", x)), lambda x: None, suffix=" %")
        self.loading = True; self.fan_pct.setValue(40); self.loading = False; v.addWidget(self.fan_box); self.fan_box.setVisible(False)
        g = QGridLayout(); g.setSpacing(8)
        for n, (label, ic, page) in enumerate((("Lighting", "keyboard", "lighting"), ("Per-key colours", "palette", "keys"), ("Battery and Screen", "battery", "battery"),
                                               ("Audio", "audio", "audio"), ("Settings", "settings", "settings"), ("Quit", "power", None))):
            b_ = self.btn(label, (lambda pg=page: self.go(pg)) if page else self.close); b_.setIcon(make_icon(ic)); b_.setIconSize(QSize(18, 18)); b_.setMinimumHeight(42); g.addWidget(b_, n // 2, n % 2)
        lay.addSpacing(4); lay.addLayout(g); lay.addStretch()

    def auto_picked(self, i):
        if self.loading: return
        self.auto_hint.setText(AUTO_HINT[AUTO_KEYS[i]]); self.smart_box.setVisible(i == 2); self.do(lambda: ipc.call("set", key="auto_mode", value=AUTO_KEYS[i]), lambda r: f"Mode control: {AUTO_KEYS[i]}", refresh=False)

    def fan_picked(self, i):
        if self.loading: return
        self.fan_box.setVisible(i == 3); self.curve_box.setVisible(i == 0); self.fan_apply(i)

    def fan_apply(self, i):
        mode = ("smart", "auto", "max", "manual")[i]; p = self.fan_pct.value()
        self.do(lambda: control.fan(mode, p if mode == "manual" else None), lambda r: {"smart": "Fans: smart", "auto": "Fans: firmware auto", "max": "Fans: MAX", "manual": f"Fans: manual {p} %"}[mode], refresh=False)

    # ---- Lighting: keyboard and rear bar on one page, one at a time ------------------------------------------------------------------------
    def p_lighting(self):
        lay = self.page("Lighting")
        self.light_tabs = Segmented(["Keyboard", "Rear bar", "Profiles"], tiles=True, icons=["keyboard", "bar", "palette"]); lay.addWidget(self.light_tabs); stack = QStackedWidget(); lay.addWidget(stack)
        self.light_tabs.picked.connect(stack.setCurrentIndex); self.light_tabs.set_index(0)
        kb = QWidget(); v = QVBoxLayout(kb); v.setContentsMargins(0, 6, 0, 0); v.setSpacing(12)
        self.kb_on = self.switch(v, "Backlight", lambda x: self.do(lambda: actions.run("keyboard:on" if x else "keyboard:off"), lambda r: r, refresh=False)); self.sep(v)
        self.kb_level = self.slider(v, "Brightness", 0, 50, 5, lambda x: self.live("kbl", lambda: keyboard.set_brightness(x)))
        self.kb_fx = QComboBox(); self.kb_fx.addItems([e.capitalize() for e in KB_EFFECTS]); self.kb_fx.setMinimumWidth(170); self.kb_color = ColorButton("00aec7")
        self.kb_fx.activated.connect(lambda i: self.do(lambda: keyboard.set_effect(KB_EFFECTS[i], self.kb_color.hex, self.kb_speed.value() or 5, max(self.kb_level.value(), 50 if KB_EFFECTS[i] == "neon" else 10)), "Effect applied", refresh=False) or QTimer.singleShot(300, lambda: self.sync_enabled()))
        self.kb_color.changed.connect(lambda h_: self.live("kbc", lambda: keyboard.set_color(h_)))
        self.sep(v); self.line(v, "Lighting mode", self.kb_fx); self.line(v, "Colour", self.kb_color)
        self.kb_speed = self.slider(v, "Effect speed", 1, 11, 1, lambda x: self.live("kbs", lambda: keyboard.set_speed(x))); v.addStretch(); stack.addWidget(kb)
        rr = QWidget(); v = QVBoxLayout(rr); v.setContentsMargins(0, 6, 0, 0); v.setSpacing(12)
        self.rear_on = self.switch(v, "Light", lambda x: self.do(lambda: actions.run("rear:on" if x else "rear:off"), lambda r: r, refresh=False)); self.sep(v)
        self.rear_fx = Segmented([m.capitalize() for m in hw.REAR_MODES]); self.rear_fx.picked.connect(lambda i: self.rear_apply()); self.line(v, "Effect", self.rear_fx)
        self.zc = [ColorButton("00aec7") for _ in range(3)]; self.rear_color = ColorButton("00aec7")
        for b in self.zc + [self.rear_color]: b.changed.connect(lambda h_: self.rear_apply())
        self.sep(v); self.line(v, "Static: left, middle, right", *self.zc); self.line(v, "Colour (other effects)", self.rear_color)
        self.rear_speed = self.slider(v, "Speed", 1, 5, 1, lambda x: self.rear_apply()); self.rear_bright = self.slider(v, "Brightness", 0, 100, 10, lambda x: self.rear_apply(), suffix=" %"); v.addStretch(); stack.addWidget(rr)
        stack.addWidget(self.looks_tab()); lay.addStretch()

    def looks_tab(self):
        """Saved looks (a named keyboard effect/brightness/colours plus rear light) and what is applied at boot."""
        w = QWidget(); v = QVBoxLayout(w); v.setContentsMargins(0, 6, 0, 0); v.setSpacing(12)
        v.addWidget(self.hint("Set the keyboard and rear light the way you like (effect, speed, brightness), then save it as a look."))
        self.look_cb = QComboBox(); self.look_cb.setMinimumWidth(150)
        self.line(v, "Saved looks", self.look_cb, self.btn("Apply", self.look_apply, True), self.btn("Delete", self.look_delete))
        self.look_name = QLineEdit(); self.look_name.setPlaceholderText("name, e.g. wave-dim"); self.look_name.setMinimumWidth(150)
        self.line(v, "Save what is on now", self.look_name, self.btn("Save", self.look_save)); self.sep(v)
        self.boot_cb = QComboBox(); self.boot_cb.setMinimumWidth(150)
        self.line(v, "Use at boot", self.boot_cb, self.btn("Set", self.boot_set), self.btn("Clear", lambda: self.do(lambda: ipc.call("boot_look", action="clear"), "Nothing is applied at boot now", refresh=False)))
        self.boot_lbl = self.hint("…"); v.addWidget(self.boot_lbl)
        v.addWidget(self.hint("Applied by the TrueSense service when the laptop starts (before you log in) and again after every resume from suspend."))
        self.reload_looks(); v.addStretch(); return w

    def reload_looks(self):
        ns = looks.names(); self.look_cb.clear(); self.look_cb.addItems(ns or ["(none saved)"])
        self.boot_cb.clear(); self.boot_cb.addItem("What is on now", "__current__"); [self.boot_cb.addItem(n, n) for n in ns]
        def work():
            try: cur = ipc.call("boot_look", action="get"); txt = ("At boot: " + looks.describe(cur)) if cur else "At boot: nothing is applied"
            except Exception as e: txt = "At boot: needs the updated service (re-run the installer)" if "unknown command" in str(e) else f"At boot: unknown ({e})"
            self.say_boot(txt)
        threading.Thread(target=work, daemon=True).start()

    def look_save(self):
        name = self.look_name.text().strip()
        def go(): looks.save(name); return name
        self.do(go, lambda n: f"Saved look '{n}'", refresh=False); QTimer.singleShot(400, self.reload_looks)

    def look_apply(self):
        n = self.look_cb.currentText()
        self.do(lambda: looks.apply(looks.load(n)), lambda r: f"Look '{n}': " + ", ".join(r), refresh=False)

    def look_delete(self):
        n = self.look_cb.currentText(); self.do(lambda: looks.delete(n), f"Deleted look '{n}'", refresh=False); QTimer.singleShot(400, self.reload_looks)

    def boot_set(self):
        sel = self.boot_cb.currentData()
        def go():
            look = looks.capture() if sel == "__current__" else looks.load(sel); ipc.call("boot_look", action="set", look=look); return looks.describe(look)
        self.do(go, lambda d: f"At boot: {d}", refresh=False); QTimer.singleShot(500, self.reload_looks)

    def sync_enabled(self, look=None):
        """Grey out what the current keyboard look cannot use: wave and neon have no colour; static and per-key looks have no animation speed."""
        look = look if look is not None else keyboard.last(); name = look.get("name") if look.get("mode") == "effect" else None
        self.kb_color.setEnabled(name not in keyboard.NO_COLOR and name != "off"); self.kb_speed.setEnabled(name is not None and name not in ("static", "off"))

    def rear_apply(self):
        """Apply the rear light as shown. Choosing an effect must never switch the light off: brightness 0 means off, so an effect picked while the bar is off turns it on."""
        if self.loading: return
        mode = list(hw.REAR_MODES)[max(0, self.rear_fx.group.checkedId())]; sp = self.rear_speed.value() or 3; br = self.rear_bright.value()
        if br == 0: br = self.rear_last or 100; self.loading = True; self._set(self.rear_bright, br); self.loading = False     # was off: come back at the last brightness
        self.rear_last = br; zones = [b.hex for b in self.zc]; color = self.rear_color.hex
        def go():
            if mode == "static": hw.rear_set("static", speed=sp, brightness=br, zones=zones, verify=False)
            else: hw.rear_set(mode, color, sp, br, verify=False)
        self.live("rear", go)
        self.loading = True; self.rear_on.setChecked(True); self.loading = False

    # ---- Per-key -------------------------------------------------------------------------------------------------------------------------
    def p_keys(self):
        lay = self.page("Per-key colours"); lay.addWidget(self.hint("Click or drag to choose keys (Ctrl adds more), then pick a colour."))
        self.kv = KeyboardView(); self.kv.selection_changed.connect(self.sel_changed); hk = QHBoxLayout(); hk.addWidget(self.kv); hk.addStretch(); lay.addLayout(hk)
        for chunk in (KEY_GROUPS[:4], KEY_GROUPS[4:8], KEY_GROUPS[8:]):
            h = QHBoxLayout(); h.setSpacing(6)
            for g in chunk: h.addWidget(self.btn(g, lambda g=g: self.kv.select_group(g)))
            h.addStretch(); lay.addLayout(h)
        self.pk_color = ColorButton("ff0000"); self.pk_color.changed.connect(lambda h_: self.paint_sel()); self.sel_lbl = QLabel("no keys chosen"); self.sel_lbl.setObjectName("hint")
        v = self.card(lay, "Colour of the chosen keys")
        self.line(v, "Colour", self.pk_color, self.btn("Paint", self.paint_sel, True), self.btn("Black", lambda: (self.pk_color.set_hex("000000"), self.paint_sel()))); v.addWidget(self.sel_lbl)
        v = self.card(lay, "Whole keyboard")
        self.line(v, "Presets", self.btn("Rainbow", lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.rainbow()), None), "Rainbow")),
                  self.btn("Fill", lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.canvas(keymap.hexcolor(self.pk_color.hex))), None), "Filled")),
                  self.btn("Stock", lambda: self.do(lambda: keyboard.restore_default(None), "Stock look")))
        lay.addStretch()

    def sel_changed(self):
        names = sorted({self.kv.labels[c] for c in self.kv.sel if c in self.kv.labels}); self.sel_lbl.setText(f"{len(self.kv.sel)} key(s): " + ", ".join(names[:8]) + ("…" if len(names) > 8 else ""))

    def paint_sel(self):
        if not self.kv.sel or self.loading: return
        hexc = self.pk_color.hex; ch = {c: hexc for c in self.kv.sel}
        for c in self.kv.sel: self.kv.cells[c] = hexc
        self.kv.update(); self.live("paint", lambda: keyboard.paint_cells(ch))

    # ---- Battery and screen ------------------------------------------------------------------------------------------------------------
    def p_battery(self):
        lay = self.page("Battery and Screen"); v = self.card(lay, "Battery", "", True); self.stat_bat = v.right
        self.limit = self.switch(v, "80 % charge limit", lambda x: self.do(lambda: hw.set_battery_limit(x), lambda r: f"80 % limit: {'on' if r else 'off'}", refresh=False)); self.sep(v)
        self.usb = self.seg(v, "USB charging while off", ["Off", "10 %", "20 %", "30 %"], lambda i: self.do(lambda: hw.set_usb_charge(hw.USB_LEVELS[i]), lambda r: f"USB charge stops at {r} %", refresh=False)); self.sep(v)
        self.calib = self.switch(v, "Calibration", lambda x: self.do(lambda: hw.set_calibration(x), lambda r: f"Calibration: {'on' if r else 'off'}", refresh=False), "Needs AC power and takes hours. Switch off to cancel.")
        v = self.card(lay, "Screen and boot")
        self.od = self.switch(v, "LCD overdrive", lambda x: self.do(lambda: hw.set_overdrive(x), lambda r: f"Overdrive: {'on' if r else 'off'}", refresh=False)); self.sep(v)
        self.boot = self.switch(v, "Boot animation and sound", lambda x: self.do(lambda: hw.set_boot_sound(x), lambda r: f"Boot sound: {'on' if r else 'off'}", refresh=False)); self.sep(v)
        self.kb_timeout = self.switch(v, "Keyboard light off after 30 s", lambda x: self.do(lambda: hw.set_kb_timeout(x), "Saved", refresh=False))
        lay.addStretch()

    # ---- Audio -----------------------------------------------------------------------------------------------------------------------------
    def p_audio(self):
        lay = self.page("Audio"); v = self.card(lay, "Output")
        self.out = self.seg(v, "Play sound on", ["Speakers", "Headphones"], lambda i: self.do(lambda: audio.set_output(("speakers", "headphones")[i]), f"Output: {('speakers', 'headphones')[i]}"))
        self.sep(v); self.dts = self.switch(v, "Acer DTS sound", lambda x: self.do(lambda: audio.dts_power(x), "DTS " + ("on" if x else "off")), "This laptop plays on the speakers or the headphone jack, not both at once.")
        v = self.card(lay, "3.5 mm jack")
        self.jack = self.seg(v, "I plugged in", [l for _, l in JACK], self.jack_pick); self.jack_hint = self.hint(""); v.addWidget(self.jack_hint)
        self.jack_state = QLabel("…"); self.jack_state.setObjectName("hint"); v.addWidget(self.jack_state)
        self.boost = self.slider(v, "Headset mic boost", 0, 3, 1, lambda x: None, lambda x: self.do(lambda: audio.headset_boost(x), lambda r: f"Boost: {r}"))
        v = self.card(lay, "Microphone")
        self.mute = self.switch(v, "Mute every microphone", lambda x: self.do(lambda: audio.mic_set_mute(x), "Microphones muted" if x else "Microphones live")); self.sep(v)
        self.v_head = self.seg(v, "Headset (AI noise reduction)", [x.capitalize() for x in VOICE], lambda i: self.do(lambda: (audio.voice_set("headset", VOICE[i]), audio.apply_mic_policy(False))[0], f"Headset mic: {VOICE[i]}"))
        self.v_arr = self.seg(v, "Built-in (AI noise reduction)", [x.capitalize() for x in VOICE], lambda i: self.do(lambda: (audio.voice_set("array", VOICE[i]), audio.apply_mic_policy(False))[0], f"Built-in mics: {VOICE[i]}")); self.sep(v)
        self.v_note = self.switch(v, "Notify when the microphone changes", lambda x: self.do(lambda: config.write_user({"audio": {"notify": x}}), "Saved")); lay.addStretch()

    def jack_pick(self, i):
        val = JACK[i][0]; self.jack_hint.setText(JACK_HINT[val])
        self.do(lambda: (config.write_user({"audio": {"jack": val}}), audio.apply_mic_policy(False))[0], f"3.5 mm jack: {val}")

    # ---- Settings --------------------------------------------------------------------------------------------------------------------------
    def p_settings(self):
        lay = self.page("Settings"); v = self.card(lay, "PREDATOR key")
        v.addWidget(self.hint("What each press does: power-next, power:turbo, profile:gaming, output-toggle, mic-toggle, exec:firefox ..."))
        self.key_edits = {}
        for g, label in GESTURES:
            e = QLineEdit(self.cfg["keys"][g]); e.setMinimumWidth(170); self.line(v, label, e, self.btn("Save", lambda g=g, e=e: self.save_key(g, e)), self.btn("Test", lambda e=e: self.do(lambda: actions.run(e.text()) or "ok", lambda r: str(r))))
            self.key_edits[g] = e
        self.line(v, "Apply saved changes", self.btn("Restart keys", lambda: self.do(lambda: __import__("subprocess").run(["systemctl", "--user", "restart", "truesense-keys"], check=False), "Key service restarted")))
        v = self.card(lay, "Start up")
        self.autostart = self.switch(v, "Open this window at login", self.set_autostart, "Writes ~/.config/autostart/truesense.desktop. Hyprland needs `exec-once = truesense gui` (or uwsm) to run autostart entries.")
        self.autostart.setChecked(os.path.exists(AUTOSTART)); self.sep(v)
        v = self.card(lay, "At login"); v.addWidget(self.hint("Lighting is saved on every change. Choose what comes back. For a named look at boot, see Lighting, Profiles."))
        s = self.cfg["startup"]; keys = [k for k, _ in START]
        for key, title in (("keyboard", "Keyboard"), ("rear", "Rear light")):
            sg = Segmented([l for _, l in START]); sg.set_index(keys.index(s[key]) if s[key] in keys else 0)
            sg.picked.connect(lambda i, key=key: self.do(lambda: config.write_user({"startup": {key: keys[i]}}), f"{key} at login: {keys[i]}")); self.line(v, title, sg)
        from . import startup
        self.line(v, "Try it now", self.btn("Apply", lambda: self.do(lambda: startup.apply(), lambda r: "; ".join(r))))
        v = self.card(lay, "CPU power limits")
        self.pl1, self.pl2 = QDoubleSpinBox(), QDoubleSpinBox()
        for s_, val in ((self.pl1, 65), (self.pl2, 80)): s_.setRange(25, 120); s_.setDecimals(0); s_.setSuffix(" W"); s_.setValue(val)
        self.line(v, "PL1 / PL2 (25 to 120 W)", self.pl1, self.pl2, self.btn("Apply", lambda: self.do(lambda: ipc.call("cpu_limits", pl1=self.pl1.value(), pl2=self.pl2.value()), "CPU limits applied", refresh=False)))
        lay.addStretch()

    def set_autostart(self, on):
        def go():
            if on:
                os.makedirs(os.path.dirname(AUTOSTART), exist_ok=True)
                with open(AUTOSTART, "w") as f: f.write("[Desktop Entry]\nType=Application\nName=TrueSense\nExec=truesense gui\nX-GNOME-Autostart-enabled=true\n")
            else:
                try: os.remove(AUTOSTART)
                except FileNotFoundError: pass
        self.do(go, "Window opens at login" if on else "Window does not open at login", refresh=False)

    # ---- Fan curves ------------------------------------------------------------------------------------------------------------------
    def p_curves(self):
        lay = self.page("Fan curves"); lay.addWidget(self.hint("Drag a point to move it. Double-click empty space to add one, right-click a point to remove it. 0 % means the firmware runs the fans (they stop when cool)."))
        self.curve_side = Segmented(["CPU fan", "GPU fan"]); lay.addWidget(self.curve_side)
        self.editor = CurveEditor(); hk = QHBoxLayout(); hk.addWidget(self.editor); hk.addStretch(); lay.addLayout(hk)
        self.curve_side.picked.connect(lambda i: (setattr(self.editor, "side", ("cpu", "gpu")[i]), self.editor.update())); self.curve_side.set_index(0)
        v = self.card(lay, "Behaviour")
        self.c_pre = self.slider(v, "Early boost when heating (°C)", 0, 15, 1, lambda x: None, suffix=" °C")
        self.c_hys = self.slider(v, "Wait before slowing down (°C)", 0, 15, 1, lambda x: None, suffix=" °C")
        self.c_rate = self.slider(v, "Slow-down speed (% per second)", 1, 10, 1, lambda x: None, suffix=" %/s")
        v = self.card(lay, "Curves")
        self.curve_list = QComboBox(); self.curve_list.setMinimumWidth(150); self.curve_list.activated.connect(lambda i: self.load_curve_in_editor(self.curve_list.currentText()))
        self.copy_btn = self.btn("Copy to edit", self.curve_copy)
        self.line(v, "Curve", self.curve_list, self.btn("Use", self.curve_use, True)); self.curve_name = QLineEdit(); self.curve_name.setPlaceholderText("name"); self.curve_name.setMinimumWidth(120)
        self.curve_save_btn = self.btn("Save", self.curve_save); self.del_btn = self.btn("Delete", self.curve_delete)
        self.line(v, "Edit", self.copy_btn, self.del_btn); self.line(v, "Save as", self.curve_name, self.curve_save_btn); self.curve_msg = self.hint(""); v.addWidget(self.curve_msg)
        self.reload_curves(); lay.addStretch()

    def reload_curves(self, select=None):
        self.curve_list.clear(); self.curve_list.addItems(curves.names()); self.curve_cb.clear(); self.curve_cb.addItems(curves.names())
        sel = select or "truesense"; i = self.curve_list.findText(sel); self.curve_list.setCurrentIndex(max(0, i)); self.curve_cb.setCurrentIndex(max(0, self.curve_cb.findText(sel)))
        self.load_curve_in_editor(self.curve_list.currentText())

    def load_curve_in_editor(self, name):
        try: c = curves.load(name)
        except Exception as e: self.say(f"Error: {e}"); return
        locked = name in curves.BUILTIN; self.editor.locked = locked; self.editor.set_curve(c); self.loading = True
        for s_ in (self.c_pre, self.c_hys, self.c_rate): s_.setEnabled(not locked)
        self.copy_btn.setVisible(locked); self.del_btn.setEnabled(not locked); self.curve_save_btn.setEnabled(not locked)
        self.curve_msg.setText("Built-in curves can't be edited. Press Copy to edit to make your own version." if locked else "")
        for s_, k in ((self.c_pre, "predict"), (self.c_hys, "hysteresis"), (self.c_rate, "down_rate")): s_.setValue(int(round(c[k])))
        self.loading = False

    def editor_curve(self):
        c = {k: v_ for k, v_ in self.editor.curve.items() if k != "description"}
        c.update(predict=self.c_pre.value(), hysteresis=self.c_hys.value(), down_rate=self.c_rate.value()); return curves.validate(c)

    def curve_use(self):
        try: c = self.editor_curve()
        except Exception as e: self.curve_msg.setText(str(e)); self.say(f"Error: {e}"); return
        name = self.curve_list.currentText(); builtin_same = name in curves.BUILTIN and c == curves.load(name)
        self.curve_msg.setText(""); self.do(lambda: ipc.call("fan_curve", action="use", name=name if builtin_same else (name if name not in curves.BUILTIN else "custom"), data=None if builtin_same else c), lambda r: f"Fan curve in use: {r['name']}", refresh=False)

    def curve_save(self):
        name = self.curve_name.text().strip()
        try: c = self.editor_curve(); curves.save(name, c)
        except Exception as e: self.curve_msg.setText(str(e)); self.say(f"Error: {e}"); return
        self.curve_msg.setText(f"Saved '{name}'. Press Use this curve to run it."); self.reload_curves(name); self.say(f"Saved fan curve '{name}'")

    def curve_copy(self):
        """A built-in curve is read-only; copying it makes an editable one under a free name."""
        src = self.curve_list.currentText(); base = (self.curve_name.text().strip() or src + "-copy"); name, n = base, 2
        while name in curves.names(): name = f"{base}-{n}"; n += 1
        try: curves.save(name, curves.load(src))
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_curves(name); self.curve_msg.setText(f"Copied to '{name}'. Drag the points to change it, then Save and Use this curve."); self.say(f"Copied '{src}' to '{name}'")

    def curve_delete(self):
        name = self.curve_list.currentText()
        try: curves.delete(name)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_curves(); self.say(f"Deleted fan curve '{name}'")

    def curve_picked(self, i):
        name = self.curve_cb.currentText()
        def go():
            c = curves.load(name); return ipc.call("fan_curve", action="use", name=name, data=None if name in curves.BUILTIN else c)
        self.do(go, lambda r: f"Fan curve in use: {r['name']}", refresh=False)

    # ---- Smart auto profiles ---------------------------------------------------------------------------------------------------------
    def p_smart(self):
        lay = self.page("Smart auto profiles"); lay.addWidget(self.hint("How Smart auto chooses the power mode. Built-in profiles can't be edited: copy one, then change the copy."))
        v = self.card(lay, "Which mode when"); self.sm_modes = {}
        for k, label in smartprofiles.LABELS.items():
            cb = QComboBox(); cb.addItems([m.capitalize() for m in smartprofiles.MODES]); cb.setMinimumWidth(120); self.line(v, label, cb); self.sm_modes[k] = cb
        v = self.card(lay, "Turbo and timing"); self.sm_turbo = self.switch(v, "Allow Turbo on power", lambda x: None); self.sm_nums = {}
        for k, (lo, hi, label) in smartprofiles.NUMS.items(): self.sm_nums[k] = self.slider(v, label, lo, hi, 1, lambda x: None)
        v = self.card(lay, "Profiles"); self.sm_list = QComboBox(); self.sm_list.setMinimumWidth(150); self.sm_list.activated.connect(lambda i: self.load_smart_in_editor(self.sm_list.currentText()))
        self.sm_copy = self.btn("Copy to edit", self.smart_copy); self.line(v, "Profile", self.sm_list, self.btn("Use", self.smart_use, True))
        self.sm_name = QLineEdit(); self.sm_name.setPlaceholderText("name"); self.sm_name.setMinimumWidth(120); self.sm_save = self.btn("Save", self.smart_save); self.sm_del = self.btn("Delete", self.smart_delete)
        self.line(v, "Edit", self.sm_copy, self.sm_del); self.line(v, "Save as", self.sm_name, self.sm_save); self.sm_msg = self.hint(""); v.addWidget(self.sm_msg); self.reload_smart(); lay.addStretch()

    def reload_smart(self, select=None):
        ns = smartprofiles.names(); self.sm_list.clear(); self.sm_list.addItems(ns); self.smart_cb.clear(); self.smart_cb.addItems(ns)
        sel = select or "truesense"; self.sm_list.setCurrentIndex(max(0, self.sm_list.findText(sel))); self.smart_cb.setCurrentIndex(max(0, self.smart_cb.findText(sel))); self.load_smart_in_editor(self.sm_list.currentText())

    def load_smart_in_editor(self, name):
        try: p = smartprofiles.load(name)
        except Exception as e: self.say(f"Error: {e}"); return
        locked = name in smartprofiles.BUILTIN; self.loading = True
        for k, cb in self.sm_modes.items(): cb.setCurrentIndex(smartprofiles.MODES.index(p[k])); cb.setEnabled(not locked)
        self.sm_turbo.setChecked(p["ac_turbo"]); self.sm_turbo.setEnabled(not locked)
        for k, s_ in self.sm_nums.items(): s_.setValue(p[k]); s_.setEnabled(not locked)
        self.loading = False; self.sm_copy.setVisible(locked); self.sm_save.setEnabled(not locked); self.sm_del.setEnabled(not locked)
        self.sm_msg.setText("Built-in profiles can't be edited. Press Copy to edit to make your own version." if locked else (p.get("description") or ""))

    def smart_editor_profile(self):
        p = {k: smartprofiles.MODES[cb.currentIndex()] for k, cb in self.sm_modes.items()}; p["ac_turbo"] = self.sm_turbo.isChecked()
        p.update({k: s_.value() for k, s_ in self.sm_nums.items()}); return smartprofiles.validate(p)

    def smart_use(self):
        name = self.sm_list.currentText()
        try: p = None if name in smartprofiles.BUILTIN else self.smart_editor_profile()
        except Exception as e: self.sm_msg.setText(str(e)); return
        self.do(lambda: ipc.call("smart_profile", action="use", name=name, data=p), lambda r: f"Smart auto profile in use: {r['name']}", refresh=False)

    def smart_save(self):
        name = self.sm_name.text().strip() or self.sm_list.currentText()
        try: smartprofiles.save(name, self.smart_editor_profile())
        except Exception as e: self.sm_msg.setText(str(e)); self.say(f"Error: {e}"); return
        self.reload_smart(name); self.sm_msg.setText(f"Saved '{name}'. Press Use this profile to run it."); self.say(f"Saved smart profile '{name}'")

    def smart_copy(self):
        src = self.sm_list.currentText()
        try: n = smartprofiles.copy(src, self.sm_name.text().strip() or None)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_smart(n); self.sm_msg.setText(f"Copied to '{n}'. Change what you like, then Save and Use this profile."); self.say(f"Copied '{src}' to '{n}'")

    def smart_delete(self):
        name = self.sm_list.currentText()
        try: smartprofiles.delete(name)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_smart(); self.say(f"Deleted smart profile '{name}'")

    def smart_picked(self, i):
        name = self.smart_cb.currentText()
        self.do(lambda: ipc.call("smart_profile", action="use", name=name, data=None if name in smartprofiles.BUILTIN else smartprofiles.load(name)), lambda r: f"Smart auto profile in use: {r['name']}", refresh=False)

    def save_key(self, g, e):
        err = actions.validate(e.text())
        if err: self.statusBar().showMessage(err, 8000); return
        self.do(lambda: config.write_user({"keys": {g: e.text()}}), f"{g}: saved")

    # ---- refresh -----------------------------------------------------------------------------------------------------------------------
    def tick(self):
        """Once a second: temperatures and fans. Every 2 s, when you are not touching anything, re-read the hardware so changes made elsewhere
        (Noctalia, the PREDATOR key, Fn keys, the command line) show up here."""
        if not self.isVisible() or self.isMinimized(): return
        try:
            s = hw.sensors(); self.stat_cpu.setText(f"CPU: {s['cpu_temp']} °C · {s['cpu_fan_rpm']} RPM"); self.stat_gpu.setText(f"GPU: {s['gpu_temp']} °C · {s['gpu_fan_rpm']} RPM")
            self.stat_fan.setText(f"{max(s['cpu_fan_rpm'], s['gpu_fan_rpm'])} RPM"); self.stat_bat.setText(self.src)
            if self.pages.currentIndex() == PAGES["curves"]: self.editor.marks = {"cpu": s["cpu_temp"], "gpu": s["gpu_temp"]}; self.editor.update()
        except HwError: pass
        self.ticks += 1
        idle = time.monotonic() - self.last_action > 2.0 and not any(self.busy.values()) and not self.pending
        dragging = any(s.isSliderDown() for s in (self.kb_level, self.kb_speed, self.rear_bright, self.rear_speed, self.fan_pct, self.boost))
        popup = any(c.view().isVisible() for c in (self.kb_fx,))
        if self.ticks % 2 == 0 and idle and not dragging and not popup and not self.refreshing: self.full_refresh("audio_fast" if self.pages.currentIndex() == PAGES["audio"] else "home")

    def full_refresh(self, scope=None):
        """Read the hardware off the UI thread. The audio page (pactl, amixer, DTS) is only read while it is shown, so the main page stays quick."""
        scope = scope or ("audio" if self.pages.currentIndex() == PAGES["audio"] else "home")
        self.refreshing = True
        def work():
            d = {}
            def get(k, fn):
                try: d[k] = fn()
                except Exception: pass
            if scope in ("audio", "audio_fast"):
                get("out", audio.output_status); get("jack", audio.jack_status); get("mic", audio.mic_state); get("boost", audio.headset_boost); get("voice", audio.voice_modes)
                get("jackset", lambda: config.load()["audio"]["jack"])
                if scope == "audio": get("dts", lambda: "DTS:            on" in audio.dts_status())      # slow (starts a helper): only when the page opens or after a change
            else:
                get("state", lambda: ipc.call("state")); get("mode", hw.get_mode); get("fan", hw.fan_state); get("rear", hw.rear_get); get("kb", keyboard.get_brightness)
                get("kbtimeout", hw.get_kb_timeout); get("limit", hw.get_battery_limit); get("calib", hw.get_calibration); get("usb", hw.get_usb_charge); get("od", hw.get_overdrive)
                get("boot", hw.get_boot_sound); get("gpu", hw.get_gpu_mode); get("look", keyboard.last); get("cells", keyboard.current_cells); get("ac", hw.on_ac)
            self.say_refresh(d); self.refreshing = False
        threading.Thread(target=work, daemon=True).start()

    @staticmethod
    def _set(slider, value):
        if not slider.isSliderDown(): slider.setValue(value)            # a refresh must never move a slider while it is being dragged

    def _apply(self, d):
        self.loading = True
        try:
            if "state" in d:
                s = d["state"]["settings"]; am = s.get("auto_mode") or ("smart" if s["auto_profile"] else "off"); self.auto.set_index(AUTO_KEYS.index(am) if am in AUTO_KEYS else 0); self.auto_hint.setText(AUTO_HINT.get(am, "")); self.smart_box.setVisible(am == "smart")
                lv = d["state"]["live"]; sp_ = lv.get("smart_profile")
                if sp_ and self.smart_cb.currentText() != sp_ and not self.smart_cb.view().isVisible(): self.smart_cb.setCurrentIndex(max(0, self.smart_cb.findText(sp_)))
                self.smart_status.setText(f"Now: {lv.get('mode_reason')}" if lv.get("mode_reason") and am != "off" else "")
                hl = d["state"].get("hold_left_s", 0); self.hold_box.setVisible(bool(hl) and am != "off"); self.hold_lbl.setText(f"You picked a mode: held for {max(1, hl // 60)} more min, then {am} auto takes over.")
                fc = d["state"]["live"].get("fan_curve")
                if fc and self.curve_cb.currentText() != fc and not self.curve_cb.view().isVisible(): self.curve_cb.setCurrentIndex(max(0, self.curve_cb.findText(fc)))
                if d.get("fan", {}).get("mode") == "manual": self._set(self.fan_pct, max(20, d["fan"]["duty_register_pct"]["cpu"]))
                fi = 0 if s["smart_fan"] else {"auto": 1, "max": 2, "manual": 3}.get(d.get("fan", {}).get("mode"), 1); self.fan.set_index(fi); self.fan_box.setVisible(fi == 3); self.curve_box.setVisible(fi == 0)
            if "ac" in d: self.src = "AC" if d["ac"] else "Battery"
            if d.get("mode") in self.modes: self.power.set_index(self.modes.index(d["mode"]))
            if "rear" in d:
                r = d["rear"]; self.rear_on.setChecked(r["brightness"] > 0); self.rear_last = r["brightness"] or self.rear_last; self._set(self.rear_bright, r["brightness"]); self._set(self.rear_speed, r["speed"])
                self.rear_fx.set_index(list(hw.REAR_MODES).index(r["mode"]) if r["mode"] in hw.REAR_MODES else -1); self.rear_color.set_hex(r["color"])
                zs = r.get("zones") or userstate.get("rear_zones")
                for b, z in zip(self.zc, zs if isinstance(zs, list) and len(zs) == 3 else [r["color"]] * 3): b.set_hex(z)
            if "kb" in d: self._set(self.kb_level, d["kb"]); self.kb_on.setChecked(d["kb"] > 0)
            look = d.get("look") or {}
            if look.get("mode") == "effect" and look.get("name") in KB_EFFECTS: self.kb_fx.setCurrentIndex(KB_EFFECTS.index(look["name"])); self.kb_color.set_hex(look.get("color", "00aec7")); self._set(self.kb_speed, look.get("speed", 5))
            self.sync_enabled(look)
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
            if d.get("boost") is not None: self._set(self.boost, d["boost"])
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
