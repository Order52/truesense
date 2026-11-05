# SPDX-License-Identifier: MIT
"""Optional window for TrueSense (Qt 6 via PyQt6). A thin layer over the same functions the CLI uses; nothing else depends on it.

Run: truesense gui        Needs python-pyqt6 (Arch: `pacman -S python-pyqt6`); runs natively on Wayland (Hyprland) and X11.
"""
import os
import shlex
import subprocess
import sys
import threading
import time
from . import actions, audio, backend as hw, config, control, cpu, curves, designs, gamerules, gpuundervolt, ipc, keybinds, keyboard, keymap, keyprofiles, looks, perkeyfx, profiles, smartprofiles, startup, touchpad, undervolt, userstate
from .device import HwError


class _Lazy:
    """A module that is imported the first time one of its names is used. `update` pulls in the whole HTTP stack (urllib, http.client, ssl: about 13 ms) and is only needed when you
    check for updates, so the window no longer pays for it at start-up."""
    def __init__(self, name): self._name, self._mod = name, None

    def __getattr__(self, attr):
        if self._mod is None:
            import importlib
            self._mod = importlib.import_module("." + self._name, __package__)
        return getattr(self._mod, attr)


update = _Lazy("update")

try:
    from PyQt6.QtCore import Qt, QTimer, pyqtSignal, QObject, QRectF, QEvent
    from PyQt6.QtNetwork import QLocalServer, QLocalSocket
    from PyQt6.QtSvg import QSvgRenderer
    from PyQt6.QtCore import QByteArray, QSize
    from PyQt6.QtGui import QColor, QPainter, QFont, QPen, QIcon, QPixmap, QImage, QFontMetrics, QShortcut, QKeySequence
    from PyQt6.QtWidgets import (QApplication, QCheckBox, QColorDialog, QComboBox, QHBoxLayout, QLabel, QLineEdit, QMainWindow,
                                 QAbstractButton, QPushButton, QScrollArea, QSlider, QBoxLayout, QStackedWidget, QVBoxLayout, QWidget, QFrame, QButtonGroup, QSizePolicy, QAbstractSpinBox, QDoubleSpinBox, QGridLayout, QFileDialog, QMessageBox, QInputDialog)
    from PyQt6.QtGui import QDesktopServices
    from PyQt6.QtCore import QUrl
except ImportError as _e:
    Qt = None; _IMPORT_ERROR = _e


# ---- vector icons (24x24 stroke icons, drawn at any size and colour; nothing to ship) -------------------------------------------------------
_P = {
    "eco": "M6 18c0-8 5-13 13-13 0 8-5 13-13 13zM6 18l8-8",
    "quiet": "M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z",
    "balanced": "M12 4v16M6 20h12M5 8h14M5 8l-3 7a3 3 0 0 0 6 0zM19 8l-3 7a3 3 0 0 0 6 0z",
    "performance": "M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18zM12 13.2l3.6-4.4M12 6.6v1.1M6.6 12h1.1M17.4 12h-1.1M8.4 8.4l.8.8M15.6 8.4l-.8.8",
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

_ICONS = {}


def icon_pixmap(name, color="#dfe5ee", size=24):
    """An icon as a pixmap, rendered once per (name, colour, size): tiles repaint often and rendering the SVG each time cost about 6 ms apiece."""
    key = (name, color, size); px = _ICONS.get(key)
    if px is None: px = _ICONS[key] = _render_icon(name, color, size)
    return px


def _render_icon(name, color, size):
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
        super().__init__(); self.label, self.icon_name, self.fill = label, icon, fill; self.setAccessibleName(label)
        self.setCheckable(True); self.setCursor(Qt.CursorShape.PointingHandCursor); self.setMinimumHeight(66); self.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed)

    def minimumSizeHint(self): return QSize(50, 66)
    def sizeHint(self): return QSize(76, 66)

    def paintEvent(self, _):
        p = QPainter(self); p.setRenderHint(QPainter.RenderHint.Antialiasing); r = QRectF(self.rect()).adjusted(1, 1, -1, -1); on = self.isChecked(); hot = self.underMouse()
        if on and self.fill: bg, fg, border = QColor(self.fill), "#06121f", QColor(self.fill)
        elif on: bg, fg, border = QColor("#243247"), ACCENT, QColor(ACCENT)
        else: bg, fg, border = QColor("#272b32" if hot else "#202328"), "#aab3c0", QColor("#1f2733")
        p.setBrush(bg); p.setPen(QPen(border, 1.4)); p.drawRoundedRect(r, 12, 12)
        px = icon_pixmap(self.icon_name, fg, 22); p.drawPixmap(int(r.center().x() - 11), int(r.top() + 10), px)
        f = QFont(self.font()); f.setBold(on); px_ = getattr(self, "px", 12)
        while px_ > 8:                                                   # a long label shrinks to fit a narrow tile
            f.setPixelSize(px_); p.setFont(f)
            if p.fontMetrics().horizontalAdvance(self.label) <= r.width() - 6: break
            px_ -= 1
        f.setPixelSize(px_); p.setFont(f); p.setPen(QColor(fg)); p.drawText(QRectF(r.left(), r.top() + 36, r.width(), r.height() - 38), Qt.AlignmentFlag.AlignCenter, self.label)


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
#card { background: transparent; border: 1px solid #262a31; border-radius: 14px; }
#sep { background: #272a30; max-height: 1px; min-height: 1px; border: none; }
#section { font-size: 11px; font-weight: 700; letter-spacing: 1.5px; color: #7d8590; padding-top: 6px; }
#cardtitle { font-size: 15px; font-weight: 700; color: #e8edf3; }
#tile { background: #1d1f24; border: 1px solid #1b222c; border-radius: 16px; }
#tilevalue { font-size: 26px; font-weight: 700; color: #ffffff; }
#tilelabel { font-size: 11px; letter-spacing: 1px; color: #6c7787; }
#hint { color: #8892a2; font-size: 12px; }
#h1 { font-size: 20px; font-weight: 700; }
#stats { color: #aab3c0; font-size: 13px; }
QPushButton { background: #171d26; border: 1px solid #222b37; border-radius: 10px; padding: 8px 16px; color: #dfe5ee; }
QPushButton:hover { background: #1d2530; border-color: #2c3847; }
QPushButton:pressed { background: #10252b; }
QWidget#segwrap { background: #202328; border: 1px solid #2e3239; border-radius: 10px; }
QPushButton#seg { background: transparent; border: none; border-radius: 8px; padding: 7px 10px; margin: 0; color: #8b929c; }
QPushButton#seg:hover { color: #dfe5ee; }
QPushButton#seg:checked { background: #363c46; color: #ffffff; font-weight: 700; }
QPushButton#tilebtn { background: #202328; border: 1px solid #1f2733; border-radius: 12px; padding: 8px 4px; color: #aab3c0; font-size: 13px; }
QPushButton#tilebtn:hover { background: #272b32; }
QPushButton#tilebtn:checked { background: #243247; color: #58a6ff; border: 1px solid #58a6ff; font-weight: 700; }
QPushButton#quiet { background: transparent; border: 1px solid #2e3239; padding: 5px 12px; }
QFrame#banner { background: #1c2b3f; border: 1px solid #2f5d8f; border-radius: 12px; }
QPushButton#primary { background: #58a6ff; color: #031418; border: none; font-weight: 700; }
QPushButton#primary:hover { background: #7ab8ff; }
QPushButton#primary:pressed { background: #4a90e0; }
QComboBox { background: #1b1d22; border: 1px solid #222b37; border-radius: 10px; padding: 7px 12px; min-height: 20px; }
QComboBox::drop-down { border: none; width: 24px; }
QComboBox:disabled { color: #666c76; }
QSlider::handle:horizontal:disabled { background: #4a505a; border-color: #2e3239; }
QSlider::sub-page:horizontal:disabled { background: #3a404a; }
QComboBox QAbstractItemView { background: #1d1f24; border: 1px solid #222b37; selection-background-color: #243247; outline: 0; }
QLineEdit { background: #1b1d22; border: 1px solid #222b37; border-radius: 10px; padding: 7px 12px; }
QLineEdit:focus { border-color: #58a6ff; }
QPushButton:focus { border: 1px solid #58a6ff; }
QPushButton:disabled { color: #5d646e; background: #17191d; border-color: #20242a; }
QComboBox:focus { border-color: #58a6ff; }
QToolTip { background: #202328; color: #e8edf3; border: 1px solid #2e3239; padding: 6px; }
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
             "smart": "Switches by heat, load and battery: Performance and Turbo when gaming or heavy work (backs off if it overheats), Eco on battery. A mode you pick yourself ends by itself when the situation changes."}
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
PAGES = {"home": 0, "lighting": 1, "battery": 2, "audio": 3, "settings": 4, "curves": 5, "smart": 6, "predator": 7, "undervolt": 8, "profiles": 9}      # "keys" opens Lighting on its Per-key tab


class TabPages(QWidget if Qt else object):
    """Like a QStackedWidget, but only the current page takes part in the layout, so the area is as tall as the tab you are on (Qt's stacked widget always reports the tallest page,
    which left long empty stretches and needless scrolling)."""
    currentChanged = pyqtSignal(int) if Qt else None

    def __init__(self):
        super().__init__(); self.box = QVBoxLayout(self); self.box.setContentsMargins(0, 0, 0, 0); self.box.setSpacing(0); self.pages = []; self.cur = -1

    def addWidget(self, w):
        self.pages.append(w); self.box.addWidget(w); w.setVisible(len(self.pages) == 1)
        if self.cur < 0: self.cur = 0
        return len(self.pages) - 1

    def count(self): return len(self.pages)
    def widget(self, i): return self.pages[i] if 0 <= i < len(self.pages) else None
    def currentIndex(self): return self.cur
    def currentWidget(self): return self.widget(self.cur)

    def setCurrentIndex(self, i):
        if not 0 <= i < len(self.pages) or i == self.cur: return
        self.cur = i
        for k, w in enumerate(self.pages): w.setVisible(k == i)
        self.updateGeometry(); self.currentChanged.emit(i)


class Row(QWidget if Qt else object):
    """A settings row: the name on the left, the controls on the right. When the window is too narrow for both, the controls move under the name (and text boxes and drop-downs stretch), so nothing is ever cut off."""
    NAME_MIN = 120

    def __init__(self, label, widgets, gap=10, name_min=None):
        super().__init__(); self.label = label; self.widgets = list(widgets); self.gap = gap; self.stacked = None
        if name_min is not None: self.NAME_MIN = name_min
        self.box = QBoxLayout(QBoxLayout.Direction.LeftToRight, self); self.box.setContentsMargins(0, 2, 0, 2); self.box.setSpacing(6)
        self.inner = QHBoxLayout(); self.inner.setSpacing(gap); self.inner.setContentsMargins(0, 0, 0, 0)
        self.box.addWidget(label, 1); self.box.addLayout(self.inner); self.pref = {}; self.policy = {}
        for k, w in enumerate(self.widgets):
            self.inner.addWidget(w)
            if not w.accessibleName() and not (isinstance(w, QAbstractButton) and w.text()): w.setAccessibleName(label.text())          # a screen reader announces the row's name for its control
            if k == 0 and isinstance(w, (QComboBox, QLineEdit, QAbstractSpinBox)): label.setBuddy(w)                                      # clicking the name focuses the control
            if isinstance(w, (QComboBox, QLineEdit)): self.pref[w] = w.minimumWidth(); self.policy[w] = w.sizePolicy()
        self.setSizePolicy(QSizePolicy.Policy.Preferred, QSizePolicy.Policy.Fixed)             # a row is exactly as tall as it needs: it must never soak up spare height in a taller tab

    def minimumSizeHint(self): return QSize(60, super().minimumSizeHint().height())

    def shown(self): return [w for w in self.widgets if not w.isHidden()]

    def need(self):
        ws = self.shown(); return self.NAME_MIN + self.box.spacing() + sum(max(w.sizeHint().width(), self.pref.get(w, 0)) for w in ws) + self.gap * max(0, len(ws) - 1)

    def event(self, e):
        r = super().event(e)
        if e.type() == QEvent.Type.LayoutRequest and self.width() > 0: self._restack()                      # a control was shown or hidden
        return r

    def resizeEvent(self, e):
        super().resizeEvent(e); self._restack()

    def _restack(self):
        stack = bool(self.shown()) and self.need() > self.width()
        if stack == self.stacked: return
        self.stacked = stack; self.box.setDirection(QBoxLayout.Direction.TopToBottom if stack else QBoxLayout.Direction.LeftToRight)
        for w, m in self.pref.items():
            w.setMinimumWidth(min(m, 80) if stack else m)
            if stack: w.setSizePolicy(QSizePolicy.Policy.Expanding, w.sizePolicy().verticalPolicy())
            else: w.setSizePolicy(self.policy[w])
        self.updateGeometry()


class FlowGrid(QWidget if Qt else object):
    """Buttons in a grid that uses two columns when there is room and one when there is not."""
    def __init__(self, items, two_at=340):
        super().__init__(); self.items = list(items); self.two_at = two_at; self.cols = None; self.grid = QGridLayout(self); self.grid.setContentsMargins(0, 0, 0, 0); self.grid.setSpacing(8)
        self.setSizePolicy(QSizePolicy.Policy.Preferred, QSizePolicy.Policy.Fixed); self._fill(2 if two_at else 1)

    def minimumSizeHint(self): return QSize(60, super().minimumSizeHint().height())

    def _fill(self, cols):
        self.cols = cols
        for w in self.items: self.grid.removeWidget(w)
        for n, w in enumerate(self.items): self.grid.addWidget(w, n // cols, n % cols)
        for c in range(2): self.grid.setColumnStretch(c, 1 if c < cols else 0)
        self.updateGeometry()

    def resizeEvent(self, e):
        super().resizeEvent(e); cols = 2 if self.width() >= self.two_at else 1
        if cols != self.cols: self._fill(cols)


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

    def resizeEvent(self, e):
        super().resizeEvent(e)
        if self.btns and isinstance(self.btns[0], TileButton):                      # every tile in a group uses the same label size: the one the longest label needs
            w = self.btns[0].width() - 8; f = QFont(self.font()); f.setBold(True); px = 12
            while px > 8:
                f.setPixelSize(px)
                if max(QFontMetrics(f).horizontalAdvance(b.label) for b in self.btns) <= w: break
                px -= 1
            for b in self.btns:
                if getattr(b, "px", None) != px: b.px = px; b.update()

    def set_index(self, i):
        if 0 <= i < len(self.btns): self.btns[i].setChecked(True)
        else:
            self.group.setExclusive(False); [b.setChecked(False) for b in self.btns]; self.group.setExclusive(True)


class WheelGuard(QObject if Qt else object):
    """Scrolling the page with the mouse wheel must never change a setting by accident. Sliders, drop-downs and number boxes ignore the wheel and the page scrolls instead.
    (Drag a slider, click a drop-down, or type, to change a value.)"""
    def eventFilter(self, obj, ev):
        if ev.type() == QEvent.Type.Wheel and isinstance(obj, (QSlider, QComboBox, QAbstractSpinBox)):
            w = obj.parentWidget()
            while w is not None and not isinstance(w, QScrollArea): w = w.parentWidget()
            if w is not None:
                bar = w.verticalScrollBar(); bar.setValue(bar.value() - ev.angleDelta().y() // 2)            # the page scrolls instead
            ev.accept(); return True
        return False


class NavFilter(QObject if Qt else object):
    """The mouse's back button (and Alt+Left) goes back one page, like a browser; the forward button goes forward again."""
    def __init__(self, win):
        super().__init__(win); self.win = win

    def eventFilter(self, obj, ev):
        t = ev.type()
        if t == QEvent.Type.MouseButtonPress and self.win.isActiveWindow():
            if ev.button() == Qt.MouseButton.BackButton: self.win.nav_back(); return True
            if ev.button() == Qt.MouseButton.ForwardButton: self.win.nav_forward(); return True
        elif t == QEvent.Type.MouseButtonRelease and ev.button() in (Qt.MouseButton.BackButton, Qt.MouseButton.ForwardButton): return True      # the press was ours
        return False


class Bus(QObject if Qt else object):
    message = pyqtSignal(str) if Qt else None
    boot_text = pyqtSignal(str) if Qt else None
    uv = pyqtSignal(dict) if Qt else None
    batt = pyqtSignal(dict) if Qt else None
    report = pyqtSignal(str) if Qt else None
    upd = pyqtSignal(dict) if Qt else None
    refreshed = pyqtSignal(dict) if Qt else None
    game = pyqtSignal(dict) if Qt else None
    pm = pyqtSignal(dict) if Qt else None
    sens = pyqtSignal(dict) if Qt else None
    snd = pyqtSignal(dict) if Qt else None


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


# The real PH16-71 keyboard (from the product photo): main block, then four numpad columns with the media keys above and the PREDATOR key beside NumLk.
KB_MAIN = [   # rows top to bottom: (key, width) or (key, width, absolute x); "_" = gap
    [("esc", 1)] + [(f"f{i}", .93) for i in range(1, 13)] + [("prtsc", .93), ("ins", .93), ("del", .93)],
    [(k, 1) for k in "`1234567890"] + [("minus", 1), ("=", 1), ("backspace", 2)],
    [("tab", 1.5)] + [(k, 1) for k in "qwertyuiop"] + [("[", 1), ("]", 1), ("\\", 1.5)],
    [("caps", 1.75)] + [(k, 1) for k in "asdfghjkl"] + [(";", 1), ("'", 1), ("return", 2.25)],
    [("lshift", 2.25)] + [(k, 1) for k in "zxcvbnm"] + [(",", 1), (".", 1), ("/", 1), ("rshift", 1.75), ("up", 1)],
    [("ctrl", 1.25), ("fn", 1.25), ("windows", 1.25), ("lalt", 1.25), ("space", 4.75), ("ralt", 1), ("options", 1), ("rctrl", 1.25), ("left", 1), ("down", 1), ("right", 1)],
]
KB_PAD = [    # numpad: (key, column 0..3, width, height) per row; the media keys sit in the top row
    [("backward", 0, 1, 1), ("playpause", 1, 1, 1), ("forward", 2, 1, 1), ("power", 3, 1, 1)],
    [("predator", 0, 1, 1), ("numlock", 1, 1, 1), ("num/", 2, 1, 1), ("*", 3, 1, 1)],
    [("num7", 0, 1, 1), ("num8", 1, 1, 1), ("num9", 2, 1, 1), ("-", 3, 1, 1)],
    [("num4", 0, 1, 1), ("num5", 1, 1, 1), ("num6", 2, 1, 1), ("+", 3, 1, 1)],
    [("num1", 0, 1, 1), ("num2", 1, 1, 1), ("num3", 2, 1, 1), ("numenter", 3, 1, 2)],
    [("num0", 0, 2, 1), ("num.", 2, 1, 1)],
]
PAD_X = 16.5
KB_LABELS = {"backspace": "⌫", "return": "↵", "lshift": "Shift", "rshift": "Shift", "caps": "Caps", "windows": "Win", "options": "Opt", "lalt": "Alt", "ralt": "Alt", "ctrl": "Ctrl", "rctrl": "Ctrl",
             "space": "", "up": "↑", "down": "↓", "left": "←", "right": "→", "prtsc": "PrtSc", "predator": "P", "power": "⏻", "playpause": "▶", "backward": "⏮", "forward": "⏭", "numlock": "Num",
             "numenter": "↵", "num/": "/", "num.": ".", "minus": "-", "tab": "Tab", "esc": "Esc", "ins": "Ins", "del": "Del", "fn": "Fn", "num0": "0", "num1": "1", "num2": "2", "num3": "3",
             "num4": "4", "num5": "5", "num6": "6", "num7": "7", "num8": "8", "num9": "9"}
KB_GROUPS = [("All keys", "all"), ("WASD", "wasd"), ("Arrows", "arrows"), ("F1 to F12", "fkeys"), ("Number row", "numrow"), ("Letters", "letters"), ("Numpad", "numpad")]
PALETTE = ["ff3b30", "ff9500", "ffcc00", "34c759", "00c7be", "32ade6", "007aff", "5856d6", "af52de", "ff2d55", "ffffff", "c7c7cc", "8e8e93", "3a3a3c", "00aec7", "000000"]


class PerKeyBoard(QWidget if Qt else object):
    """The keyboard as a picture in its real layout. Click a key, Ctrl+click to add or remove, drag a box to select several, right-click a key to pick up its colour."""
    selection_changed = pyqtSignal() if Qt else None
    picked = pyqtSignal(str) if Qt else None
    UNITS_W, UNITS_H = 20.5, 6.4

    def __init__(self):
        super().__init__(); self.cells = ["00aec7"] * 126 + ["000000"] * 2; self.sel = set(); self.keys = []; self.hover = None; self.band = None; self.base = set(); self.press_pos = None
        self.setMouseTracking(True); self.setFocusPolicy(Qt.FocusPolicy.StrongFocus); self.setMinimumHeight(150); self.setAccessibleName("Keyboard picture: click keys to select them"); self._layout()

    def _layout(self):
        seen = set(); ys = [0.0, 1.3, 2.3, 3.3, 4.3, 5.3]

        def add(name, x, y, w, h):
            cs = keymap.NAMES.get(name) or []
            if cs and not seen.intersection(cs): self.keys.append((name, cs, x, y, w, h)); seen.update(cs)
        for r, row in enumerate(KB_MAIN):
            x = 0.0
            for item in row:
                name, w = item[0], item[1]; ax = item[2] if len(item) > 2 else x
                if name != "_": add(name, ax, ys[r], w, 1)
                x = x + w if len(item) == 2 else x
        for r, row in enumerate(KB_PAD):
            for name, col, w, h in row: add(name, PAD_X + col, ys[r], w, h)
        y = 6.4
        extra = [(n, cs) for n, cs in keymap.NAMES.items() if cs and not seen.intersection(cs) and n not in ("enter", "win")]        # anything the picture does not know still gets a key
        for i, (n, cs) in enumerate(extra): self.keys.append((n, cs, i * 1.0, y, 1, 1)); seen.update(cs); self.UNITS_H = max(self.UNITS_H, y + 1.1)

    def unit(self): return self.width() / self.UNITS_W
    def hit_rect(self, k, u): return QRectF(k[2] * u + 1, k[3] * u + 1, k[4] * u - 2, k[5] * u - 2)
    def sizeHint(self): return QSize(430, int(430 / self.UNITS_W * self.UNITS_H))
    def heightForWidth(self, w): return int(w / self.UNITS_W * self.UNITS_H)
    def resizeEvent(self, e): self.setFixedHeight(self.heightForWidth(self.width()))

    def key_at(self, pos):
        u = self.unit()
        for k in self.keys:
            if self.hit_rect(k, u).contains(pos): return k
        return None

    def set_cells(self, cells): self.cells = list(cells); self.update()
    def selected_names(self): return sorted(k[0] for k in self.keys if self.sel.intersection(k[1]))

    def paintEvent(self, _):
        p = QPainter(self); p.setRenderHint(QPainter.RenderHint.Antialiasing); u = self.unit(); f = QFont(); f.setPixelSize(max(7, int(u * .36))); p.setFont(f)
        p.setBrush(QColor("#141519")); p.setPen(Qt.PenStyle.NoPen); p.drawRoundedRect(QRectF(self.rect()).adjusted(0, 0, 0, 0), 10, 10)
        for k in self.keys:
            name, cs, *_ = k; r = self.hit_rect(k, u); col = QColor("#" + self.cells[cs[0]]); on = bool(self.sel.intersection(cs)); hov = self.hover == name
            p.setBrush(col.lighter(118) if hov else col); p.setPen(QPen(QColor("#ffffff") if on else QColor("#0e0f12"), 2.2 if on else 1)); p.drawRoundedRect(r, 4, 4)
            lum = .3 * col.red() + .59 * col.green() + .11 * col.blue(); p.setPen(QColor("#10131a" if lum > 150 else "#f2f5fa"))
            p.drawText(r, Qt.AlignmentFlag.AlignCenter, KB_LABELS.get(name, name.upper() if len(name) == 1 else name[:3].capitalize()))
        if self.band: p.setBrush(QColor(88, 166, 255, 40)); p.setPen(QPen(QColor("#58a6ff"), 1, Qt.PenStyle.DashLine)); p.drawRect(self.band)
        if self.hasFocus(): p.setBrush(Qt.BrushStyle.NoBrush); p.setPen(QPen(QColor(ACCENT), 1)); p.drawRoundedRect(QRectF(self.rect()).adjusted(.5, .5, -.5, -.5), 10, 10)

    def _emit(self): self.update(); self.selection_changed.emit()

    def mousePressEvent(self, e):
        self.setFocus(); pos = e.position(); k = self.key_at(pos); mods = e.modifiers()
        if e.button() == Qt.MouseButton.RightButton:
            if k: self.picked.emit(self.cells[k[1][0]])
            return
        self.press_pos = pos; self.base = set(self.sel) if mods & (Qt.KeyboardModifier.ControlModifier | Qt.KeyboardModifier.ShiftModifier) else set()
        if k:
            if mods & Qt.KeyboardModifier.ControlModifier and self.sel.issuperset(k[1]): self.sel -= set(k[1])
            else: self.sel = self.base | set(k[1])
        elif not self.base: self.sel = set()
        self._emit()

    def mouseMoveEvent(self, e):
        pos = e.position(); k = self.key_at(pos); name = k[0] if k else None
        if name != self.hover: self.hover = name; self.setToolTip(name or ""); self.update()
        if e.buttons() & Qt.MouseButton.LeftButton and self.press_pos is not None and (pos - self.press_pos).manhattanLength() > 6:
            self.band = QRectF(self.press_pos, pos).normalized(); u = self.unit()
            self.sel = self.base | {c for kk in self.keys if self.band.intersects(self.hit_rect(kk, u)) for c in kk[1]}; self._emit()

    def mouseReleaseEvent(self, e): self.band = None; self.press_pos = None; self.update()
    def leaveEvent(self, e): self.hover = None; self.update()

    def event(self, e):
        if e.type() == QEvent.Type.ShortcutOverride and e.key() == Qt.Key.Key_Escape and self.sel: e.accept(); return True        # Esc clears a selection first; the next Esc closes the window
        return super().event(e)

    def keyPressEvent(self, e):
        if e.key() == Qt.Key.Key_A and e.modifiers() & Qt.KeyboardModifier.ControlModifier: self.sel = {c for kk in self.keys for c in kk[1]}; self._emit()
        elif e.key() == Qt.Key.Key_Escape: self.sel = set(); self._emit()
        else: super().keyPressEvent(e)

    def select_group(self, group):
        if group == "all": self.sel = {c for kk in self.keys for c in kk[1]}
        elif group == "none": self.sel = set()
        elif group == "invert": every = {c for kk in self.keys for c in kk[1]}; self.sel = every - self.sel
        else: self.sel = set(keymap.cells_for(group))
        self._emit()

    def x_of(self, cell):
        for kk in self.keys:
            if cell in kk[1]: return kk[2] + kk[4] / 2
        return 0


ACTION_TYPES = [   # (label, kind, base spec / prefix)
    ("Nothing", "fixed", "none"), ("Next power mode", "fixed", "power-next"), ("Previous power mode", "fixed", "power-prev"), ("Set a power mode", "mode", "power"),
    ("Cycle through power modes", "cycle", "power-cycle"), ("Mode control: next (Manual / Native / Smart)", "fixed", "auto-next"), ("Mode control: choose", "auto", "auto"),
    ("Fans: set", "fan", "fan"), ("Fans: toggle Max", "fixed", "fan-max-toggle"), ("Fans: toggle smart curve", "fixed", "smart-fan-toggle"),
    ("Apply a profile", "profile", "profile"), ("Next profile", "fixed", "profile-next"), ("Apply a lighting look", "look", "look"),
    ("Switch speakers / headphones", "fixed", "output-toggle"), ("Cycle audio outputs", "fixed", "output-cycle"), ("Mute the microphones", "fixed", "mic-toggle"), ("Next voice mode", "fixed", "voice-next"),
    ("Toggle the touchpad", "fixed", "touchpad-toggle"), ("Keyboard light on/off", "fixed", "keyboard:toggle"), ("Next keyboard effect", "fixed", "keyboard-effect-next"),
    ("Rear light on/off", "fixed", "rear:toggle"), ("Next rear effect", "fixed", "rear-effect-next"), ("80 % battery limit on/off", "fixed", "battery-limit-toggle"), ("LCD overdrive on/off", "fixed", "overdrive-toggle"),
    ("Run a script or program", "script", "exec"), ("Run a command", "command", "exec"), ("Show a message", "text", "notify"),
]


class ActionEditor(QWidget if Qt else object):
    """One PREDATOR-key gesture: pick WHAT it does, then its detail (a mode, a profile, a script chosen with a file picker, ...). value() is the action spec."""
    changed = pyqtSignal() if Qt else None

    def __init__(self, name=""):
        super().__init__(); v = QVBoxLayout(self); v.setContentsMargins(0, 0, 0, 0); v.setSpacing(6)
        top = QHBoxLayout(); top.setSpacing(8); self.type = QComboBox(); self.type.addItems([a[0] for a in ACTION_TYPES]); self.type.setMinimumWidth(190); top.addWidget(self.type, 1)
        self.test_btn = QPushButton("Test"); top.addWidget(self.test_btn); v.addLayout(top)
        self.arg_row = QWidget(); a = QHBoxLayout(self.arg_row); a.setContentsMargins(0, 0, 0, 0); a.setSpacing(8)
        self.combo = QComboBox(); self.edit = QLineEdit(); self.browse = QPushButton("Browse…"); self.spin = QDoubleSpinBox(); self.spin.setRange(20, 100); self.spin.setDecimals(0); self.spin.setSuffix(" %")
        for w in (self.combo, self.spin, self.edit, self.browse): a.addWidget(w, 1 if w is self.edit else 0)
        v.addWidget(self.arg_row)
        self.type.currentIndexChanged.connect(self._kind_changed); self.combo.currentIndexChanged.connect(lambda *_: self.changed.emit()); self.edit.textEdited.connect(lambda *_: self.changed.emit())
        self.combo.currentIndexChanged.connect(lambda *_: self.spin.setVisible(self.kind() == "fan" and self.combo.currentText() == "manual %"))
        self.spin.valueChanged.connect(lambda *_: self.changed.emit()); self.browse.clicked.connect(self._browse); self._kind_changed()
        for w_, what in ((self.type, "action"), (self.combo, "choice"), (self.edit, "text or program"), (self.spin, "percent"), (self.test_btn, "test")): w_.setAccessibleName(f"{name} {what}".strip())      # named for screen readers

    def kind(self): return ACTION_TYPES[self.type.currentIndex()][1]

    def _kind_changed(self, *_):
        k = self.kind(); self.arg_row.setVisible(k != "fixed"); self.combo.clear()
        items = {"mode": list(hw.MODES), "auto": ["off", "native", "smart"], "fan": ["smart", "auto", "max", "manual %"], "profile": profiles.names(), "look": looks.names()}.get(k)
        if items is not None: self.combo.addItems(items)
        self.combo.setVisible(items is not None); self.spin.setVisible(False)
        self.edit.setVisible(k in ("cycle", "script", "command", "text")); self.browse.setVisible(k == "script")
        self.edit.setPlaceholderText({"cycle": "eco,balanced,turbo", "script": "no file chosen", "command": "e.g. notify-send hello", "text": "message"}.get(k, "")); self.edit.setReadOnly(k == "script")
        if k == "cycle" and not self.edit.text(): self.edit.setText("eco,balanced,turbo")
        self.changed.emit()

    def _browse(self):
        path, _ = QFileDialog.getOpenFileName(self, "Choose a script or program to run", os.path.expanduser("~"), "All files (*)")
        if path: self.edit.setText(path); self.changed.emit()

    @staticmethod
    def script_command(path):
        """How to run a chosen file: directly if it is executable, otherwise through its interpreter, else open it with the desktop's default program."""
        q = shlex.quote(path)
        if os.access(path, os.X_OK) and os.path.isfile(path): return q
        ext = os.path.splitext(path)[1].lower()
        return {"sh": "sh ", "bash": "bash ", "py": "python3 ", "pl": "perl ", "lua": "lua "}.get(ext[1:], "xdg-open ") + q

    def value(self):
        label, kind, base = ACTION_TYPES[self.type.currentIndex()]
        if kind == "fixed": return base
        if kind in ("mode", "auto", "profile", "look"): return f"{base}:{self.combo.currentText()}" if self.combo.currentText() else base + ":"
        if kind == "cycle": return f"power-cycle:{self.edit.text().replace(' ', '')}"
        if kind == "fan": return f"fan:{int(self.spin.value())}" if self.combo.currentText() == "manual %" else f"fan:{self.combo.currentText()}"
        if kind == "script": return "exec:" + self.script_command(self.edit.text()) if self.edit.text() else "exec:"
        if kind == "command": return "exec:" + self.edit.text()
        return f"notify:{self.edit.text()}"

    def set_spec(self, spec):
        name, _, arg = spec.partition(":"); self.blockSignals(True)
        for i, (_label, kind, base) in enumerate(ACTION_TYPES):
            if kind == "fixed" and spec == base: self.type.setCurrentIndex(i); break
            if kind in ("mode", "auto", "profile", "look", "cycle", "fan", "text") and name == base and kind != "fixed":
                if kind == "fan" and not (arg in ("smart", "auto", "max") or arg.isdigit()): continue
                self.type.setCurrentIndex(i); self._kind_changed()
                if kind == "cycle" or kind == "text": self.edit.setText(arg)
                elif kind == "fan":
                    if arg.isdigit(): self.combo.setCurrentText("manual %"); self.spin.setVisible(True); self.spin.setValue(int(arg))
                    else: self.combo.setCurrentText(arg)
                else: self.combo.setCurrentText(arg)
                break
            if name == "exec" and kind in ("script", "command"):
                cmd = arg; parts = shlex.split(cmd) if cmd else []; target = parts[-1] if parts else ""
                is_script = bool(parts) and os.path.isabs(target) and (len(parts) == 1 or parts[0] in ("sh", "bash", "python3", "perl", "lua", "xdg-open")) and kind == "script"
                if (kind == "script") == is_script:
                    self.type.setCurrentIndex(i); self._kind_changed(); self.edit.setText(target if is_script else cmd); break
        self.blockSignals(False)


class CurveEditor(QWidget if Qt else object):
    """Fan curve graph: drag a point to move it, double-click empty space to add one, right-click a point to remove it."""
    changed = pyqtSignal() if Qt else None
    T0, T1 = 30, 100

    def __init__(self):
        super().__init__(); self.setMinimumWidth(280); self.setFixedHeight(250); self.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed); self.setMouseTracking(True); self.curve = curves.load("truesense"); self.side = "cpu"; self.drag = None; self.locked = False; self.marks = {"cpu": None, "gpu": None}

    def sizeHint(self): return QSize(432, 250)
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
    def __init__(self, lazy=False):
        """lazy=True (the real launcher): only Home is built before the window appears; the other pages are built right after the first paint, one per event-loop turn, or at once if you navigate first."""
        super().__init__(); self.setWindowTitle("TrueSense")
        self.apply_size_mode(bool(userstate.get("gui_fixed", True)), save=False)
        self.wheel_guard = WheelGuard(self); QApplication.instance().installEventFilter(self.wheel_guard)
        self.bus = Bus(); self.bus.message.connect(self._msg); self.bus.boot_text.connect(lambda s: self.boot_lbl.setText(s)); self.bus.uv.connect(self._uv_show); self.bus.batt.connect(self._batt_show); self.bus.report.connect(self._show_report); self.bus.upd.connect(self._upd_show); self.bus.refreshed.connect(self._apply); self.bus.game.connect(lambda r: self.game_show(r)); self.bus.pm.connect(lambda r: self.pm_show(r)); self.bus.sens.connect(lambda r: self._sens_show(r)); self.bus.snd.connect(lambda r: self.snd_btn.setEnabled(True)); self.sensing = False; self.closing = False; self.busy = {}; self.pending = {}; self.loading = False; self.last_action = 0.0; self.refreshing = False; self.ticks = 0; self.rear_last = 100
        self.cfg = config.load(); self.src = ""
        self.pages = QStackedWidget(); self.setCentralWidget(self.pages); self.statusBar().showMessage("Ready")
        self.nav = [0]; self.nav_fwd = []; self.navigating = False; self.nav_filter = NavFilter(self); QApplication.instance().installEventFilter(self.nav_filter)
        self._builders = (self.p_home, self.p_lighting, self.p_battery, self.p_audio, self.p_settings, self.p_curves, self.p_smart, self.p_predator, self.p_undervolt, self.p_profiles)
        self.built = False; self.pending_apply = None; self._target = None; self._next = 1
        if lazy:
            self._builders[0]()
            for _ in self._builders[1:]: self.pages.addWidget(QWidget())                       # placeholders keep every page number where PAGES says it is
            QTimer.singleShot(30, self._build_next)                                            # after the first paint
        else:
            for build in self._builders: build()
            self.built = True
        self.pages.currentChanged.connect(lambda i: self.uv_refresh() if i == PAGES["undervolt"] else (self.battery_refresh() if i == PAGES["battery"] else (self.pm_refresh() if i == PAGES["settings"] else None)))
        self.pages.currentChanged.connect(self.nav_changed); QShortcut(QKeySequence("Alt+Left"), self, activated=self.nav_back); QShortcut(QKeySequence("Alt+Right"), self, activated=self.nav_forward)
        QShortcut(QKeySequence("Escape"), self, activated=self.close); QShortcut(QKeySequence("Ctrl+Q"), self, activated=self.close)
        self.pages.currentChanged.connect(lambda i: self.refresh_if_stale(scope="audio" if i == PAGES["audio"] else None))
        if self.cfg["update"]["auto_check"]: QTimer.singleShot(6000, lambda: self.check_updates(False))
        self.timer = QTimer(self); self.timer.timeout.connect(self.tick); self.timer.start(1000); self.full_refresh(); self.tick()

    # ---- plumbing --------------------------------------------------------------------------------------------------------------------
    def closeEvent(self, e):
        """Stop the timer and cut the worker threads off from the widgets: a late result must never land in a window that is going away."""
        self.closing = True; self.timer.stop(); QApplication.instance().removeEventFilter(self.wheel_guard); QApplication.instance().removeEventFilter(self.nav_filter)
        for sig in (self.bus.message, self.bus.refreshed, self.bus.boot_text, self.bus.uv, self.bus.batt, self.bus.report, self.bus.upd):
            try: sig.disconnect()
            except TypeError: pass
        super().closeEvent(e)

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

    def apply_size_mode(self, fixed, save=True):
        """Fixed size by default: the compact 480 pixel window that tiling desktops (Hyprland, sway) keep floating. Turned off, the window resizes freely and every page adapts down to 380 pixels."""
        if save: userstate.set("gui_fixed", bool(fixed))
        h = min(760, QApplication.primaryScreen().availableGeometry().height() - 60)
        if fixed: self.setFixedSize(480, h)
        else: self.setMinimumSize(380, 480); self.setMaximumSize(16777215, 16777215); self.resize(max(self.width(), 480) if self.isVisible() else 480, h)

    PAGE_TITLES = {0: "", 1: "Lighting", 2: "Battery and Screen", 3: "Audio", 4: "Settings", 5: "Fan curves", 6: "Smart auto profiles", 7: "PREDATOR key", 8: "Undervolt", 9: "Profiles"}

    def nav_changed(self, i):
        self.setWindowTitle("TrueSense" + (" - " + self.PAGE_TITLES[i] if self.PAGE_TITLES.get(i) else ""))
        if self.navigating: return
        if not self.nav or self.nav[-1] != i: self.nav.append(i); self.nav = self.nav[-30:]; self.nav_fwd.clear()

    def nav_back(self):
        """Back: first out of a sub-tab (Lighting's Per-key tab), then to the page you came from."""
        if self.pages.currentIndex() == PAGES["lighting"] and self.light_stack.currentIndex() > 0: self.light_tabs.set_index(0); self.light_stack.setCurrentIndex(0); return
        if len(self.nav) < 2: return
        self.nav_fwd.append(self.nav.pop()); self.navigating = True
        try: self.pages.setCurrentIndex(self.nav[-1])
        finally: self.navigating = False

    def nav_forward(self):
        if not self.nav_fwd: return
        i = self.nav_fwd.pop(); self.nav.append(i); self.navigating = True
        try: self.pages.setCurrentIndex(i)
        finally: self.navigating = False

    def go(self, name):
        self.ensure_built()
        if name == "keys": self.pages.setCurrentIndex(PAGES["lighting"]); self.light_tabs.set_index(1); self.light_stack.setCurrentIndex(1); return
        self.pages.setCurrentIndex(PAGES[name])

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
        w = QWidget(); w.setMaximumWidth(860); lay = QVBoxLayout(w); lay.setContentsMargins(16, 18, 16, 20); lay.setSpacing(12)
        sc = QScrollArea(); sc.setAlignment(Qt.AlignmentFlag.AlignHCenter | Qt.AlignmentFlag.AlignTop); sc.setWidgetResizable(True); sc.setFrameShape(QFrame.Shape.NoFrame); sc.setHorizontalScrollBarPolicy(Qt.ScrollBarPolicy.ScrollBarAlwaysOff); sc.setWidget(w); self._place(sc)
        h = QHBoxLayout(); h.setSpacing(10)
        if back:
            b = self.btn(" Back", lambda: self.pages.setCurrentIndex(0)); b.setToolTip("Back to the main page. Alt+Left or the mouse back button go back one step; Esc closes the app."); b.setIcon(make_icon("back")); b.setObjectName("quiet"); h.addWidget(b)
        if not back:
            il = QLabel(); il.setPixmap(logo_pixmap(30)); il.setFixedSize(32, 32); h.addWidget(il)
        t_ = QLabel(heading); t_.setObjectName("brand" if not back else "h1"); h.addWidget(t_); h.addStretch(); lay.addLayout(h); return lay

    def _place(self, sc):
        if self._target is None: self.pages.addWidget(sc); return
        ph = self.pages.widget(self._target); self.pages.insertWidget(self._target, sc); self.pages.removeWidget(ph); ph.deleteLater()

    def _build_next(self):
        """One more page, then back to the event loop (the window stays responsive while the rest is built)."""
        if self.built or self.closing: return
        self._build_one()
        if self._next < len(self._builders): QTimer.singleShot(0, self._build_next)

    def _build_one(self):
        i = self._next; self._next += 1; self._target = i
        try: self._builders[i]()
        finally: self._target = None
        if self._next >= len(self._builders):
            self.built = True
            if self.pending_apply: d, self.pending_apply = self.pending_apply, None; self._apply(d)       # data that arrived while pages were still being built
            self.tick()

    def ensure_built(self):
        """Anything that touches a page that may not exist yet (navigation, the single-instance hand-off) builds the rest first."""
        while not self.built: self._build_one()

    def card(self, lay, title, icon="", right=False):
        """A plain section: icon and title on the left, a live value on the right, rows underneath."""
        c = QFrame(); c.setObjectName("card"); v = QVBoxLayout(c); v.setContentsMargins(14, 12, 14, 12); v.setSpacing(10)
        h = QHBoxLayout(); h.setSpacing(8)
        if icon:
            il = QLabel(); il.setPixmap(icon_pixmap(icon, "#dfe5ee", 20)); il.setFixedSize(22, 22); h.addWidget(il)
        t_ = QLabel(title); t_.setObjectName("cardtitle"); h.addWidget(t_); h.addStretch(); v.right = None
        if right: v.right = QLabel(""); v.right.setObjectName("stats"); v.right.setWordWrap(True); v.right.setMinimumWidth(0); v.right.setSizePolicy(QSizePolicy.Policy.Preferred, QSizePolicy.Policy.Preferred); v.right.setAlignment(Qt.AlignmentFlag.AlignRight | Qt.AlignmentFlag.AlignVCenter); h.addWidget(v.right)
        v.addLayout(h); lay.addWidget(c); return v

    @staticmethod
    def hint(text):
        l = QLabel(text); l.setObjectName("hint"); l.setWordWrap(True); return l

    @staticmethod
    def sep(v):
        f = QFrame(); f.setObjectName("sep"); v.addWidget(f)

    def line(self, v, text, *widgets, sub=None, name_min=None):
        """One settings row: the name on the left, the control on the right (under the name when the window is narrow)."""
        lab = QLabel(text); lab.setWordWrap(True); v.addWidget(Row(lab, widgets, name_min=name_min))
        if sub: v.addWidget(self.hint(sub))
        return lab

    def switch(self, v, text, cb, sub=None):
        c = Switch(); c.toggled.connect(lambda x: None if self.loading else cb(x)); self.line(v, text, c, sub=sub); return c

    def seg(self, v, text, labels, cb):
        s = Segmented(labels); s.picked.connect(lambda i: None if self.loading else cb(i)); self.line(v, text, s); return s

    def slider(self, v, text, lo, hi, step, cb, final=None, suffix="", prefix=""):
        """The name on the left, the current value on the right, a full-width slider underneath."""
        s = QSlider(Qt.Orientation.Horizontal); s.setRange(lo, hi); s.setSingleStep(step); s.setPageStep(step); s.setAccessibleName(text)
        val = QLabel(f"{prefix if s.value() else ''}{s.value()}{suffix}"); val.setObjectName("stats"); s.vlabel = val
        s.valueChanged.connect(lambda x: val.setText(f"{prefix if x else ''}{x}{suffix}")); s.valueChanged.connect(lambda x: None if self.loading else cb(x))
        if final: s.sliderReleased.connect(lambda: final(s.value()))
        lab = QLabel(text); lab.setWordWrap(True); h = QHBoxLayout(); h.addWidget(lab, 1); h.addWidget(val); v.addLayout(h); v.addWidget(s); return s

    def btn(self, text, cb, primary=False):
        b = QPushButton(text); b.setCursor(Qt.CursorShape.PointingHandCursor)
        if primary: b.setObjectName("primary")
        b.clicked.connect(lambda *_: cb()); return b

    # ---- Home: modes, GPU, fans, then where to go ---------------------------------------------------------------------------------------
    def p_home(self):
        lay = self.page("TrueSense", back=False)
        self.banner = QFrame(); self.banner.setObjectName("banner"); bl = QHBoxLayout(self.banner); bl.setContentsMargins(14, 10, 10, 10); self.banner_lbl = QLabel(""); self.banner_lbl.setWordWrap(True); bl.addWidget(self.banner_lbl, 1)
        bl.addWidget(self.btn("Update", self.update_now, True)); bl.addWidget(self.btn("Later", lambda: self.banner.setVisible(False))); lay.addWidget(self.banner); self.banner.setVisible(False)
        v = self.card(lay, "Performance Mode", "performance", True); self.stat_cpu = v.right
        try: self.modes = hw.supported_modes()
        except Exception: self.modes = list(hw.MODES)
        order = [m for m in ("eco", "quiet", "balanced", "performance", "turbo") if m in self.modes] or self.modes; self.modes = order
        cols = {"eco": "#5de0a0", "quiet": "#5db8ff", "balanced": "#58a6ff", "performance": "#ffb347", "turbo": "#ff5d6c"}
        self.power = Segmented([m.capitalize() for m in order], tiles=True, icons=[m if m in _P else "balanced" for m in order], colors=[cols.get(m, "#58a6ff") for m in order]); v.addWidget(self.power)
        self.power.picked.connect(lambda i: None if self.loading else self.do(lambda: control.power(order[i], 30), lambda r: f"Power mode: {r} (held 30 min)", refresh=False))
        TIPS = {"eco": "Lowest power and heat; for battery and light work", "quiet": "Low fan noise; reduced speed", "balanced": "PredatorSense's default: everyday use", "performance": "More power for demanding work and games", "turbo": "Maximum power; loud fans and high heat"}
        for b_, m_ in zip(self.power.btns, order): b_.setToolTip(TIPS.get(m_, ""))
        self.auto = Segmented(["Manual", "Native", "Smart"]); self.auto.picked.connect(self.auto_picked); self.line(v, "Mode control", self.auto)
        self.auto_hint = self.hint(AUTO_HINT["off"]); v.addWidget(self.auto_hint)
        self.smart_box = QWidget(); sb = QVBoxLayout(self.smart_box); sb.setContentsMargins(0, 0, 0, 0); sb.setSpacing(8)
        self.smart_cb = QComboBox(); self.smart_cb.setMinimumWidth(150); self.smart_cb.activated.connect(self.smart_picked)
        self.line(sb, "Smart profile", self.smart_cb, self.btn("Edit", lambda: self.go("smart"))); self.smart_status = self.hint(""); sb.addWidget(self.smart_status); v.addWidget(self.smart_box); self.smart_box.setVisible(False)
        self.hold_box = QWidget(); hb = QHBoxLayout(self.hold_box); hb.setContentsMargins(0, 0, 0, 0); self.hold_lbl = self.hint("")
        hb.addWidget(self.hold_lbl, 1); hb.addWidget(self.btn("Resume auto", lambda: self.do(lambda: ipc.call("hold", minutes=None), "Auto is back in charge", refresh=False))); v.addWidget(self.hold_box); self.hold_box.setVisible(False)
        v = self.card(lay, "GPU Mode", "gpu", True); self.stat_gpu = v.right
        self.gpu = Segmented([m.capitalize() for m in hw.GPU_MODES], tiles=True, icons=["optimus", "gpu", "auto"]); v.addWidget(self.gpu)
        self.gpu.picked.connect(self.gpu_picked)
        v = self.card(lay, "Fans", "fan", True); self.stat_fan = v.right
        self.fan = Segmented(FAN_MODES, tiles=True, icons=["smart", "auto", "max", "manual"])
        for b_, tip in zip(self.fan.btns, ("Follows your fan curve by temperature and load", "The firmware's own control, exactly like PredatorSense on Windows", "Both fans at full speed", "Set the speed yourself")): b_.setToolTip(tip)
        self.fan.picked.connect(self.fan_picked); v.addWidget(self.fan)
        self.curve_box = QWidget(); cb = QVBoxLayout(self.curve_box); cb.setContentsMargins(0, 0, 0, 0)
        self.curve_cb = QComboBox(); self.curve_cb.setMinimumWidth(140); self.curve_cb.activated.connect(self.curve_picked)
        self.line(cb, "Smart curve", self.curve_cb, self.btn("Edit", lambda: self.go("curves"))); v.addWidget(self.curve_box)
        self.fan_hint = self.hint(self.FAN_HINTS[0]); v.addWidget(self.fan_hint)
        self.fan_box = QWidget(); fb = QVBoxLayout(self.fan_box); fb.setContentsMargins(0, 0, 0, 0); fb.setSpacing(8)      # only shown while Manual is chosen
        self.fan_link = self.switch(fb, "Control both fans together", self.fan_link_changed)
        self.fan_off_btn = self.btn("Turn fans off…", self.fan_off_ask); self.fan_off_btn.setToolTip("Asks first; only allowed while cool; TrueSense gives the fans back to the firmware if the laptop gets warm. It then tells you what the fans really do.")
        self.line(fb, "Fans off", self.fan_off_btn)
        self.fan_one = QWidget(); o = QVBoxLayout(self.fan_one); o.setContentsMargins(0, 0, 0, 0); o.setSpacing(8)
        self.fan_pct = self.slider(o, "Fan speed", 20, 100, 5, lambda x: self.fan_manual_live(), suffix=" %"); fb.addWidget(self.fan_one)
        self.fan_two = QWidget(); w2 = QVBoxLayout(self.fan_two); w2.setContentsMargins(0, 0, 0, 0); w2.setSpacing(8)
        self.fan_cpu = self.slider(w2, "CPU fan", 20, 100, 5, lambda x: self.fan_manual_live(), suffix=" %"); self.fan_gpu = self.slider(w2, "GPU fan", 20, 100, 5, lambda x: self.fan_manual_live(), suffix=" %"); fb.addWidget(self.fan_two)
        self.loading = True
        for s_ in (self.fan_pct, self.fan_cpu, self.fan_gpu): s_.setValue(40)
        self.fan_link.setChecked(bool(userstate.get("fan_link", True))); self.loading = False; self.fan_two.setVisible(not self.fan_link.isChecked()); self.fan_one.setVisible(self.fan_link.isChecked())
        v.addWidget(self.fan_box); self.fan_box.setVisible(False)
        btns = []
        for _n, (label, ic, page) in enumerate((("Lighting", "keyboard", "lighting"), ("Fan curves", "fan", "curves"), ("PREDATOR key", "settings", "predator"), ("Undervolt", "performance", "undervolt"),
                                               ("Profiles", "manual", "profiles"), ("Battery and Screen", "battery", "battery"), ("Audio", "audio", "audio"), ("Settings", "settings", "settings"), ("Quit", "power", None))):
            b_ = self.btn(label, (lambda pg=page: self.go(pg)) if page else self.close); b_.setIcon(make_icon(ic)); b_.setIconSize(QSize(18, 18)); b_.setMinimumHeight(42); btns.append(b_)
        lay.addSpacing(4); lay.addWidget(FlowGrid(btns)); lay.addStretch()

    def p_profiles(self):
        lay = self.page("Profiles")
        lay.addWidget(self.hint("A profile is a one-click bundle: it sets the power mode, fans, rear light, battery limit, screen overdrive, boot sound and audio in one go. It applies once (the power mode is then held like when you pick a tile). A profile only changes what it lists below: anything it does not list stays as it is, so switching from one profile to another can leave things from the earlier one (for example battery-saver's 80 % limit) switched on."))
        v = self.card(lay, "Choose")
        self.prof_cb = QComboBox(); self.prof_cb.setMinimumWidth(150); self.prof_cb.activated.connect(lambda i: self.prof_changed())
        self.line(v, "Profile", self.prof_cb, self.btn("Apply", self.prof_apply, True), self.btn("Next", self.prof_next)); self.prof_now = self.hint(""); v.addWidget(self.prof_now)
        v = self.card(lay, "What it changes (and only this)"); self.prof_info = QLabel(""); self.prof_info.setWordWrap(True); self.prof_info.setObjectName("stats"); v.addWidget(self.prof_info)
        self.line(v, "Details", self.btn("Show the file", self.prof_show), self.btn("Delete", self.prof_delete))
        v = self.card(lay, "Make your own")
        v.addWidget(self.hint("Set everything the way you like first, then save: TrueSense records the current power mode, fans, rear light, battery, screen, boot sound and audio. (Keyboard colours are saved under Lighting, Profiles.)"))
        self.prof_name = QLineEdit(); self.prof_name.setPlaceholderText("name, e.g. my-gaming"); self.prof_name.setMinimumWidth(120); self.line(v, "Save the current state as", self.prof_name, self.btn("Save", self.prof_save))
        v = self.card(lay, "How this differs")
        v.addWidget(self.hint("Smart auto profiles decide HOW the power mode follows load and heat. Fan curves are only the fan shape. Lighting profiles and key sets are only lights. Key bindings are only the PREDATOR key. A profile here sets all of it once; it does not keep watching."))
        lay.addStretch(); self.prof_reload()

    def prof_reload(self, select=None):
        cur = select or userstate.get("profile") or (self.prof_cb.currentData() if self.prof_cb.count() else "")
        for cb in (self.prof_cb,):
            cb.blockSignals(True); cb.clear()
            for n in profiles.names(): cb.addItem(f"{n}  ({profiles.source(n)})" if cb is self.prof_cb else n, n)
            if cur: cb.setCurrentIndex(max(0, cb.findData(cur)))
            cb.blockSignals(False)
        self.prof_changed()

    def prof_changed(self):
        n = self.prof_cb.currentData()
        if not n: self.prof_info.setText("No profiles yet."); return
        try: d = profiles.load(n); self.prof_info.setText((d.get("description") or "") + "\n\n" + "\n".join("• " + line for line in profiles.describe(d)))
        except Exception as e: self.prof_info.setText(f"Could not read this profile: {e}")
        cur = userstate.get("profile"); self.prof_now.setText(f"Last applied: {cur}" if cur else "No profile applied yet.")

    def prof_apply(self):
        n = self.prof_cb.currentData()
        if n: self.do(lambda: profiles.apply(n), lambda r: r if isinstance(r, str) and r else f"Profile '{n}' applied"); QTimer.singleShot(1500, self.prof_changed)

    def prof_next(self):
        self.do(lambda: profiles.apply(profiles.next_name()), lambda r: r); QTimer.singleShot(1500, lambda: self.prof_reload())

    def prof_save(self):
        n = self.prof_name.text().strip()
        def go(): profiles.save(n, profiles.capture("saved from the window")); return n
        self.do(go, lambda r: f"Saved profile '{r}' (the current power, fans, rear light, battery, screen, boot sound and audio)", refresh=False); QTimer.singleShot(600, lambda: self.prof_reload(n))

    def prof_show(self):
        n = self.prof_cb.currentData()
        if not n: return
        try: self.say_report(f"Profile {n}\n\n" + config.dumps(profiles.load(n)))
        except Exception as e: self.say(f"Error: {e}")

    def prof_delete(self):
        n = self.prof_cb.currentData()
        if not n: return
        if profiles.source(n) != "yours": self.say("Shipped profiles cannot be deleted; save a copy under your own name instead"); return
        if QMessageBox.question(self, "Delete profile", f"Delete the profile '{n}'?") != QMessageBox.StandardButton.Yes: return
        self.do(lambda: profiles.delete(n), f"Deleted profile '{n}'", refresh=False); QTimer.singleShot(400, self.prof_reload)

    def auto_picked(self, i):
        if self.loading: return
        self.auto_hint.setText(AUTO_HINT[AUTO_KEYS[i]]); self.smart_box.setVisible(i == 2); self.do(lambda: ipc.call("set", key="auto_mode", value=AUTO_KEYS[i]), lambda r: f"Mode control: {AUTO_KEYS[i]}", refresh=False)

    FAN_HINTS = ("Follows your fan curve: quiet when cool, faster as it heats. Edit it under Fan curves.",
                 "The firmware's own control, exactly like Auto in PredatorSense on Windows (the fans stop when cool).",
                 "Both fans at full speed until you pick another mode.",
                 "The sliders go from 20 to 100 %. Turn fans off asks first and only works while cool; the fans return to Auto by themselves if it gets warm. On this laptop the firmware may keep them at a minimum speed, and TrueSense tells you what they really do.")

    def fan_picked(self, i):
        if self.loading: return
        self.fan_box.setVisible(i == 3); self.curve_box.setVisible(i == 0); self.fan_hint.setText(self.FAN_HINTS[i]); self.fan_apply(i)

    def manual_duty(self):
        """(cpu %, gpu %) from whichever sliders are showing."""
        c, g = (self.fan_pct.value(),) * 2 if self.fan_link.isChecked() else (self.fan_cpu.value(), self.fan_gpu.value())
        return (max(20, c), max(20, g))

    def fan_manual_live(self):
        if self.loading: return
        c, g = self.manual_duty(); self.live("fanpct", lambda: control.fan("manual", c, g))

    def gpu_picked(self, i):
        """The GPU mode is a firmware setting that only takes effect after a reboot, so it is confirmed first and can be undone by picking the old one again."""
        if self.loading: return
        mode = list(hw.GPU_MODES)[i]; box = QMessageBox(QMessageBox.Icon.Question, "Change the GPU mode", f"Switch the GPU mode to {mode.capitalize()}?\n\nIt takes effect after the next reboot. Pick your previous mode again before rebooting to cancel.",
                                                          QMessageBox.StandardButton.Apply | QMessageBox.StandardButton.Cancel, self)
        box.setDefaultButton(QMessageBox.StandardButton.Cancel)
        if box.exec() != QMessageBox.StandardButton.Apply: self.full_refresh(); return
        self.do(lambda: hw.set_gpu_mode(mode), lambda r: f"GPU mode: {r} (reboot to apply)", refresh=False)

    def fan_off_ask(self):
        if getattr(self, "fan_asking", False) or time.monotonic() - getattr(self, "fan_asked_at", -9) < 1.5: return          # one question per click
        self.fan_asking = True
        try:
            box = QMessageBox(QMessageBox.Icon.Warning, "Turn the fans off", "Stop both fans?\n\nWith the fans off the laptop can overheat quickly under any load. This is only for quiet, light use.\n\n"
                              "It is only allowed while the CPU is under 60 °C and the GPU under 55 °C. If the CPU reaches 70 °C or the GPU 65 °C, TrueSense gives the fans back to the firmware automatically.\n\n"
                              "Note: on this laptop the firmware may keep the fans at a minimum speed. TrueSense checks the fan speed afterwards and tells you what really happened.",
                              QMessageBox.StandardButton.Apply | QMessageBox.StandardButton.Cancel, self)
            box.setDefaultButton(QMessageBox.StandardButton.Cancel); box.button(QMessageBox.StandardButton.Apply).setText("Turn fans off")
            ok = box.exec() == QMessageBox.StandardButton.Apply
        finally: self.fan_asking = False; self.fan_asked_at = time.monotonic()
        if not ok: return
        def stop():
            control.fan("off", confirm=True); time.sleep(8)                                              # then look at what the fans really do
            s = hw.sensors(); rpm = [s.get("cpu_fan_rpm"), s.get("gpu_fan_rpm")]
            if any(v and v > 500 for v in rpm): return f"The firmware accepted the stop but the fans still turn (CPU {rpm[0]} rpm, GPU {rpm[1]} rpm): it keeps a minimum speed in manual mode, so the fans cannot really be stopped on this laptop. They stay under TrueSense's watch; Auto brings them back to the firmware."
            return "Fans stopped (confirmed by the fan speed). They come back on Auto automatically if the laptop gets warm."
        self.do(stop, lambda r: r)

    def fan_link_changed(self, on):
        """One slider for both fans, or one each. Switching keeps the speeds you have: both sliders start from the same value."""
        userstate.set("fan_link", bool(on)); self.fan_one.setVisible(on); self.fan_two.setVisible(not on); self.loading = True
        if on: self.fan_pct.setValue(max(self.fan_cpu.value(), self.fan_gpu.value()))
        else: self.fan_cpu.setValue(self.fan_pct.value()); self.fan_gpu.setValue(self.fan_pct.value())
        self.loading = False
        if self.fan.group.checkedId() == 3: self.fan_manual_live()

    def fan_apply(self, i):
        mode = ("smart", "auto", "max", "manual")[i]; c, g = self.manual_duty()
        self.do(lambda: control.fan(mode, c if mode == "manual" else None, g if mode == "manual" else None),
                lambda r: {"smart": "Fans: smart", "auto": "Fans: firmware auto", "max": "Fans: MAX", "manual": f"Fans: manual CPU {c} % / GPU {g} %" if c != g else f"Fans: manual {c} %"}[mode], refresh=False)

    # ---- Lighting: keyboard and rear bar on one page, one at a time ------------------------------------------------------------------------
    def p_lighting(self):
        lay = self.page("Lighting")
        self.light_tabs = Segmented(["Keyboard", "Per-key", "Rear bar", "Profiles"], tiles=True, icons=["keyboard", "palette", "bar", "manual"]); lay.addWidget(self.light_tabs); stack = TabPages(); lay.addWidget(stack); self.light_stack = stack
        self.light_tabs.picked.connect(stack.setCurrentIndex); self.light_tabs.set_index(0)
        kb = QWidget(); v = QVBoxLayout(kb); v.setContentsMargins(0, 6, 0, 0); v.setSpacing(12)
        self.kb_on = self.switch(v, "Backlight", lambda x: self.do(lambda: actions.run("keyboard:on" if x else "keyboard:off"), lambda r: r, refresh=False)); self.sep(v)
        self.kb_level = self.slider(v, "Brightness", 0, 50, 5, lambda x: self.live("kbl", lambda: keyboard.set_brightness(x)))
        self.kb_fx = QComboBox(); self.kb_fx.addItems([e.capitalize() for e in KB_EFFECTS]); self.kb_fx.setMinimumWidth(170); self.kb_color = ColorButton("00aec7")
        self.kb_fx.activated.connect(lambda i: self.do(lambda: keyboard.set_effect(KB_EFFECTS[i], self.kb_color.hex, self.kb_speed.value() or 5, max(self.kb_level.value(), 50 if KB_EFFECTS[i] == "neon" else 10)), "Effect applied", refresh=False) or QTimer.singleShot(300, lambda: self.sync_enabled()))
        self.kb_color.changed.connect(lambda h_: self.live("kbc", lambda: keyboard.set_color(h_)))
        self.sep(v); self.line(v, "Lighting mode", self.kb_fx); self.line(v, "Colour", self.kb_color)
        self.kb_speed = self.slider(v, "Effect speed", keyboard.SPEED_MIN, keyboard.SPEED_MAX, 1, lambda x: self.live("kbs", lambda: keyboard.set_speed(x))); self.sep(v)
        self.kb_oper = self.switch(v, "Colour follows the power mode", lambda x: self.do(lambda: ipc.call("set", key="operating_follow", value=bool(x)), "Keyboard colour follows the power mode: " + ("on" if x else "off"), refresh=False), sub="Eco green, Quiet blue, Balanced cyan, Performance orange, Turbo red. Off leaves the colours you picked.")
        self.line(v, "Default", self.btn("Reset to default", lambda: self.do(lambda: keyboard.restore_default(None), "Keyboard back to the stock look"))); v.addStretch(); stack.addWidget(kb); stack.addWidget(self.keys_tab())
        rr = QWidget(); v = QVBoxLayout(rr); v.setContentsMargins(0, 6, 0, 0); v.setSpacing(12)
        self.rear_on = self.switch(v, "Light", lambda x: self.do(lambda: actions.run("rear:on" if x else "rear:off"), lambda r: r, refresh=False)); self.sep(v)
        self.rear_fx = Segmented([m.capitalize() for m in hw.REAR_MODES]); self.rear_fx.picked.connect(lambda i: self.rear_apply()); self.line(v, "Effect", self.rear_fx)
        self.zc = [ColorButton("00aec7") for _ in range(3)]; self.rear_color = ColorButton("00aec7")
        for b in self.zc + [self.rear_color]: b.changed.connect(lambda h_: self.rear_apply())
        self.sep(v); self.line(v, "Static: left, middle, right", *self.zc); self.line(v, "Colour (other effects)", self.rear_color)
        self.rear_speed = self.slider(v, "Speed", 1, 5, 1, lambda x: self.rear_apply()); self.rear_bright = self.slider(v, "Brightness", 0, 100, 10, lambda x: self.rear_apply(), suffix=" %"); self.sep(v)
        self.line(v, "Default", self.btn("Reset to default", lambda: self.do(lambda: hw.rear_set("breathing", "3723c7", 3, 100, 1), "Rear light back to the stock look"))); v.addStretch(); stack.addWidget(rr)
        stack.addWidget(self.looks_tab()); lay.addStretch()

    def looks_tab(self):
        """Lighting profiles: the keyboard look (effect, speed, brightness or per-key colours) and the rear bar, saved under a name. Works like the other profile pages."""
        w = QWidget(); v = QVBoxLayout(w); v.setContentsMargins(0, 6, 0, 0); v.setSpacing(14)
        v.addWidget(self.hint("A lighting profile remembers the keyboard and the rear bar as they are now. Set the lights the way you like (Keyboard, Per-key, Rear bar), then save them here."))
        c = self.card(v, "Profile"); self.look_cb = QComboBox(); self.look_cb.setMinimumWidth(150); self.look_cb.activated.connect(lambda i: self.look_selected())
        self.line(c, "Profile", self.look_cb, self.btn("Apply", self.look_apply, True)); self.look_copy = self.btn("Copy to edit", self.look_copy_do); self.look_del = self.btn("Delete", self.look_delete)
        self.line(c, "Edit", self.look_copy, self.look_del); self.look_name = QLineEdit(); self.look_name.setPlaceholderText("name"); self.look_name.setMinimumWidth(120); self.look_save_btn = self.btn("Save", self.look_save)
        self.line(c, "Save as", self.look_name, self.look_save_btn); self.look_msg = self.hint(""); c.addWidget(self.look_msg); self.line(c, "Default", self.btn("Reset to default", self.look_reset))
        c = self.card(v, "Per-key sets")
        c.addWidget(self.hint("Only the 128 key colours (no effect, no rear bar), used by `truesense keyboard profile` and by the login setting in Settings."))
        self.ks_cb = QComboBox(); self.ks_cb.setMinimumWidth(150); self.line(c, "Key set", self.ks_cb, self.btn("Apply", self.ks_apply, True), self.btn("Delete", self.ks_delete))
        self.ks_nm = QLineEdit(); self.ks_nm.setPlaceholderText("name"); self.ks_nm.setMinimumWidth(120); self.line(c, "Save the keys now as", self.ks_nm, self.btn("Save", self.ks_save)); self.ks_reload()
        c = self.card(v, "At boot")
        c.addWidget(self.hint("Applied by the TrueSense service when the laptop starts (before you log in) and again after every resume from suspend."))
        self.boot_cb = QComboBox(); self.boot_cb.setMinimumWidth(150); self.line(c, "Profile", self.boot_cb, self.btn("Use", self.boot_set, True)); self.line(c, "Boot lighting", self.btn("Don't change at boot", lambda: self.do(lambda: ipc.call("boot_look", action="clear"), "Lighting is left alone at boot", refresh=False) or QTimer.singleShot(500, self.reload_looks)))
        self.boot_lbl = self.hint("Checking what is applied at boot…"); c.addWidget(self.boot_lbl); self.reload_looks(); v.addStretch(); return w

    def ks_reload(self, select=None):
        cur = select or (self.ks_cb.currentText() if self.ks_cb.count() else ""); self.ks_cb.clear(); self.ks_cb.addItems(keyprofiles.names())
        if cur: self.ks_cb.setCurrentIndex(max(0, self.ks_cb.findText(cur)))

    def ks_apply(self):
        n = self.ks_cb.currentText()
        if n: self.do(lambda: keyprofiles.load(n, None), f"Key set '{n}' applied", refresh=False)

    def ks_save(self):
        n = self.ks_nm.text().strip(); cells = [tuple(bytes.fromhex(c)) for c in self.board.cells]
        def go(): keyprofiles.save(n, cells); return n
        self.do(go, lambda r: f"Saved key set '{r}'", refresh=False); QTimer.singleShot(400, lambda: self.ks_reload(n))

    def ks_delete(self):
        n = self.ks_cb.currentText()
        if n and QMessageBox.question(self, "Delete key set", f"Delete the key set '{n}'?") == QMessageBox.StandardButton.Yes: self.do(lambda: keyprofiles.delete(n), f"Deleted key set '{n}'", refresh=False); QTimer.singleShot(400, self.ks_reload)

    def reload_looks(self, select=None):
        ns = looks.names(); sel = select or (self.look_cb.currentText() if self.look_cb.count() else "native"); self.look_cb.clear(); self.look_cb.addItems(ns); self.look_cb.setCurrentIndex(max(0, self.look_cb.findText(sel)))
        self.boot_cb.clear(); self.boot_cb.addItem("What is on now", "__current__"); [self.boot_cb.addItem(n, n) for n in ns]; self.look_selected()
        def work():
            try: cur = ipc.call("boot_look", action="get"); txt = ("At boot: " + looks.describe(cur)) if cur else "At boot: the lighting is left alone"
            except Exception as e: txt = "At boot: needs the updated service (re-run the installer)" if "unknown command" in str(e) else f"At boot: unknown ({e})"
            self.say_boot(txt)
        threading.Thread(target=work, daemon=True).start()

    def look_selected(self):
        name = self.look_cb.currentText(); builtin = name in looks.BUILTIN
        self.look_copy.setVisible(builtin); self.look_del.setEnabled(not builtin); self.look_save_btn.setEnabled(not builtin or bool(self.look_name.text().strip()))
        try: d = looks.describe(looks.load(name))
        except Exception: d = ""
        self.look_msg.setText(f"Built in, read-only: PredatorSense's own lighting. Press Copy to edit to make your own.   {d}" if builtin else d)

    def look_save(self):
        name = self.look_name.text().strip() or self.look_cb.currentText()
        def go(): looks.save(name); return name
        self.do(go, lambda n: f"Saved lighting profile '{n}'", refresh=False); QTimer.singleShot(500, lambda: (self.reload_looks(name), self.look_name.clear()))

    def look_copy_do(self):
        src = self.look_cb.currentText()
        try: n = looks.copy(src, self.look_name.text().strip() or None)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_looks(n); self.look_name.clear(); self.say(f"Copied '{src}' to '{n}'. Change the lights, then Save to update it.")

    def look_apply(self):
        n = self.look_cb.currentText()
        self.do(lambda: looks.apply(looks.load(n)), lambda r: f"Lighting profile '{n}' applied", refresh=True)

    def look_delete(self):
        n = self.look_cb.currentText()
        try: looks.delete(n)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_looks("native"); self.say(f"Deleted lighting profile '{n}'")

    def look_reset(self):
        self.do(lambda: looks.apply(looks.load("native")), "Lighting back to the default (native)", refresh=True); self.reload_looks("native")

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
    def keys_tab(self):
        """Per-key colours: a tab of the Lighting page (the keyboard as a picture, a palette, tools, and software animations)."""
        w = QWidget(); lay = QVBoxLayout(w); lay.setContentsMargins(0, 6, 0, 0); lay.setSpacing(14); self.history, self.redo_stack, self.recent = [], [], []
        lay.addWidget(self.hint("Click a key, drag a box around several, or Ctrl+click to add. Right-click a key to pick up its colour. Changes show on the keyboard as you make them."))
        self.board = PerKeyBoard(); self.board.selection_changed.connect(self.sel_changed); self.board.picked.connect(lambda h: self.set_colour(h, apply=False)); lay.addWidget(self.board)
        self.sel_cb = QComboBox(); self.sel_cb.setAccessibleName("Select keys by group"); self.sel_cb.addItem("Select…", None); [self.sel_cb.addItem(l, g) for l, g in KB_GROUPS]; self.sel_cb.addItem("Nothing", "none"); self.sel_cb.addItem("Invert selection", "invert")
        self.sel_cb.activated.connect(lambda i: (self.board.select_group(self.sel_cb.currentData()), self.sel_cb.setCurrentIndex(0)) if self.sel_cb.currentData() else None)
        self.sel_lbl = QLabel("No keys selected"); self.sel_lbl.setObjectName("hint"); h = QHBoxLayout(); h.addWidget(self.sel_lbl, 1); h.addWidget(self.sel_cb); lay.addLayout(h)
        v = self.card(lay, "Colour")
        self.cur_hex = "ff3b30"; self.cur_btn = ColorButton(self.cur_hex); self.cur_btn.changed.connect(lambda h_: self.set_colour(h_)); self.hex_edit = QLineEdit(self.cur_hex); self.hex_edit.setMaxLength(7); self.hex_edit.setFixedWidth(90)
        self.hex_edit.editingFinished.connect(lambda: self.set_colour(self.hex_edit.text().lstrip("#")))
        self.line(v, "Selected colour", self.cur_btn, self.hex_edit)
        g = QGridLayout(); g.setSpacing(6)
        for i, c in enumerate(PALETTE):
            b_ = QPushButton(); b_.setMinimumSize(20, 26); b_.setFixedHeight(26); b_.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed); b_.setStyleSheet(f"background:#{c}; border:2px solid #2e3239; border-radius:7px;"); b_.setCursor(Qt.CursorShape.PointingHandCursor)
            b_.setAccessibleName(f"Colour #{c}"); b_.setToolTip(f"#{c}"); b_.clicked.connect(lambda _, c=c: self.set_colour(c)); g.addWidget(b_, i // 8, i % 8)
        v.addLayout(g); self.recent_row = QHBoxLayout(); self.recent_row.setSpacing(6); rl = QLabel("Recent"); rl.setObjectName("hint"); self.recent_row.addWidget(rl); self.recent_row.addStretch(); v.addLayout(self.recent_row)
        v = self.card(lay, "Tools")
        self.g1, self.g2 = ColorButton("ff2d55"), ColorButton("5856d6")
        self.line(v, "Gradient across the selection", self.g1, self.g2, self.btn("Apply", self.apply_gradient))
        self.line(v, "Selection", self.btn("Rainbow", self.apply_rainbow), self.btn("Clear (off)", lambda: self.set_colour("000000")))
        self.undo_btn = self.btn("Undo", self.undo); self.redo_btn = self.btn("Redo", self.redo); self.line(v, "History", self.undo_btn, self.redo_btn); self.undo_btn.setEnabled(False); self.redo_btn.setEnabled(False)
        v = self.card(lay, "Animation")
        v.addWidget(self.hint("The keyboard cannot animate single keys by itself, so TrueSense draws it: about 5 frames a second, while the key service runs. Slow effects look best. It animates the selected keys, or every key if none are selected."))
        self.fx_cb = QComboBox(); self.fx_cb.addItem("Off (static colours)", "off"); [self.fx_cb.addItem(label, k) for k, label in perkeyfx.MODES.items()]; self.fx_cb.setMinimumWidth(170)
        self.fx_cb.activated.connect(lambda i: self.fx_push()); self.line(v, "Mode", self.fx_cb); self.fx_speed = self.slider(v, "Speed", 1, 10, 1, lambda x: self.fx_push())
        v = self.card(lay, "Designs and pictures")
        v.addWidget(self.hint("A design is a still picture of colours (yours live in ~/.config/truesense/designs). A picture file is scaled across the keys."))
        self.ds_cb = QComboBox(); self.ds_cb.setMinimumWidth(170); self.reload_designs()
        self.line(v, "Design", self.ds_cb, self.btn("Apply", self.design_apply, True)); self.ds_name = QLineEdit(); self.ds_name.setPlaceholderText("name, e.g. my-design"); self.ds_name.setMinimumWidth(130)
        self.line(v, "Save the keys as a design", self.ds_name, self.btn("Save", self.design_save)); self.line(v, "Picture", self.btn("Choose a picture…", self.image_apply))
        self.img_prev = QLabel(); self.img_prev.setFixedHeight(0); self.img_prev.setAlignment(Qt.AlignmentFlag.AlignCenter); v.addWidget(self.img_prev); self.img_msg = self.hint("No picture used yet. After you choose one, this shows what was sent: your picture squeezed onto the 22 x 6 keys, and the keyboard drawing above changes to match."); v.addWidget(self.img_msg)
        v = self.card(lay, "Whole keyboard")
        self.pk_level = self.slider(v, "Brightness", 0, 50, 5, lambda x: self.live("pkb", lambda: keyboard.set_brightness(x)))
        self.line(v, "Fill and presets", self.btn("Fill all", self.fill_all), self.btn("Rainbow", self.rainbow_all))
        self.line(v, "Reset", self.btn("Reset to stock", self.reset_keys))
        self.pk_name = QLineEdit(); self.pk_name.setPlaceholderText("name, e.g. my-keys"); self.pk_name.setMinimumWidth(130); self.line(v, "Save as a lighting profile (listed under Profiles)", self.pk_name, self.btn("Save", self.save_perkey_look))
        lay.addStretch(); return w

    def sel_changed(self):
        n = len(self.board.sel); names = self.board.selected_names()
        self.sel_lbl.setText("No keys selected" if not n else f"{len(names)} key{'s' if len(names) != 1 else ''}: " + ", ".join(names[:6]) + ("…" if len(names) > 6 else ""))

    def snapshot(self): return list(self.board.cells)

    def commit(self, cells):
        """Show new colours on the picture, remember them for undo, and send them to the keyboard (newest wins while you drag)."""
        self.history.append(self.snapshot()); self.history = self.history[-40:]; self.redo_stack.clear(); self.board.set_cells(cells); self.sync_history(); self.upload()

    def fx_active(self): return self.fx_cb.currentData() != "off"

    def fx_push(self):
        """Hand the animation settings (mode, speed, which keys, the colours underneath) to the key service."""
        mode, speed, keys, base = self.fx_cb.currentData(), self.fx_speed.value(), sorted(self.board.sel) or None, list(self.board.cells)
        self.do(lambda: perkeyfx.set_fx(mode, speed, keys, base), lambda r: "Animation off" if mode == "off" else f"Animation: {perkeyfx.MODES[mode]} (the key service draws it)", refresh=False)

    def upload(self):
        if self.fx_active(): self.fx_push(); return                       # an animation is running: it draws the frames, just give it the new colours
        cells = [tuple(bytes.fromhex(c)) for c in self.board.cells]; self.live("paint", lambda: keyboard.per_key(cells, None))

    def sync_history(self): self.undo_btn.setEnabled(bool(self.history)); self.redo_btn.setEnabled(bool(self.redo_stack))

    def undo(self):
        if not self.history: return
        self.redo_stack.append(self.snapshot()); self.board.set_cells(self.history.pop()); self.sync_history(); self.upload()

    def redo(self):
        if not self.redo_stack: return
        self.history.append(self.snapshot()); self.board.set_cells(self.redo_stack.pop()); self.sync_history(); self.upload()

    def set_colour(self, hexc, apply=True):
        hexc = (hexc or "").strip().lstrip("#").lower()
        if len(hexc) != 6 or any(c not in "0123456789abcdef" for c in hexc): self.hex_edit.setText(self.cur_hex); return
        self.cur_hex = hexc; self.cur_btn.set_hex(hexc); self.hex_edit.setText(hexc)
        if hexc not in PALETTE and hexc not in self.recent: self.recent = ([hexc] + self.recent)[:8]; self.refresh_recent()
        if apply and self.board.sel:
            cells = self.snapshot()
            for c in self.board.sel: cells[c] = hexc
            self.commit(cells)

    def refresh_recent(self):
        while self.recent_row.count() > 2: w = self.recent_row.takeAt(1).widget(); w and w.deleteLater()
        for c in self.recent:
            b_ = QPushButton(); b_.setFixedSize(26, 22); b_.setStyleSheet(f"background:#{c}; border:2px solid #2e3239; border-radius:6px;"); b_.setAccessibleName(f"Recent colour #{c}"); b_.setToolTip(f"#{c}"); b_.clicked.connect(lambda _, c=c: self.set_colour(c)); self.recent_row.insertWidget(self.recent_row.count() - 1, b_)

    def _selected_by_x(self): return sorted(self.board.sel, key=self.board.x_of)

    def apply_gradient(self):
        s = self._selected_by_x()
        if len(s) < 2: self.say("Error: select two or more keys for a gradient"); return
        a_, b_ = keymap.hexcolor(self.g1.hex), keymap.hexcolor(self.g2.hex); cells = self.snapshot(); xs = [self.board.x_of(c) for c in s]; lo, hi = min(xs), max(xs) or 1
        for c, x in zip(s, xs): cells[c] = "%02x%02x%02x" % keymap.lerp(a_, b_, (x - lo) / (hi - lo) if hi > lo else 0)
        self.commit(cells)

    def apply_rainbow(self):
        s = self._selected_by_x()
        if not s: self.say("Error: select some keys first"); return
        cells = self.snapshot(); xs = [self.board.x_of(c) for c in s]; lo, hi = min(xs), max(xs)
        for c, x in zip(s, xs): cells[c] = "%02x%02x%02x" % keymap.hsv(0.85 * ((x - lo) / (hi - lo) if hi > lo else 0))
        self.commit(cells)

    def fill_all(self):
        cells = self.snapshot()
        for k in self.board.keys:
            for c in k[1]: cells[c] = self.cur_hex
        self.commit(cells)

    def rainbow_all(self):
        self.board.sel_backup = set(self.board.sel); self.board.select_group("all"); self.apply_rainbow(); self.board.sel = self.board.sel_backup; self.board.update(); self.sel_changed()

    def reset_keys(self):
        self.commit(["00aec7"] * 126 + ["000000"] * 2); self.do(lambda: keyboard.restore_default(None), "Keyboard back to the stock look", refresh=False)

    def reload_designs(self, select=None):
        try: ds = designs.load_all()
        except Exception: ds = {}
        self.ds_cb.clear()
        for n, d in sorted(ds.items()): self.ds_cb.addItem(f"{n}  ({d['source']})", n)
        if select: self.ds_cb.setCurrentIndex(max(0, self.ds_cb.findData(select)))

    @staticmethod
    def _hexes(cells): return ["%02x%02x%02x" % tuple(c) for c in cells]

    def design_apply(self):
        n = self.ds_cb.currentData()
        try: self.commit(self._hexes(designs.cells_of(designs.get(n)))); self.say(f"Design '{n}' applied")
        except Exception as e: self.say(f"Error: {e}")

    def design_save(self):
        n = self.ds_name.text().strip()
        try: p = designs.save(n, [tuple(bytes.fromhex(c)) for c in self.board.cells]); self.reload_designs(n); self.say(f"Saved design '{n}' ({p})")
        except Exception as e: self.say(f"Error: {e}")

    def image_apply(self):
        path, _ = QFileDialog.getOpenFileName(self, "Choose a picture", os.path.expanduser("~"), "Pictures (*.png *.jpg *.jpeg *.webp *.bmp *.gif)")
        if not path: return
        name = os.path.basename(path)
        try: cells = designs.image_cells(path)
        except Exception as e:
            self.img_prev.setFixedHeight(0); self.img_msg.setText(f"NOT painted: {e}"); self.say(f"Error: {e}"); return
        self.commit(self._hexes(cells))                                      # the keyboard drawing above changes and the keys are sent
        img = QImage(22, 6, QImage.Format.Format_RGB32); img.fill(0)
        for c in range(126): img.setPixel(c // 6, c % 6, (cells[c][0] << 16) | (cells[c][1] << 8) | cells[c][2] | 0xFF000000)
        self.img_prev.setPixmap(QPixmap.fromImage(img.scaled(330, 90, Qt.AspectRatioMode.IgnoreAspectRatio, Qt.TransformationMode.FastTransformation))); self.img_prev.setFixedHeight(96)
        self.img_msg.setText(f"Sent '{name}' to the keyboard: scaled to 22 x 6 keys (preview above, each block is one key). If the keys do not match, check the status bar for an error; the keyboard drawing in this tab shows what the keys were told.")
        self.say(f"Picture '{name}' painted on the keys")

    def save_perkey_look(self):
        name = self.pk_name.text().strip()
        def go(): looks.save(name, {"keyboard": {"mode": "perkey", "brightness": keyboard.get_brightness() or 25, "cells": list(self.board.cells)}, "rear": None}); return name
        self.do(go, lambda n: f"Saved lighting profile '{n}' (Lighting, Profiles)", refresh=False); QTimer.singleShot(500, lambda: self.reload_looks(self.pk_name.text().strip() or None))

    # ---- Battery and screen ------------------------------------------------------------------------------------------------------------
    def p_battery(self):
        lay = self.page("Battery and Screen"); v = self.card(lay, "Battery", "", True); self.stat_bat = v.right
        self.limit = self.switch(v, "80 % charge limit", lambda x: self.do(lambda: hw.set_battery_limit(x), lambda r: f"80 % limit: {'on' if r else 'off'}", refresh=False)); self.sep(v)
        self.sep(v); self.full_btn = self.btn("Charge to 100 % once", self.full_toggle); self.full_lbl = self.hint("")
        self.line(v, "Need a full battery", self.full_btn); v.addWidget(self.full_lbl)
        v.addWidget(self.hint("The firmware offers two levels: 80 % or 100 %. Charge to 100 % once lifts the limit for one charge; the 80 % limit comes back by itself when you unplug, when the battery has been full for 5 minutes, or after 24 hours."))
        self.sep(v); self.usb = self.seg(v, "USB charging while off", ["Off", "10 %", "20 %", "30 %"], lambda i: self.do(lambda: hw.set_usb_charge(hw.USB_LEVELS[i]), lambda r: f"USB charge stops at {r} %", refresh=False)); self.sep(v)
        self.calib = self.switch(v, "Calibration", self.calib_picked, "Needs AC power and takes hours. Switch off to cancel.")
        v = self.card(lay, "Screen and boot")
        self.od = self.switch(v, "LCD overdrive", lambda x: self.do(lambda: hw.set_overdrive(x), lambda r: f"Overdrive: {'on' if r else 'off'}", refresh=False)); self.sep(v)
        self.boot = self.switch(v, "Boot animation and sound", lambda x: self.do(lambda: hw.set_boot_sound(x), lambda r: f"Boot sound: {'on' if r else 'off'}", refresh=False)); self.sep(v)
        self.kb_timeout = self.switch(v, "Keyboard light off after 30 s", lambda x: self.do(lambda: hw.set_kb_timeout(x), "Saved", refresh=False))
        lay.addStretch()

    # ---- Audio -----------------------------------------------------------------------------------------------------------------------------
    def calib_picked(self, on):
        if on:
            box = QMessageBox(QMessageBox.Icon.Question, "Battery calibration", "Start a battery calibration?\n\nIt needs the charger connected and takes several hours; the battery is charged and discharged fully. Switch it off here to cancel.",
                              QMessageBox.StandardButton.Apply | QMessageBox.StandardButton.Cancel, self); box.setDefaultButton(QMessageBox.StandardButton.Cancel)
            if box.exec() != QMessageBox.StandardButton.Apply: self.loading = True; self.calib.setChecked(False); self.loading = False; return
        self.do(lambda: hw.set_calibration(on), lambda r: f"Calibration: {'on' if r else 'off'}", refresh=False)

    def full_toggle(self):
        if self.full_btn.property("active"): self.do(lambda: ipc.call("battery_full", action="cancel"), "80 % limit is back on", refresh=False); QTimer.singleShot(700, self.battery_refresh)
        else: self.do(lambda: ipc.call("battery_full", action="start"), "Charging to 100 % once: the 80 % limit comes back by itself", refresh=False); QTimer.singleShot(700, self.battery_refresh)

    def battery_refresh(self):
        def work():
            try: r = ipc.call("battery_full", action="get")
            except Exception as e: r = {"error": str(e)}
            self.say_batt(r)
        threading.Thread(target=work, daemon=True).start()

    def say_batt(self, r):
        try: self.bus.batt.emit(r)
        except RuntimeError: pass

    def _batt_show(self, r):
        if "error" in r: self.full_btn.setEnabled(False); self.full_lbl.setText("needs the updated service" if "unknown command" in r["error"] else r["error"][:120]); return
        act = bool(r.get("active")); self.full_btn.setProperty("active", act); self.full_btn.setText("Back to the 80 % limit" if act else "Charge to 100 % once")
        self.full_btn.setEnabled(act or (bool(r.get("limit")) and bool(r.get("ac")))); self.full_lbl.setText(f"Battery {r.get('capacity')} %. " + ("Charging to full this time." if act else ("Plug in the charger to use this." if not r.get("ac") else ("The 80 % limit is off already." if not r.get("limit") else ""))))

    def p_audio(self):
        lay = self.page("Audio")
        v = self.card(lay, "Sound output"); self.snd_lbl = self.hint("Checking that the sound card has an output…"); v.addWidget(self.snd_lbl)
        self.snd_btn = self.btn("Fix sound", self.sound_fix); self.line(v, "No sound at all?", self.snd_btn, sub="Restarts the audio session manager (WirePlumber) so it probes the sound card again, and checks that an output exists. Nothing is written to the sound chip.")
        v = self.card(lay, "Output")
        self.out = self.seg(v, "Play sound on", ["Speakers", "Headphones"], lambda i: self.do(lambda: audio.set_output(("speakers", "headphones")[i]), f"Output: {('speakers', 'headphones')[i]}"))
        self.spk = self.switch(v, "Noise filter on the speakers", lambda x: self.do(lambda: audio.speaker_filter(bool(x)), "Speaker noise filter " + ("on" if x else "off")), "Off by default: it also gates music. For calls on the speakers."); self.sep(v)
        self.dts = self.switch(v, "Acer DTS sound", lambda x: self.do(lambda: audio.dts_power(x), "DTS " + ("on" if x else "off")), "This laptop plays on the speakers or the headphone jack, not both at once.")
        v = self.card(lay, "3.5 mm jack")
        self.jack = self.seg(v, "I plugged in", [l for _, l in JACK], self.jack_pick); self.jack_hint = self.hint(""); v.addWidget(self.jack_hint)
        self.jack_state = QLabel("Looking at the 3.5 mm jack…"); self.jack_state.setObjectName("hint"); v.addWidget(self.jack_state)
        self.boost = self.slider(v, "Headset mic boost", 0, 3, 1, lambda x: None, lambda x: self.do(lambda: audio.headset_boost(x), lambda r: f"Boost: {r}"))
        v = self.card(lay, "Microphone")
        self.micvol = self.slider(v, "Microphone volume", 0, 150, 5, lambda x: None, lambda x: self.do(lambda: audio.mic_volume(x), lambda r: f"Microphone volume: {r} %", refresh=False), suffix=" %")
        self.mute = self.switch(v, "Mute every microphone", lambda x: self.do(lambda: audio.mic_set_mute(x), "Microphones muted" if x else "Microphones live")); self.sep(v)
        self.v_head = self.seg(v, "Headset (AI noise reduction)", [x.capitalize() for x in VOICE], lambda i: self.do(lambda: (audio.voice_set("headset", VOICE[i]), audio.apply_mic_policy(False))[0], f"Headset mic: {VOICE[i]}"))
        self.v_arr = self.seg(v, "Built-in (AI noise reduction)", [x.capitalize() for x in VOICE], lambda i: self.do(lambda: (audio.voice_set("array", VOICE[i]), audio.apply_mic_policy(False))[0], f"Built-in mics: {VOICE[i]}")); self.sep(v)
        self.v_note = self.switch(v, "Notify when the microphone changes", lambda x: self.do(lambda: config.write_user({"audio": {"notify": x}}), "Saved")); lay.addStretch()

    def sound_fix(self):
        self.snd_btn.setEnabled(False)
        def work():
            try: r = audio.repair(); msg = ("Sound output repaired: " + "; ".join(r["steps"])) if r["fixed"] and r["changed"] else ("The sound output is there: nothing to repair" if r["fixed"] else "Could not repair the sound output (" + "; ".join(r["steps"]) + "). Check the kernel log for 'sof' errors and try a reboot.")
            except Exception as e: msg = f"Error: {e}"
            self.say(msg)
            try: self.bus.snd.emit({"enable": True})
            except RuntimeError: pass
            self.full_refresh("audio")
        threading.Thread(target=work, daemon=True).start()

    def jack_pick(self, i):
        val = JACK[i][0]; self.jack_hint.setText(JACK_HINT[val])
        self.do(lambda: (config.write_user({"audio": {"jack": val}}), audio.apply_mic_policy(False))[0], f"3.5 mm jack: {val}")

    # ---- Settings --------------------------------------------------------------------------------------------------------------------------
    def p_settings(self):
        lay = self.page("Settings"); v = self.card(lay, "Window")
        self.autostart = self.switch(v, "Open this window at login", self.set_autostart, "Writes ~/.config/autostart/truesense.desktop. Hyprland needs `exec-once = truesense gui` (or uwsm) to run autostart entries.")
        self.autostart.setChecked(os.path.exists(AUTOSTART)); v.addWidget(self.hint("Lighting at login is set under At login below, and at boot in Lighting, Profiles. The PREDATOR key has its own page."))
        self.size_sw = self.switch(v, "Fixed-size window", lambda x: self.apply_size_mode(x), sub="On (default): the compact 480 pixel window that floats on Hyprland and sway. Off: resize the window freely (it tiles on tiling desktops)."); self.size_sw.setChecked(bool(userstate.get("gui_fixed", True)))
        v = self.card(lay, "At login")
        v.addWidget(self.hint("What the lights and a profile do when you log in (the truesense-startup service). Boot lighting, before login, is in Lighting, Profiles."))
        self.su_kb = QComboBox(); self.su_rear = QComboBox(); self.su_prof = QComboBox()
        for cb in (self.su_kb, self.su_rear, self.su_prof): cb.setMinimumWidth(150)
        self.su_kb.activated.connect(lambda i: self.su_pick("keyboard", self.su_kb)); self.su_rear.activated.connect(lambda i: self.su_pick("rear", self.su_rear)); self.su_prof.activated.connect(lambda i: self.su_pick("profile", self.su_prof))
        self.line(v, "Keyboard lighting", self.su_kb); self.line(v, "Rear light bar", self.su_rear); self.line(v, "Profile (applied first)", self.su_prof); self.line(v, "Try it now", self.btn("Apply the login setup", self.su_apply)); self.su_reload()
        v = self.card(lay, "Touchpad")
        self.tp_sw = self.switch(v, "Touchpad on", lambda x: self.do(lambda: ipc.call("touchpad", action="on" if x else "off"), "Touchpad " + ("on" if x else "off"), refresh=False), "The same as Fn+F10.")
        v = self.card(lay, "Power management")
        self.pm_sw = self.switch(v, "TrueSense manages power", lambda x: self.pm_set(x), sub="Stops and disables other power managers that fight it (auto-cpufreq, TLP, tuned, power-profiles-daemon) and puts the CPU back to sane defaults, turbo on. Off puts them back exactly as they were."); self.pm_lbl = self.hint(""); self.pm_lbl.setVisible(False); v.addWidget(self.pm_lbl)
        v = self.card(lay, "Updates")
        from . import __version__
        self.upd_lbl = self.hint("Not checked yet."); self.upd_btn = self.btn("Update…", self.update_now, True); self.upd_btn.setVisible(False); self.notes_btn = self.btn("What's new", self.open_notes); self.notes_btn.setVisible(False)
        self.line(v, f"Installed: version {__version__}", self.btn("Check now", lambda: self.check_updates(True)), self.notes_btn, self.upd_btn); v.addWidget(self.upd_lbl)
        self.auto_upd = self.switch(v, "Check for updates automatically", self.set_auto_update, "Once a day it asks GitHub for the newest release number (nothing else is sent) and shows a notification; nothing is installed without you pressing Update.")
        self.auto_upd.setChecked(bool(self.cfg["update"]["auto_check"]))
        v = self.card(lay, "Diagnostics")
        self.line(v, "Configuration", self.btn("Show…", self.show_config), self.btn("Check", self.check_config)); self.line(v, "Service", self.btn("Status…", self.show_service))
        self.line(v, "System check", self.btn("Run check…", self.run_doctor)); self.line(v, "Support", self.btn("Copy report", self.copy_report))
        self.line(v, "Hardware self-test", self.btn("Run self-test…", self.run_selftest), sub="Sets each firmware feature, reads it back and restores it (fans are skipped). Takes about a minute."); self.line(v, "Service log", self.btn("Show log…", self.show_log))
        v = self.card(lay, "About")
        lg = QLabel(); lg.setPixmap(logo_pixmap(40)); lg.setFixedSize(42, 42); self.line(v, f"TrueSense {__version__}", lg)
        self.line(v, "Credits and licence", self.btn("Credits", lambda: QDesktopServices.openUrl(QUrl("https://github.com/Order52/truesense/blob/main/docs/CREDITS.md"))), self.btn("Source", lambda: QDesktopServices.openUrl(QUrl("https://github.com/Order52/truesense"))))
        v = self.card(lay, "Reset")
        self.line(v, "Everything back to defaults", self.btn("Reset all…", self.reset_all)); v.addWidget(self.hint("Back to how a fresh install behaves, like PredatorSense on Windows: Native auto mode control, firmware fans, power limits left to the firmware, lighting stock, undervolt off, PREDATOR key `default`. Your saved profiles are kept."))
        lay.addStretch()

    # ---- updates -------------------------------------------------------------------------------------------------------------------
    def set_auto_update(self, on):
        self.do(lambda: config.write_user({"update": {"auto_check": on}}), "Automatic update checks: " + ("on" if on else "off"), refresh=False)
        if on: self.check_updates(False)

    def check_updates(self, force):
        self.upd_lbl.setText("Checking…")
        def work():
            try: r = update.check(force=force)
            except Exception as e: r = {"current": "", "latest": None, "available": False, "error": str(e), "info": None}
            try: self.bus.upd.emit(r)
            except RuntimeError: pass
        threading.Thread(target=work, daemon=True).start()

    def _upd_show(self, r):
        if r.get("done"): self._upd_done(r["done"]); return
        self.upd_info = r; av = bool(r.get("available")); info = r.get("info") or {}
        self.upd_btn.setVisible(av); self.notes_btn.setVisible(bool(info.get("url")))
        if av: txt = f"TrueSense {r['latest']} is available (you have {r['current']})."
        elif r.get("latest"): txt = f"You have the newest version ({r['current']})."
        else: txt = "Could not find the newest version."
        if r.get("error"): txt += f"  {r['error']}"
        self.upd_lbl.setText(txt); self.banner.setVisible(av); self.banner_lbl.setText(f"TrueSense {r.get('latest')} is available." if av else "")

    def open_notes(self):
        url = (getattr(self, "upd_info", {}).get("info") or {}).get("url")
        if url: QDesktopServices.openUrl(QUrl(url))

    def update_now(self):
        r = getattr(self, "upd_info", None)
        if not r or not r.get("available"): self.check_updates(True); return
        box = QMessageBox(QMessageBox.Icon.Question, "Update TrueSense", f"Update TrueSense from {r['current']} to {r['latest']}?\n\nThis downloads the release from GitHub and runs its installer with administrator rights: "
                          "you will be asked for your password. The service restarts for a moment (the fans go back to the firmware meanwhile).", QMessageBox.StandardButton.Apply | QMessageBox.StandardButton.Cancel, self)
        box.setDefaultButton(QMessageBox.StandardButton.Cancel); box.button(QMessageBox.StandardButton.Apply).setText("Update")
        if box.exec() != QMessageBox.StandardButton.Apply: return
        self.upd_btn.setEnabled(False)
        self.upd_lbl.setText("Downloading and installing… (enter your password when asked)" if update.polkit_agent_running() else "A terminal window opens: type your password there to finish the update.")
        def work():
            try: res = update.apply(r["info"]["tag"], gui=True); out = {"ok": res["returncode"] == 0, "version": res["version"], "code": res["returncode"], "via": res.get("via")}
            except Exception as e: out = {"ok": False, "error": str(e)}
            try: self.bus.upd.emit({"done": out, **r})
            except RuntimeError: pass
        threading.Thread(target=work, daemon=True).start()

    def _upd_done(self, out):
        self.upd_btn.setEnabled(True)
        if out.get("ok"):
            self.upd_lbl.setText(f"Updated to {out['version']}.")
            box = QMessageBox(QMessageBox.Icon.Information, "Updated", f"TrueSense {out['version']} is installed.\n\nRestart the window to use it.", QMessageBox.StandardButton.Ok | QMessageBox.StandardButton.Cancel, self)
            box.button(QMessageBox.StandardButton.Ok).setText("Restart the window"); box.button(QMessageBox.StandardButton.Cancel).setText("Later")
            if box.exec() == QMessageBox.StandardButton.Ok: subprocess.Popen(["sh", "-c", "sleep 1; truesense gui"], start_new_session=True); self.close()
        elif out.get("via") == "terminal": self.upd_lbl.setText("The terminal window closed before the update finished. Try again, or run this in a terminal: truesense update apply"); self.say("The update did not finish: run `truesense update apply` in a terminal")
        else: self.upd_lbl.setText("The update did not finish: " + (out.get("error") or f"the installer exited with code {out.get('code')} (see /var/log/truesense-install.log)")); self.say("Error: the update did not finish")

    def run_doctor(self):
        def work():
            import io
            from . import doctor
            try: buf = io.StringIO(); doctor.run(False, buf); txt = buf.getvalue()
            except Exception as e: txt = f"The check failed: {e}"
            self.say_report(txt)
        threading.Thread(target=work, daemon=True).start()

    def pm_refresh(self):
        def go():
            try: r = ipc.call("power_managers", action="status")
            except Exception as e: r = {"error": str(e)}
            try: r["tp"] = touchpad.enabled()
            except Exception: pass
            try: self.bus.pm.emit(r)
            except RuntimeError: pass
        threading.Thread(target=go, daemon=True).start()

    def pm_show(self, r):
        if r.get("tp") is not None:
            self.loading = True
            try: self.tp_sw.setChecked(bool(r["tp"]))
            finally: self.loading = False
        if "error" in r: self.pm_lbl.setVisible(True); self.pm_lbl.setText("Power managers: could not ask the service (" + r["error"] + ")"); return
        self.loading = True
        try: self.pm_sw.setChecked(bool(r.get("on")))
        finally: self.loading = False
        found = [m["unit"] + (" (running)" if m["active"] else " (enabled)") for m in r.get("found", [])]
        self.pm_lbl.setVisible(True); self.pm_lbl.setText(("Another power manager is present: " + ", ".join(found) + (". Turn this on to stop it." if not r.get("on") else ". It will be stopped.")) if found else "No other power manager is running or enabled.")
        if r.get("done"): self.say(r["done"])

    def pm_set(self, on):
        def go():
            try:
                r = ipc.call("power_managers", action="takeover" if on else "restore")
                msg = ("Stopped: " + (", ".join(r["stopped"]) or "nothing was running") + (f". Could not stop: {', '.join(r['failed'])}" if r["failed"] else "") if on else "Restored: " + (", ".join(r["restored"]) or "nothing to restore"))
                r2 = ipc.call("power_managers", action="status"); r2["done"] = msg
            except Exception as e: r2 = {"error": str(e)}
            try: self.bus.pm.emit(r2)
            except RuntimeError: pass
        threading.Thread(target=go, daemon=True).start()

    SU_STOCK = {"keyboard": [("last", "Last used"), ("default", "Stock look"), ("none", "Leave as the firmware has it"), ("off", "Dark")],
                "rear": [("last", "Last used"), ("default", "Stock look"), ("none", "Leave as the firmware has it"), ("off", "Dark")]}

    def su_reload(self):
        try: cfg = config.load()["startup"]
        except Exception: return
        self.loading = True
        try:
            for target, cb in (("keyboard", self.su_kb), ("rear", self.su_rear)):
                cb.clear(); [cb.addItem(label, val) for val, label in self.SU_STOCK[target]]; [cb.addItem(f"Look: {n}", f"look:{n}") for n in looks.names()]
                if target == "keyboard": [cb.addItem(f"Key set: {n}", f"profile:{n}") for n in keyprofiles.names()]
                cb.setCurrentIndex(max(0, cb.findData(cfg.get(target, "last"))))
            self.su_prof.clear(); self.su_prof.addItem("None", "none"); [self.su_prof.addItem(n, n) for n in profiles.names()]; self.su_prof.setCurrentIndex(max(0, self.su_prof.findData(cfg.get("profile") or "none")))
        finally: self.loading = False

    def su_pick(self, target, cb):
        if self.loading: return
        val = cb.currentData(); self.do(lambda: startup.set_option(target, val), f"At login, {target}: {cb.currentText()}", refresh=False)

    def su_apply(self): self.do(lambda: startup.apply(), lambda r: "Login setup applied:\n" + "\n".join(r) if isinstance(r, list) else "Login setup applied", refresh=False)

    def show_config(self):
        try: self.say_report(f"Configuration files: {config.SYSTEM_PATH}, {config.USER_PATH}\n\n" + config.dumps(config.load()))
        except Exception as e: self.say(f"Error: {e}")

    def check_config(self):
        try: p = config.validate(config.load()); self.say("Configuration is valid" if not p else "Configuration problem: " + "; ".join(p))
        except Exception as e: self.say(f"Error: {e}")

    def show_service(self):
        def work():
            try: r = subprocess.run(["systemctl", "--no-pager", "status", "truesense.service"], capture_output=True, text=True, timeout=10); txt = (r.stdout or r.stderr)[-3000:]
            except (OSError, subprocess.SubprocessError) as e: txt = f"Could not read the service status: {e}"
            self.say_report(txt)
        threading.Thread(target=work, daemon=True).start()

    def run_selftest(self):
        if QMessageBox.question(self, "Hardware self-test", "This briefly changes power mode, lighting, battery and display settings and restores them. The fans are not touched.\n\nRun it now?") != QMessageBox.StandardButton.Yes: return
        self.say("Self-test running…")
        def work():
            import io
            import contextlib
            from . import selftest
            buf = io.StringIO()
            try:
                with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf): ok = selftest.run(True)
                txt = buf.getvalue() + ("\nALL PASSED" if ok else "\nSOME CHECKS FAILED")
            except Exception as e: txt = buf.getvalue() + f"\nThe self-test failed: {e}"
            self.say_report(txt)
        threading.Thread(target=work, daemon=True).start()

    def show_log(self):
        def work():
            try: r = subprocess.run(["journalctl", "-u", "truesense.service", "-b", "--no-pager", "-n", "60"], capture_output=True, text=True, timeout=10); txt = (r.stdout or r.stderr or "No log lines.")[-3000:]
            except (OSError, subprocess.SubprocessError) as e: txt = f"Could not read the log: {e}"
            self.say_report(txt)
        threading.Thread(target=work, daemon=True).start()

    def say_report(self, txt):
        try: self.bus.report.emit(txt)
        except RuntimeError: pass

    def _show_report(self, txt):
        self.last_report = txt; box = QMessageBox(QMessageBox.Icon.Information, "System check", txt[:3000], QMessageBox.StandardButton.Ok, self); box.exec()

    def copy_report(self):
        QApplication.clipboard().setText(getattr(self, "last_report", "") or "Run the system check first."); self.say("Report copied to the clipboard")

    def reset_all(self):
        if QMessageBox.question(self, "Reset to defaults", "Reset the fan curve, Smart auto profile, PREDATOR key, power limits, lighting and undervolt to their defaults?\n\nYour saved profiles are not deleted.") != QMessageBox.StandardButton.Yes: return
        def go():
            steps = (lambda: ipc.call("fan_curve", action="use", name="native"), lambda: ipc.call("smart_profile", action="use", name="native"), lambda: control.fan("auto"),
                     lambda: ipc.call("set", key="auto_mode", value="native"), lambda: keybinds.use("native"), lambda: ipc.call("cpu_limits", reset=True),
                     lambda: ipc.call("undervolt", action="boot_clear"), lambda: ipc.call("undervolt", action="reset"), lambda: ipc.call("gpu_undervolt", action="boot_clear"), lambda: ipc.call("gpu_undervolt", action="reset"),
                     lambda: keyboard.restore_default(None), lambda: hw.rear_set("breathing", "3723c7", 3, 100, 1))
            for fn in steps:
                try: fn()
                except Exception as e: self.say(f"Error: {e}")
            return "done"
        self.do(go, "Everything is back to its defaults"); QTimer.singleShot(1500, lambda: (self.reload_curves("native"), self.reload_smart("native"), self.reload_keyprofiles("native"), self.reload_uv("native"), self.reload_gpu_uv("native")))

    # ---- PREDATOR key ------------------------------------------------------------------------------------------------------------------
    def p_predator(self):
        lay = self.page("PREDATOR key")
        lay.addWidget(self.hint("What each press of the PREDATOR key does. Pick an action, or run your own script. Built-in profiles can't be edited: copy one, then change the copy."))
        v = self.card(lay, "Profile")
        self.kp_list = QComboBox(); self.kp_list.setMinimumWidth(150); self.kp_list.activated.connect(lambda i: self.load_keyprofile(self.kp_list.currentText()))
        self.line(v, "Bindings", self.kp_list, self.btn("Use", self.kp_use, True)); self.kp_copy = self.btn("Copy to edit", self.kp_copy_do); self.kp_del = self.btn("Delete", self.kp_delete)
        self.line(v, "Edit", self.kp_copy, self.kp_del); self.kp_name = QLineEdit(); self.kp_name.setPlaceholderText("name"); self.kp_name.setMinimumWidth(120); self.kp_save = self.btn("Save", self.kp_save_do)
        self.line(v, "Save as", self.kp_name, self.kp_save); self.line(v, "Default", self.btn("Reset to default", self.kp_reset)); self.kp_msg = self.hint(""); v.addWidget(self.kp_msg)
        self.kp_edit = {}
        for g in keybinds.GESTURES:
            v = self.card(lay, keybinds.LABELS[g]); ed = ActionEditor(keybinds.LABELS[g]); v.addWidget(ed); self.kp_edit[g] = ed
            ed.changed.connect(self.kp_dirty); ed.test_btn.clicked.connect(lambda _, ed=ed: self.do(lambda: actions.run(ed.value()) or "ok", lambda r: str(r), refresh=False))
        v = self.card(lay, "Timing"); self.kp_long = self.slider(v, "Long press after", 300, 2000, 50, lambda x: self.kp_dirty(), suffix=" ms"); self.kp_multi = self.slider(v, "Taps count together within", 200, 1000, 50, lambda x: self.kp_dirty(), suffix=" ms")
        self.kp_loading = False; self.reload_keyprofiles(); lay.addStretch()

    def reload_keyprofiles(self, select=None):
        ns = keybinds.names(); self.kp_list.clear(); self.kp_list.addItems(ns); sel = select or keybinds.current()["profile"]; self.kp_list.setCurrentIndex(max(0, self.kp_list.findText(sel)))
        self.load_keyprofile(self.kp_list.currentText() if self.kp_list.findText(sel) >= 0 else "default", current=sel == "custom")

    def load_keyprofile(self, name, current=False):
        try: p = keybinds.load(name) if not current else keybinds.current()
        except Exception as e: self.say(f"Error: {e}"); return
        locked = name in keybinds.BUILTIN and not current; self.kp_loading = True
        for g, ed in self.kp_edit.items(): ed.set_spec(p[g]); ed.type.setEnabled(not locked); [w.setEnabled(not locked) for w in (ed.combo, ed.edit, ed.browse, ed.spin)]
        self._set(self.kp_long, p["long_ms"]); self._set(self.kp_multi, p["multi_ms"]); self.kp_long.setEnabled(not locked); self.kp_multi.setEnabled(not locked)
        self.kp_loading = False; self.kp_copy.setVisible(locked); self.kp_save.setEnabled(not locked); self.kp_del.setEnabled(not locked)
        self.kp_msg.setText("Built-in profiles can't be edited. Press Copy to edit to make your own version." if locked else (p.get("description") or "Change the actions, then Save and Use."))

    def kp_dirty(self, *_): pass

    def kp_profile(self):
        p = {g: ed.value() for g, ed in self.kp_edit.items()}; p["long_ms"] = self.kp_long.value(); p["multi_ms"] = self.kp_multi.value(); return keybinds.validate(p)

    def kp_use(self):
        name = self.kp_list.currentText()
        try: p = self.kp_profile() if name not in keybinds.BUILTIN else keybinds.load(name)
        except Exception as e: self.kp_msg.setText(str(e)); self.say(f"Error: {e}"); return
        def go():
            if name not in keybinds.BUILTIN: keybinds.save(name, p)
            keybinds.use(name); return name
        self.do(go, lambda n: f"PREDATOR key: using '{n}' (the key service reloads by itself)", refresh=False)

    def kp_save_do(self):
        name = self.kp_name.text().strip() or self.kp_list.currentText()
        try: keybinds.save(name, self.kp_profile())
        except Exception as e: self.kp_msg.setText(str(e)); self.say(f"Error: {e}"); return
        self.reload_keyprofiles(name); self.kp_msg.setText(f"Saved '{name}'. Press Use to switch the key to it."); self.say(f"Saved key profile '{name}'")

    def kp_reset(self):
        self.do(lambda: keybinds.use("native"), "PREDATOR key reset to the default bindings", refresh=False); self.reload_keyprofiles("native")

    def kp_copy_do(self):
        src = self.kp_list.currentText()
        try: n = keybinds.copy(src, self.kp_name.text().strip() or None)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_keyprofiles(n); self.kp_msg.setText(f"Copied to '{n}'. Change what you like, then Save and Use."); self.say(f"Copied '{src}' to '{n}'")

    def kp_delete(self):
        name = self.kp_list.currentText()
        try: keybinds.delete(name)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_keyprofiles(); self.say(f"Deleted key profile '{name}'")

    # ---- Undervolt ---------------------------------------------------------------------------------------------------------------------
    def p_undervolt(self):
        lay = self.page("Undervolt")
        warn = self.hint("Lowers CPU or GPU voltage to run cooler. Too much can freeze, reboot or crash games. Nothing is applied unless you confirm, and a reboot always returns to stock. Test a profile under load before relying on it.")
        warn.setStyleSheet("color: #ffb347;"); lay.addWidget(warn)
        self.uv_tabs = Segmented(["CPU", "GPU"], tiles=True, icons=["performance", "gpu"]); lay.addWidget(self.uv_tabs); uvstack = TabPages(); lay.addWidget(uvstack)
        self.uv_tabs.picked.connect(uvstack.setCurrentIndex); self.uv_tabs.set_index(0); page_lay = lay
        cpu_w = QWidget(); lay = QVBoxLayout(cpu_w); lay.setContentsMargins(0, 0, 0, 0); lay.setSpacing(14)
        v = self.card(lay, "Right now", "", True); self.uv_now = v.right; self.uv_state = self.hint("Reading the current voltage…"); v.addWidget(self.uv_state)
        v = self.card(lay, "Profile")
        self.uv_list = QComboBox(); self.uv_list.setMinimumWidth(150); self.uv_list.activated.connect(lambda i: self.load_uv(self.uv_list.currentText()))
        self.line(v, "Profile", self.uv_list, self.btn("Reset", self.uv_reset), self.btn("Apply", self.uv_apply, True)); self.uv_copy = self.btn("Copy to edit", self.uv_copy_do); self.uv_del = self.btn("Delete", self.uv_delete)
        self.line(v, "Edit", self.uv_copy, self.uv_del)
        self.uv_core = self.slider(v, "Core offset", 0, undervolt.MAX_MV, 5, lambda x: None, suffix=" mV", prefix="-"); self.uv_cache = self.slider(v, "Cache offset", 0, undervolt.MAX_MV, 5, lambda x: None, suffix=" mV", prefix="-")
        self.uv_risk = QLabel(""); self.uv_risk.setWordWrap(True); v.addWidget(self.uv_risk)
        self.uv_core.valueChanged.connect(lambda *_: self.update_uv_risk()); self.uv_cache.valueChanged.connect(lambda *_: self.update_uv_risk())
        self.uv_name = QLineEdit(); self.uv_name.setPlaceholderText("name"); self.uv_name.setMinimumWidth(120); self.uv_save = self.btn("Save", self.uv_save_do)
        self.line(v, "Save as", self.uv_name, self.uv_save); self.uv_msg = self.hint(""); v.addWidget(self.uv_msg)
        self.line(v, "Default", self.btn("Reset to default (native)", self.uv_reset))
        v = self.card(lay, "At boot")
        self.uv_boot = self.switch(v, "Apply the selected profile at boot", self.uv_boot_set, "Off by default. Guarded: if the laptop does not stay up for 10 minutes after applying it, the next boot skips it and turns this off.")
        v = self.card(lay, "Power limits", "", True); self.pl_now = v.right
        v.addWidget(self.hint("How much power the CPU may draw: PL1 is sustained, PL2 is short bursts. Stock is 80 W. TrueSense keeps your values until you go back to automatic."))
        self.pl1, self.pl2 = QDoubleSpinBox(), QDoubleSpinBox()
        for s_, val in ((self.pl1, 65), (self.pl2, 80)): s_.setRange(25, 120); s_.setDecimals(0); s_.setSuffix(" W"); s_.setValue(val)
        self.line(v, "PL1 / PL2", self.pl1, self.pl2); self.pl_risk = QLabel(""); self.pl_risk.setWordWrap(True); v.addWidget(self.pl_risk)
        for s_ in (self.pl1, self.pl2): s_.valueChanged.connect(lambda *_: self.update_pl_risk())
        self.line(v, "Limits", self.btn("Use automatic", self.pl_auto), self.btn("Apply", self.pl_apply, True)); self.pl_state = self.hint(""); v.addWidget(self.pl_state); self.update_pl_risk()
        self.reload_uv(); lay.addStretch(); uvstack.addWidget(cpu_w)
        gpu_w = QWidget(); lay = QVBoxLayout(gpu_w); lay.setContentsMargins(0, 0, 0, 0); lay.setSpacing(14); self.build_gpu_uv(lay); uvstack.addWidget(gpu_w); page_lay.addStretch()

    def build_gpu_uv(self, lay):
        lay.addWidget(self.hint("GPU undervolt shifts the GPU's voltage/frequency curve up and caps the top clock, so it reaches its speed at a lower voltage. Neither setting survives a reboot."))
        v = self.card(lay, "Right now", "", True); self.gpu_now = v.right; self.gpu_state = self.hint("Reading the GPU clocks…"); v.addWidget(self.gpu_state)
        v = self.card(lay, "Profile")
        self.gp_list = QComboBox(); self.gp_list.setMinimumWidth(150); self.gp_list.activated.connect(lambda i: self.load_gpu_uv(self.gp_list.currentText()))
        self.line(v, "Profile", self.gp_list, self.btn("Reset", self.gpu_uv_reset), self.btn("Apply", self.gpu_uv_apply, True)); self.gp_copy = self.btn("Copy to edit", self.gpu_uv_copy); self.gp_del = self.btn("Delete", self.gpu_uv_delete)
        self.line(v, "Edit", self.gp_copy, self.gp_del)
        self.gp_off = self.slider(v, "Curve offset", gpuundervolt.OFFSET_RANGE[0], gpuundervolt.OFFSET_RANGE[1], 10, lambda x: None, suffix=" MHz")
        self.gp_cap = self.slider(v, "Clock cap (below 600 = none)", 0, gpuundervolt.CAP_RANGE[1], 50, lambda x: None, suffix=" MHz")
        self.gp_risk = QLabel(""); self.gp_risk.setWordWrap(True); v.addWidget(self.gp_risk)
        self.gp_off.valueChanged.connect(lambda *_: self.update_gpu_risk()); self.gp_cap.valueChanged.connect(lambda *_: self.update_gpu_risk())
        self.gp_name = QLineEdit(); self.gp_name.setPlaceholderText("name"); self.gp_name.setMinimumWidth(120); self.gp_save = self.btn("Save", self.gpu_uv_save)
        self.line(v, "Save as", self.gp_name, self.gp_save); self.gp_msg = self.hint(""); v.addWidget(self.gp_msg); self.line(v, "Default", self.btn("Reset to default (native)", self.gpu_uv_reset))
        v = self.card(lay, "At boot")
        self.gp_boot = self.switch(v, "Apply the selected profile at boot", self.gpu_boot_set, "Off by default. Guarded: if the laptop does not stay up for 10 minutes after applying it, the next boot skips it and turns this off.")
        self.reload_gpu_uv(); lay.addStretch()

    def reload_gpu_uv(self, select=None):
        ns = gpuundervolt.names(); self.gp_list.clear(); self.gp_list.addItems(ns); self.gp_list.setCurrentIndex(max(0, self.gp_list.findText(select or "native"))); self.load_gpu_uv(self.gp_list.currentText())

    def load_gpu_uv(self, name):
        try: p = gpuundervolt.load(name)
        except Exception as e: self.say(f"Error: {e}"); return
        locked = name in gpuundervolt.BUILTIN; self.loading = True; self.gp_off.setValue(p["offset"]); self.gp_cap.setValue(p["cap"]); self.loading = False; self.update_gpu_risk()
        for s_ in (self.gp_off, self.gp_cap): s_.setEnabled(not locked)
        self.gp_copy.setVisible(locked); self.gp_save.setEnabled(not locked); self.gp_del.setEnabled(not locked)
        self.gp_msg.setText(p.get("description") or ("Built-in profiles can't be edited. Press Copy to edit." if locked else ""))

    def gpu_uv_values(self): return {"offset": self.gp_off.value(), "cap": self.gp_cap.value() if self.gp_cap.value() >= gpuundervolt.CAP_RANGE[0] else 0}

    def update_gpu_risk(self):
        v = self.gpu_uv_values(); lvl, why = gpuundervolt.risk(v["offset"], v["cap"]); self.show_risk(self.gp_risk, lvl, why)

    def gpu_uv_apply(self):
        name = self.gp_list.currentText(); v = self.gpu_uv_values()
        if v["offset"] == 0 and v["cap"] == 0: self.gpu_uv_reset(); return
        lvl, why = gpuundervolt.risk(v["offset"], v["cap"]); go, force = self.confirm_risk("Apply GPU undervolt", f"Apply '{name}': curve offset {v['offset']:+d} MHz, clock cap {v['cap'] or 'none'} MHz?", lvl, why)
        if not go: return
        self.do(lambda: ipc.call("gpu_undervolt", action="apply", offset=v["offset"], cap=v["cap"], confirm=True, name=name, force=force), lambda r: f"GPU undervolt applied: offset {r['offset_mhz']:+d} MHz, cap {r['cap_mhz'] or 'none'} MHz", refresh=False); QTimer.singleShot(900, self.uv_refresh)

    def gpu_uv_reset(self):
        self.do(lambda: (ipc.call("gpu_undervolt", action="boot_clear"), ipc.call("gpu_undervolt", action="reset"))[1], "GPU undervolt reset to stock (no GPU undervolt at boot)", refresh=False); self.reload_gpu_uv("native"); QTimer.singleShot(900, self.uv_refresh)

    def gpu_boot_set(self, on):
        if self.loading: return
        name = self.gp_list.currentText(); v = self.gpu_uv_values()
        if not on: self.do(lambda: ipc.call("gpu_undervolt", action="boot_clear"), "GPU undervolt at boot: off", refresh=False); return
        lvl, why = gpuundervolt.risk(v["offset"], v["cap"]); go, force = self.confirm_risk("GPU undervolt at boot", f"Apply '{name}' every time the laptop starts?\n\nIf it does not stay up for 10 minutes after applying it, the next boot skips it and this switches off.", lvl, why)
        if not go: self.loading = True; self.gp_boot.setChecked(False); self.loading = False; return
        self.do(lambda: ipc.call("gpu_undervolt", action="boot", offset=v["offset"], cap=v["cap"], confirm=True, name=name, force=force), f"GPU undervolt at boot: {name}", refresh=False)

    def gpu_uv_save(self):
        name = self.gp_name.text().strip() or self.gp_list.currentText()
        try: gpuundervolt.save(name, self.gpu_uv_values())
        except Exception as e: self.gp_msg.setText(str(e)); self.say(f"Error: {e}"); return
        self.reload_gpu_uv(name); self.say(f"Saved GPU undervolt profile '{name}'")

    def gpu_uv_copy(self):
        src = self.gp_list.currentText()
        try: n = gpuundervolt.copy(src, self.gp_name.text().strip() or None)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_gpu_uv(n); self.gp_msg.setText(f"Copied to '{n}'. Change it, then Save and Apply."); self.say(f"Copied '{src}' to '{n}'")

    def gpu_uv_delete(self):
        name = self.gp_list.currentText()
        try: gpuundervolt.delete(name)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_gpu_uv(); self.say(f"Deleted GPU undervolt profile '{name}'")

    def reload_uv(self, select=None):
        ns = undervolt.names(); self.uv_list.clear(); self.uv_list.addItems(ns); self.uv_list.setCurrentIndex(max(0, self.uv_list.findText(select or "native"))); self.load_uv(self.uv_list.currentText())

    def load_uv(self, name):
        try: p = undervolt.load(name)
        except Exception as e: self.say(f"Error: {e}"); return
        locked = name in undervolt.BUILTIN; self.loading = True; self.uv_core.setValue(-p["core"]); self.uv_cache.setValue(-p["cache"]); self.loading = False; self.update_uv_risk()
        for s_ in (self.uv_core, self.uv_cache): s_.setEnabled(not locked)
        self.uv_copy.setVisible(locked); self.uv_save.setEnabled(not locked); self.uv_del.setEnabled(not locked)
        self.uv_msg.setText(p.get("description") or ("Built-in profiles can't be edited. Press Copy to edit." if locked else ""))

    RISK_COLOR = {"safe": "#34c759", "caution": "#ffb347", "danger": "#ff5d6c"}

    def show_risk(self, label, level, why):
        label.setText(why); label.setStyleSheet(f"color: {self.RISK_COLOR[level]}; font-weight: 600;")

    def confirm_risk(self, title, what, level, why):
        """Ask before changing voltages or power limits. Past the safety line the warning is stronger; beyond the danger line you must type APPLY. Returns (go, force)."""
        text = f"{what}\n\n{why}\n\nA reboot always returns voltages to stock. Save your work first."
        if level == "danger":
            word, ok = QInputDialog.getText(self, title, text + "\n\nThis is beyond the danger line. Type APPLY to go ahead:")
            return (ok and word.strip() == "APPLY"), True
        icon = QMessageBox.Icon.Warning if level == "caution" else QMessageBox.Icon.Question
        box = QMessageBox(icon, title, text, QMessageBox.StandardButton.Apply | QMessageBox.StandardButton.Cancel, self); box.setDefaultButton(QMessageBox.StandardButton.Cancel)
        return box.exec() == QMessageBox.StandardButton.Apply, False

    def update_pl_risk(self):
        lvl, why = cpu.pl_risk(self.pl1.value(), self.pl2.value()); self.show_risk(self.pl_risk, lvl, why if self.pl2.value() >= self.pl1.value() else "PL2 (burst) must not be below PL1 (sustained).")

    def pl_apply(self):
        a_, b_ = self.pl1.value(), self.pl2.value()
        if b_ < a_: self.say("Error: PL2 (burst) must not be below PL1 (sustained)"); return
        lvl, why = cpu.pl_risk(a_, b_); go, force = self.confirm_risk("Apply power limits", f"Set PL1 {a_:g} W and PL2 {b_:g} W?", lvl, why) if lvl != "safe" else (True, False)
        if not go: return
        self.do(lambda: ipc.call("cpu_limits", pl1=a_, pl2=b_, force=force), lambda r: f"Power limits: PL1 {r['pl1_w']:g} W, PL2 {r['pl2_w']:g} W", refresh=False); QTimer.singleShot(900, self.uv_refresh)

    def pl_auto(self):
        self.do(lambda: ipc.call("cpu_limits", reset=True), "Power limits are automatic again", refresh=False); QTimer.singleShot(900, self.uv_refresh)

    def uv_values(self): return {"core": -self.uv_core.value(), "cache": -self.uv_cache.value()}

    def update_uv_risk(self):
        v = self.uv_values(); lvl, why = undervolt.risk(v["core"], v["cache"]); self.show_risk(self.uv_risk, lvl, why)

    def uv_apply(self):
        name = self.uv_list.currentText(); vals = self.uv_values()
        if vals["core"] == 0 and vals["cache"] == 0: self.uv_reset(); return
        lvl, why = undervolt.risk(vals["core"], vals["cache"]); go, force = self.confirm_risk("Apply CPU undervolt", f"Apply '{name}': core {vals['core']} mV, cache {vals['cache']} mV?", lvl, why)
        if not go: return
        self.do(lambda: ipc.call("undervolt", action="apply", core=vals["core"], cache=vals["cache"], confirm=True, name=name, force=force), lambda r: f"Undervolt applied: core {r['core_mv']} mV, cache {r['cache_mv']} mV", refresh=False); QTimer.singleShot(900, self.uv_refresh)

    def uv_reset(self):
        self.do(lambda: (ipc.call("undervolt", action="boot_clear"), ipc.call("undervolt", action="reset"))[1], "Undervolt reset to stock (no undervolt at boot)", refresh=False); self.reload_uv("native"); QTimer.singleShot(900, self.uv_refresh)

    def uv_boot_set(self, on):
        if self.loading: return
        name = self.uv_list.currentText(); vals = self.uv_values()
        if not on: self.do(lambda: ipc.call("undervolt", action="boot_clear"), "Undervolt at boot: off", refresh=False); return
        lvl, why = undervolt.risk(vals["core"], vals["cache"]); go, force = self.confirm_risk("Undervolt at boot", f"Apply '{name}' (core {vals['core']} mV, cache {vals['cache']} mV) every time the laptop starts?\n\nIf it does not stay up for 10 minutes after applying it, the next boot skips it and this switches off.", lvl, why)
        if not go: self.loading = True; self.uv_boot.setChecked(False); self.loading = False; return
        self.do(lambda: ipc.call("undervolt", action="boot", core=vals["core"], cache=vals["cache"], confirm=True, name=name, force=force), f"Undervolt at boot: {name}", refresh=False)

    def uv_refresh(self):
        def work():
            try: r = ipc.call("undervolt", action="get")
            except Exception as e: r = {"error": str(e)}
            try: r["cpu"] = ipc.call("cpu_info")
            except Exception as e: r["cpu"] = {"error": str(e)}
            try: r["gpu"] = ipc.call("gpu_undervolt", action="get")
            except Exception as e: r["gpu"] = {"error": str(e)}
            self.say_uv(r)
        threading.Thread(target=work, daemon=True).start()

    def say_uv(self, r):
        try: self.bus.uv.emit(r)
        except RuntimeError: pass

    def _uv_show(self, r):
        if "core_mv" in r:
            self.uv_now.setText(f"core {r['core_mv']} mV · cache {r['cache_mv']} mV")
            self.uv_state.setText((f"Applied: {r['applied']['name']}. " if r.get("applied") else "Stock voltage. ") + (f"At boot: {r['boot']['name']}. " if r.get("boot") else "") + ("A boot undervolt is waiting to prove stable." if r.get("pending") else ""))
        else: self.uv_now.setText("not readable"); self.uv_state.setText(str(r.get("error", ""))[:160])
        self.loading = True; self.uv_boot.setChecked(bool(r.get("boot"))); self.loading = False
        c = r.get("cpu") or {}
        if c.get("pl1_w"):
            self.pl_now.setText(f"PL1 {c['pl1_w']:g} W · PL2 {c['pl2_w']:g} W"); self.pl_state.setText(f"Yours: {c['user']['pl1']:g} / {c['user']['pl2']:g} W." if c.get("user") else f"Automatic: {c['auto']['normal_w']} W, {c['auto']['gaming_w']} W when gaming.")
            if not getattr(self, "_pl_init", False): self._pl_init = True; self.pl1.setValue(c["user"]["pl1"] if c.get("user") else c["pl1_w"]); self.pl2.setValue(c["user"]["pl2"] if c.get("user") else c["pl2_w"])
        g = r.get("gpu") or {}
        if "offset_mhz" in g:
            self.gpu_now.setText(f"offset {g['offset_mhz']:+d} MHz"); self.gpu_state.setText((f"Applied: {g['applied']['name']} (cap {g['applied']['cap'] or 'none'} MHz). " if g.get("applied") else "Not applied by TrueSense. ") + (f"At boot: {g['boot']['name']}. " if g.get("boot") else "") + f"Driver range {g['range_mhz'][0]} to {g['range_mhz'][1]} MHz.")
        else: self.gpu_now.setText("not readable"); self.gpu_state.setText(str(g.get("error", ""))[:160])
        self.loading = True; self.gp_boot.setChecked(bool(g.get("boot"))); self.loading = False

    def uv_save_do(self):
        name = self.uv_name.text().strip() or self.uv_list.currentText()
        try: undervolt.save(name, self.uv_values())
        except Exception as e: self.uv_msg.setText(str(e)); self.say(f"Error: {e}"); return
        self.reload_uv(name); self.say(f"Saved undervolt profile '{name}'")

    def uv_copy_do(self):
        src = self.uv_list.currentText()
        try: n = undervolt.copy(src, self.uv_name.text().strip() or None)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_uv(n); self.uv_msg.setText(f"Copied to '{n}'. Change the offsets, then Save and Apply."); self.say(f"Copied '{src}' to '{n}'")

    def uv_delete(self):
        name = self.uv_list.currentText()
        try: undervolt.delete(name)
        except Exception as e: self.say(f"Error: {e}"); return
        self.reload_uv(); self.say(f"Deleted undervolt profile '{name}'")

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
        self.editor = CurveEditor(); lay.addWidget(self.editor)
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
        self.line(v, "Edit", self.copy_btn, self.del_btn); self.line(v, "Save as", self.curve_name, self.curve_save_btn)
        self.line(v, "Default", self.btn("Reset to default", self.curve_reset)); self.curve_msg = self.hint(""); v.addWidget(self.curve_msg)
        self.reload_curves(); lay.addStretch()

    def reload_curves(self, select=None):
        self.curve_list.clear(); self.curve_list.addItems(curves.names()); self.curve_cb.clear(); self.curve_cb.addItems(curves.names())
        sel = select or "native"; i = self.curve_list.findText(sel); self.curve_list.setCurrentIndex(max(0, i)); self.curve_cb.setCurrentIndex(max(0, self.curve_cb.findText(sel)))
        self.load_curve_in_editor(self.curve_list.currentText())

    def load_curve_in_editor(self, name):
        try: c = curves.load(name)
        except Exception as e: self.say(f"Error: {e}"); return
        locked = name in curves.BUILTIN; self.editor.locked = locked; self.editor.set_curve(c); self.loading = True
        for s_ in (self.c_pre, self.c_hys, self.c_rate): s_.setEnabled(not locked)
        self.copy_btn.setVisible(locked); self.del_btn.setEnabled(not locked); self.curve_save_btn.setEnabled(not locked)
        self.curve_msg.setText(("Native: the firmware controls the fans, exactly like PredatorSense on Windows. There is no curve to edit." if c.get("native") else "Built-in curves can't be edited. Press Copy to edit to make your own version.") if locked else "")
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

    def curve_reset(self):
        """Back to the built-in default curve (ours), in use and in the editor."""
        self.do(lambda: ipc.call("fan_curve", action="use", name="native"), "Fan control reset to the default (native: the firmware)", refresh=False); self.reload_curves("native")

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
        self.line(v, "Edit", self.sm_copy, self.sm_del); self.line(v, "Save as", self.sm_name, self.sm_save); self.line(v, "Default", self.btn("Reset to default", self.smart_reset)); self.sm_msg = self.hint(""); v.addWidget(self.sm_msg); self.reload_smart()
        v = self.card(lay, "Game rules")
        v.addWidget(self.hint("While one of these programs runs, the power mode is at least the one you pick (Native and Smart auto). When it closes, auto takes over again. Turbo still backs off if the laptop gets too hot."))
        self.gr_name = QLineEdit(); self.gr_name.setPlaceholderText("program, e.g. eldenring.exe or hl2_linux"); self.gr_name.setMinimumWidth(220)
        self.gr_mode = QComboBox(); self.gr_mode.addItems([m.capitalize() for m in gamerules.MODES]); self.gr_mode.setCurrentIndex(gamerules.MODES.index("performance"))
        self.line(v, "Program", self.gr_name, self.gr_mode, self.btn("Add", self.game_add, True)); self.gr_list = QComboBox(); self.gr_list.setMinimumWidth(220)
        self.line(v, "Rules", self.gr_list, self.btn("Remove", self.game_remove)); self.gr_msg = self.hint(""); v.addWidget(self.gr_msg); self.game_reload(); lay.addStretch()

    def game_call(self, **kw):
        def go():
            try: r = ipc.call("game_rules", **kw); self.bus_game(r)
            except Exception as e: self.say(f"Error: {e}")
        threading.Thread(target=go, daemon=True).start()

    def bus_game(self, r):
        try: self.bus.game.emit(r)
        except RuntimeError: pass

    def game_show(self, r):
        self.gr_list.clear()
        for k, m in sorted(r["rules"].items()): self.gr_list.addItem(f"{k}  →  {m}", k)
        self.gr_msg.setText((f"A game rule is active now: {r['running']}." if r.get("running") else "No game rule is active now.") if r["rules"] else "No game rules yet.")

    def game_reload(self): self.game_call(action="list")
    def game_add(self):
        n = self.gr_name.text().strip()
        if n: self.game_call(action="add", name=n, mode=gamerules.MODES[self.gr_mode.currentIndex()]); self.gr_name.clear()
    def game_remove(self):
        k = self.gr_list.currentData()
        if k: self.game_call(action="remove", name=k)

    def reload_smart(self, select=None):
        ns = smartprofiles.names(); self.sm_list.clear(); self.sm_list.addItems(ns); self.smart_cb.clear(); self.smart_cb.addItems(ns)
        sel = select or "native"; self.sm_list.setCurrentIndex(max(0, self.sm_list.findText(sel))); self.smart_cb.setCurrentIndex(max(0, self.smart_cb.findText(sel))); self.load_smart_in_editor(self.sm_list.currentText())

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

    def smart_reset(self):
        self.do(lambda: ipc.call("smart_profile", action="use", name="native"), "Smart auto reset to the default profile (native)", refresh=False); self.reload_smart("native")

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

    # ---- refresh -----------------------------------------------------------------------------------------------------------------------
    def _sens_show(self, s):
        self.stat_cpu.setText(f"CPU: {s['cpu_temp']} °C · {s['cpu_fan_rpm']} RPM"); self.stat_gpu.setText(f"GPU: {s['gpu_temp']} °C · {s['gpu_fan_rpm']} RPM")
        self.stat_fan.setText(f"{max(s['cpu_fan_rpm'], s['gpu_fan_rpm'])} RPM"); self.stat_bat.setText(self.src)
        if self.pages.currentIndex() == PAGES["curves"]: self.editor.marks = {"cpu": s["cpu_temp"], "gpu": s["gpu_temp"]}; self.editor.update()

    def _read_sensors(self):
        """Temperatures and fan speeds, read off the UI thread (a slow or stuck device must never freeze the window)."""
        try: s = hw.sensors()
        except HwError: s = None
        self.sensing = False
        if s is not None:
            try: self.bus.sens.emit(s)
            except RuntimeError: pass

    def tick(self):
        """Once a second: temperatures and fans. Every 2 s (every 6 s when the window is not the focused one), when you are not touching anything, re-read what changes by itself: power mode,
        fans, power source (mostly from the service, one cheap call). The slow settings (lighting, battery, screen, boot, GPU) are re-read every 20 s and whenever you come back to the window,
        open a page or change something. Every hardware read costs the firmware a few milliseconds, so the window asks for as little as it can."""
        if not self.built or not self.isVisible() or self.isMinimized(): return
        active = self.isActiveWindow(); self.ticks += 1
        if (active or self.ticks % 2 == 0) and not self.sensing: self.sensing = True; threading.Thread(target=self._read_sensors, daemon=True).start()
        idle = time.monotonic() - self.last_action > 2.0 and not any(self.busy.values()) and not self.pending
        dragging = any(s.isSliderDown() for s in (self.kb_level, self.kb_speed, self.rear_bright, self.rear_speed, self.fan_pct, self.fan_cpu, self.fan_gpu, self.boost, self.pk_level))
        popup = any(c.view().isVisible() for c in (self.kb_fx,))
        if self.ticks % (2 if active else 6) == 0 and idle and not dragging and not popup and not self.refreshing:
            if self.pages.currentIndex() == PAGES["audio"]: self.full_refresh("audio_fast")
            else: self.full_refresh("home" if (active and self.ticks % 20 == 0) else "home_fast")

    def event(self, e):
        if e.type() == QEvent.Type.WindowActivate and getattr(self, "bus", None) is not None and not self.closing: QTimer.singleShot(150, self.refresh_if_stale)    # back at the window: show things as they are now
        return super().event(e)

    last_full = 0.0

    def refresh_if_stale(self, max_age=5.0, scope=None):
        """The automatic refreshes (opening a page, coming back to the window) skip the full hardware read when one just ran: every read costs the firmware time. The audio page is always read."""
        if self.refreshing or (scope in (None, "home") and time.monotonic() - self.last_full < max_age): return
        self.full_refresh(scope)

    def full_refresh(self, scope=None):
        """Read the hardware off the UI thread. The audio page (pactl, amixer, DTS) is only read while it is shown, so the main page stays quick."""
        scope = scope or ("audio" if self.pages.currentIndex() == PAGES["audio"] else "home")
        if scope == "home": self.last_full = time.monotonic()
        self.refreshing = True
        def work():
            d = {}
            def get(k, fn):
                try: d[k] = fn()
                except Exception: pass
            if scope in ("audio", "audio_fast"):
                get("out", audio.output_status); get("jack", audio.jack_status); get("mic", audio.mic_state); get("boost", audio.headset_boost); get("voice", audio.voice_modes)
                get("jackset", lambda: config.load()["audio"]["jack"]); get("spkf", audio.speaker_filter_on); get("sound", audio.sound_state)
                if scope == "audio": get("dts", lambda: "DTS:            on" in audio.dts_status())      # slow (starts a helper): only when the page opens or after a change
            elif scope == "home_fast":
                get("state", lambda: ipc.call("state")); live = (d.get("state") or {}).get("live") or {}
                if live.get("power_mode") in hw.MODES and "ac" in live: d["mode"] = live["power_mode"]; d["ac"] = bool(live["ac"])       # the service already knows these: no hardware read
                else: get("mode", hw.get_mode); get("ac", hw.on_ac)
                get("fan", hw.fan_state)
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
        if self.closing: return
        if not self.built: self.pending_apply = {**(self.pending_apply or {}), **d}; return
        self.loading = True
        try:
            if "state" in d:
                s = d["state"]["settings"]; am = s.get("auto_mode") or ("smart" if s["auto_profile"] else "off"); self.auto.set_index(AUTO_KEYS.index(am) if am in AUTO_KEYS else 0); self.auto_hint.setText(AUTO_HINT.get(am, "")); self.smart_box.setVisible(am == "smart")
                self.kb_oper.setChecked(bool(s.get("operating_follow")))
                lv = d["state"]["live"]; sp_ = lv.get("smart_profile")
                if sp_ and self.smart_cb.currentText() != sp_ and not self.smart_cb.view().isVisible(): self.smart_cb.setCurrentIndex(max(0, self.smart_cb.findText(sp_)))
                self.smart_status.setText(f"Now: {lv.get('mode_reason')}" if lv.get("mode_reason") and am != "off" else "")
                hl = d["state"].get("hold_left_s", 0); self.hold_box.setVisible(bool(hl) and am != "off"); self.hold_lbl.setText(f"Your pick stays until something changes (plug, a game starting or stopping, heat) or {max(1, hl // 60)} more min. Then {am} auto takes over by itself.")
                fc = d["state"]["live"].get("fan_curve")
                if fc and self.curve_cb.currentText() != fc and not self.curve_cb.view().isVisible(): self.curve_cb.setCurrentIndex(max(0, self.curve_cb.findText(fc)))
                if d.get("fan", {}).get("mode") == "manual":
                    dp = d["fan"]["duty_register_pct"]; c_, g_ = max(20, dp["cpu"]), max(20, dp["gpu"])
                    self._set(self.fan_cpu, c_); self._set(self.fan_gpu, g_); self._set(self.fan_pct, max(c_, g_))
                fi = 0 if s["smart_fan"] else {"auto": 1, "max": 2, "manual": 3}.get(d.get("fan", {}).get("mode"), 1); self.fan.set_index(fi); self.fan_box.setVisible(fi == 3); self.curve_box.setVisible(fi == 0); self.fan_hint.setText(self.FAN_HINTS[fi])
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
            if "cells" in d and not self.board.sel and not self.busy.get("paint") and not self.pending.get("paint"): self.board.set_cells(["%02x%02x%02x" % tuple(c) for c in d["cells"]])
            if "kb" in d: self._set(self.pk_level, d["kb"])
            for k, w in (("limit", self.limit), ("calib", self.calib), ("od", self.od), ("boot", self.boot), ("dts", self.dts), ("spkf", self.spk)):
                if d.get(k) is not None: w.setChecked(bool(d[k]))
            if "usb" in d: self.usb.set_index(hw.USB_LEVELS.index(d["usb"]) if d["usb"] in hw.USB_LEVELS else -1)
            if "gpu" in d: self.gpu.set_index(list(hw.GPU_MODES).index(d["gpu"]) if d["gpu"] in hw.GPU_MODES else -1)
            if "sound" in d:
                st = d["sound"]; self.snd_lbl.setText("The sound card's output is present." if st.get("ok") else ("No sound card was found." if st.get("ok") is None else "No sound output: " + st.get("problem", "unknown") + ". Press Fix sound."))
            if "out" in d: self.out.set_index({"speakers": 0, "headphones": 1}.get(d["out"]["active"], -1))
            if "jackset" in d:
                keys = [k for k, _ in JACK]; self.jack.set_index(keys.index(d["jackset"]) if d["jackset"] in keys else 0); self.jack_hint.setText(JACK_HINT.get(d["jackset"], ""))
            if "jack" in d: j = d["jack"]; self.jack_state.setText("Detected now: " + ("headset with microphone" if j["headset_mic"] else "headphones" if j["headphones"] else "nothing plugged in"))
            if "mic" in d: self.mute.setChecked(bool(d["mic"]["muted"])); self._set(self.micvol, int(d["mic"].get("volume") or 0))
            if d.get("boost") is not None: self._set(self.boost, d["boost"])
            if "voice" in d:
                v = d["voice"]; self.v_head.set_index(VOICE.index(v["headset"]) if v.get("headset") in VOICE else 0); self.v_arr.set_index(VOICE.index(v["array"]) if v.get("array") in VOICE else 0)
            if "voice" in d: self.v_note.setChecked(bool(config.load()["audio"]["notify"]))
        finally: self.loading = False


_WINDOW = [None]


LOGO = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data", "truesense.svg")


def logo_pixmap(size):
    """The TrueSense logo (src/truesense/data/truesense.svg) at the given size, crisp on high-DPI screens."""
    px = QPixmap(size * 2, size * 2); px.fill(Qt.GlobalColor.transparent)
    if os.path.exists(LOGO): p = QPainter(px); QSvgRenderer(LOGO).render(p); p.end()
    px.setDevicePixelRatio(2); return px


def _icon():
    """The app icon: the logo at every common size (a drawn ring with a T if the file is missing)."""
    if os.path.exists(LOGO):
        ic = QIcon()
        for s in (16, 24, 32, 48, 64, 128, 256):
            px = QPixmap(s, s); px.fill(Qt.GlobalColor.transparent); p = QPainter(px); p.setRenderHint(QPainter.RenderHint.Antialiasing); QSvgRenderer(LOGO).render(p); p.end(); ic.addPixmap(px)
        return ic
    pm = QPixmap(128, 128); pm.fill(Qt.GlobalColor.transparent); p = QPainter(pm); p.setRenderHint(QPainter.RenderHint.Antialiasing)
    p.setBrush(QColor("#17181c")); p.setPen(QPen(QColor(ACCENT), 9)); p.drawEllipse(10, 10, 108, 108)
    f = QFont(); f.setBold(True); f.setPixelSize(70); p.setFont(f); p.setPen(QColor(ACCENT)); p.drawText(pm.rect(), Qt.AlignmentFlag.AlignCenter, "T"); p.end(); return QIcon(pm)


def main(page=None):
    if Qt is None:
        print(f"The window needs python-pyqt6 ({_IMPORT_ERROR}).  Arch: sudo pacman -S python-pyqt6.  Everything works from the command line without it.")
        return 1
    __import__("os").environ.setdefault("QT_LOGGING_RULES", "qt.qpa.wayland.textinput=false")
    def hook(kind, value, tb):
        """PyQt aborts the program on an uncaught exception inside a slot; log it and keep the window alive instead."""
        import traceback
        traceback.print_exception(kind, value, tb, file=sys.stderr)
        w_ = _WINDOW[0]
        if w_ is not None:
            try: w_.say(f"Error: {value}")
            except Exception: pass
    sys.excepthook = hook; threading.excepthook = lambda a: hook(a.exc_type, a.exc_value, a.exc_traceback)
    keyboard.prewarm()                                                    # opens the keyboard interface (about 50 ms) while Qt starts up
    app = QApplication(sys.argv[:1]); app.setApplicationName("TrueSense"); app.setWindowIcon(_icon()); app.setDesktopFileName("truesense"); app.setStyle("Fusion"); app.setStyleSheet(STYLE)
    name = "truesense-gui-" + str(__import__("os").getuid())
    probe = QLocalSocket(); probe.connectToServer(name)
    if probe.waitForConnected(150):                                   # already open: bring that window forward instead of starting a second one
        probe.write((page or "home").encode()); probe.flush(); probe.waitForBytesWritten(300); return 0
    QLocalServer.removeServer(name); server = QLocalServer(); server.listen(name)
    w = Window(lazy=True); _WINDOW[0] = w; w.show()
    if page in PAGES or page == "keys": w.go(page)
    def handoff():
        c = server.nextPendingConnection()
        if c and c.waitForReadyRead(300):
            p_ = bytes(c.readAll()).decode()
            if p_ in PAGES: w.ensure_built(); w.pages.setCurrentIndex(PAGES[p_])
        w.showNormal(); w.raise_(); w.activateWindow()
    server.newConnection.connect(handoff)
    return app.exec()
