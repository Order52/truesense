# SPDX-License-Identifier: MIT
"""`truesense tui`: the whole control surface in a terminal (curses, Python's own: nothing extra to install; works over SSH and on a bare console).

Layers, so each can be tested without a terminal:
  Data    reads the hardware and the service in the background; the screen never waits for them
  Row     one setting (choice, toggle, slider, button, info) with how to read and write it
  Canvas  what is drawn on (a curses window, or a text grid in the tests)
  App     pages, keys, mouse, status line, dialogs
Keys: arrows or hjkl move and change, Enter or Space applies, Tab / 1-7 change page, ? help, q quits. The mouse works: click a tab, a row, an option, a slider.
"""
import os
import sys
import textwrap
import threading
import time
from collections import deque

from . import __version__
from .device import HwError

PAGES_N = 7
MODE_ORDER = ("eco", "quiet", "balanced", "performance", "turbo")
MODE_LABEL = {"eco": "Eco", "quiet": "Quiet", "balanced": "Balanced", "performance": "Performance", "turbo": "Turbo"}
MODE_STYLE = {"eco": "m_eco", "quiet": "m_quiet", "balanced": "m_balanced", "performance": "m_performance", "turbo": "m_turbo"}
COLORS = [("White", "ffffff"), ("Red", "ff0000"), ("Orange", "ff7a00"), ("Yellow", "ffd000"), ("Green", "00ff40"), ("Teal", "00aec7"), ("Blue", "0050ff"), ("Purple", "8000ff"), ("Pink", "ff3fa0")]
DTS_MODES = ["Music", "Movie", "Voice", "Game1", "Game2", "Game3", "Plain"]
KB_DIRECTIONS = [(0, "Left to right"), (1, "Right to left"), (2, "Up"), (3, "Down")]


# ---- glyphs and canvas ----------------------------------------------------------------------------------------------------------------------
class Glyphs:
    def __init__(self, unicode_ok):
        self.unicode = unicode_ok
        if unicode_ok: self.h, self.v, self.tl, self.tr, self.bl, self.br, self.full, self.empty, self.knob, self.rail, self.thin, self.spark, self.dot, self.up, self.bolt = "─", "│", "╭", "╮", "╰", "╯", "█", "░", "●", "━", "─", " ▁▂▃▄▅▆▇█", "●", "▲", "⚡"
        else: self.h, self.v, self.tl, self.tr, self.bl, self.br, self.full, self.empty, self.knob, self.rail, self.thin, self.spark, self.dot, self.up, self.bolt = "-", "|", "+", "+", "+", "+", "#", ".", "o", "=", "-", " .:-=+*#%", "*", "^", "AC"


class Canvas:
    """Something text can be put on. `put` clips to the screen, so callers never worry about the edges."""
    w = h = 0
    glyphs = Glyphs(True)

    def put(self, y, x, text, style="text"): raise NotImplementedError

    def fill(self, y, x, w, style="text", ch=" "):
        for i in range(0, max(0, w)): self.put(y, x + i, ch, style)

    def box(self, y, x, h, w, title="", style="dim", tstyle="accent"):
        g = self.glyphs
        if h < 2 or w < 4: return
        self.put(y, x, g.tl + g.h * (w - 2) + g.tr, style); self.put(y + h - 1, x, g.bl + g.h * (w - 2) + g.br, style)
        for i in range(1, h - 1): self.put(y + i, x, g.v, style); self.put(y + i, x + w - 1, g.v, style)
        if title: self.put(y, x + 2, f" {title} ", tstyle)


class GridCanvas(Canvas):
    """A text screen in memory: what the tests (and screenshots) read."""
    def __init__(self, w=100, h=30, unicode_ok=True):
        self.w, self.h = w, h; self.glyphs = Glyphs(unicode_ok); self.clear()

    def clear(self): self.cells = [[(" ", "text")] * self.w for _ in range(self.h)]

    def put(self, y, x, text, style="text"):
        if not 0 <= y < self.h: return
        for i, ch in enumerate(text):
            if 0 <= x + i < self.w: self.cells[y][x + i] = (ch, style)

    def text(self): return "\n".join("".join(c for c, _ in row).rstrip() for row in self.cells)
    def style_at(self, y, x): return self.cells[y][x][1]


class CursesCanvas(Canvas):
    def __init__(self, scr):
        import curses
        self.c, self.scr = curses, scr; self.glyphs = Glyphs(_utf8()); self.attrs = {}; self._styles(); self.resize()

    def resize(self): self.h, self.w = self.scr.getmaxyx()

    def clear(self): self.scr.erase()

    def _styles(self):
        c = self.c; plain = not c.has_colors() or bool(os.environ.get("NO_COLOR"))
        if plain:
            a = {"text": 0, "dim": c.A_DIM, "accent": c.A_BOLD, "good": 0, "warn": c.A_BOLD, "bad": c.A_BOLD | c.A_UNDERLINE, "sel": c.A_REVERSE, "pill": c.A_REVERSE | c.A_BOLD, "tab": 0, "tabsel": c.A_REVERSE | c.A_BOLD,
                 "title": c.A_BOLD, "key": c.A_BOLD, "head": c.A_BOLD, "on": c.A_REVERSE, "off": c.A_DIM, "bar": c.A_BOLD, "bar_off": c.A_DIM}
            for m in MODE_STYLE.values(): a[m] = c.A_BOLD
        else:
            c.start_color()
            try: c.use_default_colors(); bg = -1
            except c.error: bg = c.COLOR_BLACK
            n = [0]

            def pair(fg, back=bg, extra=0):
                n[0] += 1; c.init_pair(n[0], fg, back); return c.color_pair(n[0]) | extra
            cy, gr, ye, re_, bl, bk, mg = c.COLOR_CYAN, c.COLOR_GREEN, c.COLOR_YELLOW, c.COLOR_RED, c.COLOR_BLUE, c.COLOR_BLACK, c.COLOR_MAGENTA
            a = {"text": 0, "dim": c.A_DIM, "accent": pair(cy, bg, c.A_BOLD), "good": pair(gr), "warn": pair(ye), "bad": pair(re_, bg, c.A_BOLD), "sel": pair(bk, cy), "pill": pair(bk, cy, c.A_BOLD),
                 "tab": c.A_DIM, "tabsel": pair(bk, cy, c.A_BOLD), "title": pair(cy, bg, c.A_BOLD), "key": pair(cy), "head": pair(mg, bg, c.A_BOLD), "on": pair(bk, gr, c.A_BOLD), "off": c.A_DIM,
                 "bar": pair(cy), "bar_off": c.A_DIM}
            for name, col in (("m_eco", gr), ("m_quiet", bl), ("m_balanced", cy), ("m_performance", ye), ("m_turbo", re_)): a[name] = pair(col, bg, c.A_BOLD); a[name + "_sel"] = pair(bk, col, c.A_BOLD)
        for m in MODE_STYLE.values(): a.setdefault(m + "_sel", c.A_REVERSE | c.A_BOLD)
        self.attrs = a

    def put(self, y, x, text, style="text"):
        if not (0 <= y < self.h) or x >= self.w or not text: return
        if x < 0: text = text[-x:]; x = 0
        text = text[: self.w - x - (1 if y == self.h - 1 else 0)]                    # never write the last cell: curses raises there
        try: self.scr.addstr(y, x, text, self.attrs.get(style, 0))
        except self.c.error: pass


def _utf8():
    import locale
    enc = (locale.getpreferredencoding(False) or "").lower()
    return "utf" in enc and os.environ.get("TERM", "") != "linux"


# ---- data: read in the background ---------------------------------------------------------------------------------------------------------
def _battery():
    import glob
    for d in glob.glob("/sys/class/power_supply/BAT*"):
        try:
            with open(d + "/capacity") as f: pct = int(f.read())
            with open(d + "/status") as f: st = f.read().strip()
            return pct, st
        except (OSError, ValueError): continue
    return None, None


class Data:
    """The latest readings. `fast` (every second) is what the dashboard shows; `slow` reads the settings of the page that is open, never twice at once."""
    def __init__(self, on_change=None):
        self.live, self.settings, self.sens, self.fanhw = {}, {}, {}, {}; self.mode = self.ac = None; self.hold = 0; self.batt = (None, None); self.daemon = False; self.err = None
        self.hist = {k: deque(maxlen=160) for k in ("tcpu", "tgpu", "cpu", "gpu", "rpm1", "rpm2")}; self.x = {}; self.errs = {}
        self.on_change = on_change or (lambda: None); self.wake = threading.Event(); self.stop = threading.Event(); self._slow_busy = False; self._slow_again = None; self.t_fast = 0.0

    # -- fast
    def fast(self):
        from . import backend as hw, ipc
        live, settings, hold, daemon = {}, {}, 0, False
        if ipc.available():
            try: st = ipc.call("state", timeout=2); live, settings, hold, daemon = st.get("live") or {}, st.get("settings") or {}, st.get("hold_left_s", 0), True
            except HwError: pass
        err = None; sens, fanhw, mode, ac = self.sens, self.fanhw, self.mode, self.ac
        try:
            if "rpm1" in live and "tcpu" in live:                                      # the service already read these this second: only the fan duty needs the hardware
                sens = {"cpu_temp": live["tcpu"], "sys_temp": live.get("chassis", 0), "gpu_temp": live["tgpu"], "cpu_fan_rpm": live["rpm1"], "gpu_fan_rpm": live["rpm2"]}
            else: sens = hw.sensors()
            fanhw = hw.fan_state()
            mode = live.get("power_mode") if live.get("power_mode") in hw.MODES else hw.get_mode()
            ac = bool(live["ac"]) if "ac" in live else hw.on_ac()
        except Exception as e: err = str(e) or type(e).__name__
        pct, st = _battery()
        self.live, self.settings, self.hold, self.daemon, self.err, self.sens, self.fanhw, self.mode, self.ac, self.batt, self.t_fast = live, settings, hold, daemon, err, sens, fanhw, mode, ac, (pct, st), time.monotonic()
        if not err:
            self.hist["tcpu"].append(live.get("tcpu") or sens.get("cpu_temp", 0)); self.hist["tgpu"].append(live.get("tgpu") or sens.get("gpu_temp", 0))
            self.hist["cpu"].append(live.get("cpu", 0)); self.hist["gpu"].append(live.get("gpu", 0)); self.hist["rpm1"].append(sens.get("cpu_fan_rpm", 0)); self.hist["rpm2"].append(sens.get("gpu_fan_rpm", 0))
        self.on_change()

    def loop(self):
        while not self.stop.is_set():
            try: self.fast()
            except Exception as e: self.err = str(e) or type(e).__name__
            self.wake.wait(1.0); self.wake.clear()

    def start(self): threading.Thread(target=self.loop, daemon=True, name="tui-data").start()

    # -- slow: (key, function) pairs of the open page
    def slow(self, reads):
        if self._slow_busy: self._slow_again = reads; return
        self._slow_busy = True

        def work(reads):
            while reads:
                for key, fn in reads:
                    try: self.x[key] = fn(); self.errs.pop(key, None)
                    except Exception as e: self.errs[key] = str(e) or type(e).__name__
                    self.on_change()
                reads, self._slow_again = self._slow_again, None
            self._slow_busy = False
        threading.Thread(target=work, args=(reads,), daemon=True, name="tui-slow").start()

    def slow_sync(self, reads):
        for key, fn in reads:
            try: self.x[key] = fn(); self.errs.pop(key, None)
            except Exception as e: self.errs[key] = str(e) or type(e).__name__


# ---- rows: one setting each ---------------------------------------------------------------------------------------------------------------
class Row:
    """kind: header | info | choice | toggle | slider | button.  get() -> current value (None = unknown); put(value) writes it (off the UI thread)."""
    def __init__(self, kind, label="", get=None, put=None, options=(), values=(), fmt=None, hint="", confirm=None, enabled=None, style=None, wrap=False, colors=None, live=False, text="Run", hide=None):
        self.kind, self.label, self.get, self.put, self.options, self.values, self.fmt, self.hint, self.confirm, self.enabled, self.style, self.wrap, self.colors, self.live, self.text, self.hide = kind, label, get, put, list(options), list(values), fmt, hint, confirm, enabled, style, wrap, colors, live, text, hide
        self.local = None; self.local_until = 0.0; self.due = 0.0

    @property
    def selectable(self): return self.kind not in ("header", "info") and (self.enabled is None or self.enabled()) and not (self.hide and self.hide())

    def value(self):
        if self.local is not None and time.monotonic() < self.local_until: return self.local
        self.local = None
        try: return self.get() if self.get else None
        except Exception: return None

    def index(self):
        v = self.value()
        if self.kind == "choice": return next((i for i, o in enumerate(self.options) if o[0] == v), None)
        if self.kind == "slider":
            if v is None: return None
            return min(range(len(self.values)), key=lambda i: abs(self.values[i] - v))
        return None

    def show(self, v):
        if v is None: return "…"
        return self.fmt(v) if self.fmt else str(v)


def choice(label, options, get, put, **kw): return Row("choice", label, get, put, options=[o if isinstance(o, tuple) else (o, str(o)) for o in options], **kw)
def toggle(label, get, put, **kw): return Row("toggle", label, get, put, **kw)
def slider(label, values, get, put, **kw): return Row("slider", label, get, put, values=values, **kw)
def button(label, text, put, **kw): return Row("button", label, None, put, text=text, **kw)
def info(label, get, **kw): return Row("info", label, get, **kw)
def header(text): return Row("header", text)


class Page:
    def __init__(self, name, rows, reads=(), extra=None):
        self.name, self.rows, self.reads, self.extra = name, rows, list(reads), extra
        self.sel = next((i for i, r in enumerate(rows) if r.selectable), 0); self.scroll = 0

    def move(self, d):
        n = len(self.rows); i = self.sel
        for _ in range(n):
            i = (i + d) % n if n else 0
            if self.rows[i].selectable: self.sel = i; return


# ---- helpers: colours of numbers --------------------------------------------------------------------------------------------------------
def temp_style(t):
    if t is None: return "dim"
    return "bad" if t >= 85 else "warn" if t >= 72 else "good"


def spark(g, values, width, lo, hi):
    vals = list(values)[-width:]; out = []
    for v in vals:
        f = 0.0 if hi <= lo else min(1.0, max(0.0, (v - lo) / (hi - lo))); out.append(g.spark[round(f * (len(g.spark) - 1))])
    return "".join(out).rjust(width)


# ---- the application ------------------------------------------------------------------------------------------------------------------
class App:
    def __init__(self, canvas, data=None, sync=False):
        self.cv, self.sync = canvas, sync; self.data = data or Data(); self.data.on_change = self.touch
        self.log = deque(maxlen=40); self.acting = None; self.repaint = False; self.quit = False; self.dirty = True; self.msg = ("", "dim", 0.0); self.modal = None; self.hits = []; self.tab_hits = []; self.page_i = 0; self.pending = []; self.kb = {}; self.rear = {}
        self.pages = self.build()

    def touch(self): self.dirty = True

    # -- feedback
    def say(self, text, style="good", secs=5.0):
        self.msg = (text, style, time.monotonic() + secs)
        if not text.startswith("…") and style in ("good", "bad"): self.log.append((time.strftime("%H:%M:%S"), text, style))      # what was done, newest last: the dashboard shows it
        self.touch()

    def run(self, label, fn, row=None, ok=None):
        """Do `fn` off the UI thread (hardware can take 100 ms+); show what happened; read everything again afterwards."""
        self.say("… " + label, "dim", 30); row = row or self.acting                  # the row being changed: its shown value is undone if the write fails

        def work():
            try: r = fn(); self.say(ok(r) if callable(ok) else (ok or label), "good")
            except Exception as e:
                if row: row.local = None
                self.say(f"{label}: {e}", "bad", 8)
            self.refresh()
        if self.sync: work()
        else: threading.Thread(target=work, daemon=True, name="tui-action").start()

    def refresh(self):
        self.data.wake.set()
        reads = self.page.reads
        if reads: self.data.slow_sync(reads) if self.sync else self.data.slow(reads)
        self.touch()

    @property
    def page(self): return self.pages[self.page_i]

    def goto(self, i):
        self.page_i = i % len(self.pages); self.hits = []; self.refresh()

    # -- building the pages
    def build(self):
        from . import audio, backend as hw, control, curves, ipc, keyboard, looks, profiles, smartprofiles
        d = self.data; x = d.x; app = self

        def live(k, default=None): return d.live.get(k, default)

        def power_put(m): return control.power(m, 30)
        mode_row = lambda label="Power mode": choice(label, [(m, MODE_LABEL[m]) for m in MODE_ORDER], lambda: d.mode, lambda m: app.run(f"Power mode: {MODE_LABEL[m]} (held 30 min)", lambda: power_put(m)), colors=MODE_STYLE,
                                                        hint="Eco / Quiet / Balanced / Performance / Turbo. Your pick is held for 30 minutes against Smart auto.")

        def auto_get(): return live("auto_mode") or d.settings.get("auto_mode")

        auto_row = lambda: choice("Automatic power mode", [("native", "Native"), ("smart", "Smart"), ("off", "Off")], auto_get, lambda v: app.run(f"Automatic power mode: {v}", lambda: ipc.call("set", key="auto_mode", value=v)),
                                  hint="Native: like PredatorSense (changes with the charger). Smart: also watches heat, load and games. Off: hands off. Needs the TrueSense service.", enabled=lambda: d.daemon)

        def fan_mode_get():
            if not d.daemon and not d.fanhw: return None
            if d.live.get("fan_off"): return "off"
            fm = d.fanhw.get("mode")
            if fm == "manual" and d.settings.get("smart_fan"): return "smart"
            if fm == "auto" and d.settings.get("smart_fan") and d.settings.get("fan_curve", "native") != "native": return "smart"
            return {"auto": "auto", "max": "max", "manual": "manual"}.get(fm)

        def fan_put(m):
            if m == "off": app.confirm("Stop BOTH fans? The service brings them back at CPU 70 C / GPU 65 C.", lambda: app.run("Fans off", lambda: control.fan("off", confirm=True)))
            else: app.run(f"Fans: {m}", lambda: control.fan(m))

        def duty_get(k): return lambda: d.fanhw.get("duty_register_pct", {}).get(k)

        def duty_put(which):
            def go(v):
                cpu = v if which == "cpu" else d.fanhw.get("duty_register_pct", {}).get("cpu", 40) or 40
                gpu = v if which == "gpu" else d.fanhw.get("duty_register_pct", {}).get("gpu", 40) or 40
                cpu, gpu = (cpu if cpu else 0), (gpu if gpu else 0)
                if v == 0: app.confirm(f"Stop the {which.upper()} fan? The service brings it back when the laptop gets warm.", lambda: app.run(f"{which.upper()} fan stopped", lambda: control.fan("manual", cpu, gpu, confirm=True)))
                else: app.run(f"{which.upper()} fan {v} %", lambda: control.fan("manual", max(cpu, 20) if cpu else 0, max(gpu, 20) if gpu else 0))
            return go

        duties = [0] + list(range(20, 101, 5))
        pct = lambda v: "OFF" if v == 0 else f"{v} %"

        def curve_get(): return (x.get("curve") or {}).get("name") or d.settings.get("fan_curve")

        def curve_put(n):
            return app.run(f"Fan curve: {n}", lambda: ipc.call("fan_curve", action="use", name=n) if n in curves.BUILTIN else ipc.call("fan_curve", action="use", name=n, data=curves.load(n)))

        def smart_profile_put(n):
            return app.run(f"Smart profile: {n}", lambda: ipc.call("smart_profile", action="use", name=n) if n in smartprofiles.BUILTIN else ipc.call("smart_profile", action="use", name=n, data=smartprofiles.load(n)))

        def hold_release(): app.run("Hold released: automatic power mode is back", lambda: ipc.call("hold", minutes=None))

        def charge_full():
            on = (x.get("full") or {}).get("active")
            return ipc.call("battery_full", action="cancel" if on else "start")

        def gpu_put(m): app.confirm(f"Switch the GPU mode to {m}? It takes effect after a reboot.", lambda: app.run(f"GPU mode: {m} (reboot to apply)", lambda: hw.set_gpu_mode(m)))

        def onoff(key, setter, label): return lambda v: app.run(f"{label}: {'on' if v else 'off'}", lambda: setter(bool(v)))

        # keyboard / light bar: remembered locally so a change of one setting keeps the others
        def kbs():
            s = {**{"name": "static", "color": "00aec7", "speed": 5, "brightness": 25, "direction": 0}, **{k: v for k, v in (x.get("kblast") or {}).items() if k in ("name", "color", "speed", "brightness", "direction")}}
            s.update(app.kb); return s

        def kb_put(**kw):
            app.kb.update(kw); s = kbs()
            if s["name"] == "off": return app.run("Keyboard effect: off", lambda: keyboard.set_effect("off"))
            return app.run(f"Keyboard: {s['name']}", lambda: keyboard.set_effect(s["name"], s["color"], int(s["speed"]), int(s["brightness"]), int(s["direction"])))

        def rear_s():
            s = {"mode": "static", "color": "00aec7", "speed": 3, "brightness": 100, "direction": 1}
            r = x.get("rear") or {}; s.update({k: r[k] for k in s if k in r}); s.update(app.rear); return s

        def rear_put(**kw):
            app.rear.update(kw); s = rear_s()
            return app.run(f"Light bar: {s['mode']}", lambda: hw.rear_set(s["mode"], s["color"], int(s["speed"]), int(s["brightness"]), int(s["direction"]), verify=False))

        color_opts = [(c, n) for n, c in COLORS]

        def color_get(getter):
            c = getter()
            return c if c in [o[0] for o in color_opts] else None

        def custom_color(setter, current):
            app.prompt("Colour as 6 hex digits (RRGGBB)", current(), lambda t: setter(color=t.strip().lstrip("#").lower()) if len(t.strip().lstrip("#")) == 6 and all(c in "0123456789abcdefABCDEF" for c in t.strip().lstrip("#")) else app.say("A colour is six hex digits, like ff8800", "bad"))

        # ---- pages
        dash = Page("Dashboard", [mode_row(), auto_row(), choice("Fans", [("smart", "Smart"), ("auto", "Auto"), ("max", "Max")], fan_mode_get, lambda m: app.run(f"Fans: {m}", lambda: control.fan(m)), hint="Smart: TrueSense's own curve. Auto: the firmware. Max: full speed. More on the Fans page.")],
                    extra=self.draw_dashboard)

        fans = Page("Fans", [
            header("Fan control"),
            choice("Mode", [("smart", "Smart curve"), ("auto", "Auto"), ("max", "Max"), ("manual", "Manual"), ("off", "Off")], fan_mode_get, fan_put, hint="Smart: TrueSense's curve. Auto: firmware. Max: 100 %. Manual: the sliders below. Off: both fans stop."),
            slider("CPU fan", duties, duty_get("cpu"), duty_put("cpu"), fmt=pct, hint="Left/Right in steps of 5. The bottom stop is OFF (asks first). Moving a slider switches to Manual.", live=True),
            slider("GPU fan", duties, duty_get("gpu"), duty_put("gpu"), fmt=pct, hint="Stop one fan and keep the other, for example when external fans cool the laptop.", live=True),
            info("CPU fan speed", lambda: f"{d.sens['cpu_fan_rpm']} rpm" if "cpu_fan_rpm" in d.sens else None),
            info("GPU fan speed", lambda: f"{d.sens['gpu_fan_rpm']} rpm" if "gpu_fan_rpm" in d.sens else None),
            header("Fan curve"),
            choice("Curve", [(n, n) for n in curves.names()], curve_get, curve_put, hint="Used by the Smart mode. native = the firmware's own curve.", enabled=lambda: d.daemon),
        ], reads=[("curve", lambda: ipc.call("fan_curve", action="get")), ("curvenames", lambda: curves.names())])

        power = Page("Power", [
            header("Mode"), mode_row(), auto_row(),
            choice("Smart profile", [(n, n) for n in smartprofiles.names()], lambda: (x.get("smartp") or {}).get("name") or d.settings.get("smart_profile"), smart_profile_put, hint="How Smart auto reacts: when to go up, how steady to stay. 'steady' for long games and external cooling.", enabled=lambda: d.daemon),
            info("Hold", lambda: f"your pick is held for {d.hold // 60} min {d.hold % 60} s" if d.hold else "none: automatic power mode is in charge"),
            button("Release the hold", "Release", lambda: hold_release(), hint="Let the automatic power mode take over now.", enabled=lambda: bool(d.hold)),
            header("Battery and charging"),
            toggle("Battery limit (80 %)", lambda: x.get("limit"), onoff("limit", hw.set_battery_limit, "Battery limit"), hint="Stops charging at 80 % to keep the battery healthy."),
            button("Charge to 100 % once", "Start / stop", lambda: app.run("Charging to 100 % once", charge_full), hint="Needs the charger and the 80 % limit on; it goes back to the limit by itself.", enabled=lambda: d.daemon and bool(x.get("limit"))),
            toggle("Battery calibration", lambda: x.get("calib"), onoff("calib", hw.set_calibration, "Battery calibration"), hint="A slow full charge and discharge cycle that re-learns the capacity."),
            choice("USB charging when off", [(0, "Off"), (10, "10 %"), (20, "20 %"), (30, "30 %")], lambda: x.get("usb"), lambda v: app.run(f"USB charging: {v}", lambda: hw.set_usb_charge(v)), hint="Charge phones from the USB port while the laptop is off, down to this battery level."),
            header("Graphics and display"),
            choice("GPU mode", [("optimus", "Hybrid"), ("nvidia", "NVIDIA only"), ("auto", "Auto")], lambda: x.get("gpu"), gpu_put, hint="Applies after a reboot."),
            toggle("Panel overdrive", lambda: x.get("od"), onoff("od", hw.set_overdrive, "Panel overdrive"), hint="Faster pixel response; can show slight overshoot."),
            toggle("Boot sound", lambda: x.get("boot"), onoff("boot", hw.set_boot_sound, "Boot sound")),
            toggle("Keyboard light timeout", lambda: x.get("kbt"), onoff("kbt", hw.set_kb_timeout, "Keyboard timeout"), hint="Turn the keyboard light off after a while without typing."),
        ], reads=[("limit", hw.get_battery_limit), ("calib", hw.get_calibration), ("usb", hw.get_usb_charge), ("gpu", hw.get_gpu_mode), ("od", hw.get_overdrive), ("boot", hw.get_boot_sound), ("kbt", hw.get_kb_timeout),
                  ("smartp", lambda: ipc.call("smart_profile", action="get")), ("full", lambda: ipc.call("battery_full", action="get"))])

        kb_effects = [(n, n.capitalize()) for n in keyboard.EFFECTS]
        rear_modes = [(n, n.capitalize()) for n in hw.REAR_MODES]
        light = Page("Lighting", [
            header("Keyboard"),
            choice("Effect", kb_effects, lambda: kbs()["name"], lambda v: kb_put(name=v), hint="The effects built into the keyboard. Per-key colours and your own images: truesense gui --page keys."),
            choice("Colour", color_opts, lambda: color_get(lambda: kbs()["color"]), lambda v: kb_put(color=v), hint="Not used by Wave, Neon and Off."),
            button("Custom colour", "Type hex…", lambda: custom_color(kb_put, lambda: kbs()["color"]), hint="Any colour as RRGGBB."),
            slider("Brightness", list(range(0, keyboard.MAX_BRIGHT + 1, 5)), lambda: kbs()["brightness"], lambda v: kb_put(brightness=v), fmt=lambda v: f"{v * 2} %", live=True),
            slider("Speed", list(range(keyboard.SPEED_MIN, keyboard.SPEED_MAX + 1)), lambda: kbs()["speed"], lambda v: kb_put(speed=v), live=True, hint="Slower is calmer."),
            choice("Direction", KB_DIRECTIONS, lambda: kbs()["direction"], lambda v: kb_put(direction=v)),
            choice("Saved lighting", [(n, n) for n in x.get("klight", [])] or [("", "none saved")], lambda: None, lambda n: app.run(f"Lighting: {n}", lambda: __import__("truesense.keyprofiles", fromlist=["load"]).load(n, None)), hint="Per-key lightings you saved in the window.", enabled=lambda: bool(x.get("klight"))),
            header("Light bar (back of the laptop)"),
            choice("Effect", rear_modes, lambda: rear_s()["mode"], lambda v: rear_put(mode=v)),
            choice("Colour", color_opts, lambda: color_get(lambda: rear_s()["color"]), lambda v: rear_put(color=v)),
            button("Custom colour", "Type hex…", lambda: custom_color(rear_put, lambda: rear_s()["color"])),
            slider("Brightness", list(range(0, 101, 10)), lambda: rear_s()["brightness"], lambda v: rear_put(brightness=v), fmt=lambda v: f"{v} %", live=True, hint="0 turns the bar off."),
            slider("Speed", [1, 2, 3, 4, 5], lambda: rear_s()["speed"], lambda v: rear_put(speed=v), live=True),
        ], reads=[("kblast", keyboard.last), ("rear", hw.rear_get), ("klight", lambda: __import__("truesense.keyprofiles", fromlist=["names"]).names())])

        audio_page = Page("Audio", [
            header("Sound"),
            info("Sound card", lambda: ("output present" if x["sound"].get("ok") else "no sound card" if x["sound"].get("ok") is None else "NO OUTPUT: " + x["sound"].get("problem", "?")) if x.get("sound") else None, style=lambda v: "good" if v == "output present" else "bad"),
            button("No sound at all?", "Fix sound", lambda: app.run("Repairing the sound output", audio.repair, ok=lambda r: "Sound output repaired: " + "; ".join(r["steps"]) if r["fixed"] and r["changed"] else "The sound output is there: nothing to repair" if r["fixed"] else "Could not repair: " + "; ".join(r["steps"])),
                   hint="Restarts WirePlumber so it probes the sound card again. Nothing is written to the sound chip."),
            choice("Play sound on", [("speakers", "Speakers"), ("headphones", "Headphones")], lambda: (x.get("out") or {}).get("active"), lambda v: app.run(f"Output: {v}", lambda: audio.set_output(v)), hint="This laptop plays on the speakers or the headphone jack, not both."),
            toggle("Noise filter on the speakers", lambda: x.get("spk"), lambda v: app.run("Speaker noise filter " + ("on" if v else "off"), lambda: audio.speaker_filter(bool(v))), hint="Off by default: it also gates music. For calls on the speakers."),
            header("Acer DTS sound"),
            toggle("DTS", lambda: x.get("dts"), lambda v: app.run("DTS " + ("on" if v else "off"), lambda: audio.dts_power(bool(v))), hint="Needs AcerUltraX."),
            choice("DTS sound mode", [(m, m) for m in DTS_MODES], lambda: x.get("dtsmode"), lambda m: app.run(f"DTS sound: {m} on every output", lambda: audio.dts_mode(m)), hint="Applies to every output: speakers, jack, USB and Bluetooth headsets. Plain is no processing."),
            button("A headset still sounds flat?", "Use DTS everywhere", lambda: app.run("DTS on every output", lambda: "; ".join(audio.dts_cover(force=True)) or "DTS is on for every output", ok=lambda r: "DTS: " + r), hint="New outputs get DTS by themselves; this does it again for all of them now."),
            info("Outputs", lambda: " · ".join(f"{o['label']}: {o['profile']}" for o in x["dtsdev"]) if x.get("dtsdev") else None, wrap=True),
            header("Microphone"),
            toggle("Mute every microphone", lambda: (x.get("mic") or {}).get("muted"), lambda v: app.run("Microphones " + ("muted" if v else "live"), lambda: audio.mic_set_mute(bool(v)))),
            slider("Microphone volume", list(range(0, 151, 5)), lambda: (x.get("mic") or {}).get("volume"), lambda v: app.run(f"Microphone volume: {v} %", lambda: audio.mic_volume(v)), fmt=lambda v: f"{v} %", live=True),
            choice("Headset mic noise reduction", [(m, m.capitalize()) for m in ("off", "conference", "personal")], lambda: (x.get("voice") or {}).get("headset"), lambda m: app.run(f"Headset mic: {m}", lambda: (audio.voice_set("headset", m), audio.apply_mic_policy(False))[0])),
            choice("Built-in mic noise reduction", [(m, m.capitalize()) for m in ("off", "conference", "personal")], lambda: (x.get("voice") or {}).get("array"), lambda m: app.run(f"Built-in mics: {m}", lambda: (audio.voice_set("array", m), audio.apply_mic_policy(False))[0])),
        ], reads=[("sound", audio.sound_state), ("out", audio.output_status), ("spk", audio.speaker_filter_on), ("mic", audio.mic_state), ("voice", audio.voice_modes),
                  ("dtsdev", audio.dts_devices), ("dts", lambda: "DTS:            on" in audio.dts_status()), ("dtsmode", lambda: next((o["profile"] for o in audio.dts_devices() if o["current"]), None))])

        def profile_rows():
            rows = [header("Profiles: a whole setup in one go (power, fans, lighting, audio)")]
            for n in profiles.names(): rows.append(button(n, "Apply", (lambda n=n: app.run(f"Profile {n}", lambda: profiles.apply(n), ok=lambda r: f"Profile {n} applied")), hint=profile_hint(n)))
            if len(rows) == 1: rows.append(info("", lambda: "no profiles found (truesense profile list)"))
            rows.append(header("Looks: the lighting at boot, as saved in the window"))
            for n in looks.names(): rows.append(button(n, "Apply", (lambda n=n: app.run(f"Look {n}", lambda: looks.apply(looks.load(n)))), hint="Applies the saved keyboard and light-bar look."))
            return rows

        def profile_hint(n):
            try: return "  ·  ".join(profiles.describe(profiles.load(n)))
            except Exception: return ""
        prof = Page("Profiles", profile_rows())

        def doctor_rows():
            from . import doctor
            rows = [header("Health check"), button("Run the check", "Run doctor", lambda: app.run("Checking everything", lambda: x.__setitem__("doctor", run_doctor(doctor)), ok="Check finished"), hint="Looks at the module, the service, power managers, audio and more. Read-only.")]
            rows.append(info("", lambda: "not run yet: press Enter on the button above", hide=lambda: bool(x.get("doctor"))))
            for title, _fn in doctor.CHECKS:
                rows.append(info(title, (lambda t=title: doctor_line(t)), style=(lambda v, t=title: doctor_style(t)), wrap=True, hide=lambda: not x.get("doctor")))
            return rows

        def doctor_line(t):
            r = next((r for r in x.get("doctor", []) if r["check"] == t), None)
            if not r: return "not checked yet" if not x.get("doctor") else "…"
            return f"{r['status']}  {r['detail']}" + (f"   -> {r['fix']}" if r["fix"] and r["status"] in ("WARN", "FAIL") else "")

        def doctor_style(t):
            r = next((r for r in x.get("doctor", []) if r["check"] == t), None)
            return {"OK": "good", "WARN": "warn", "FAIL": "bad"}.get(r["status"], "text") if r else "dim"

        system = Page("System", [
            header("TrueSense"),
            info("Version", lambda: __version__),
            info("Kernel module", lambda: x.get("modver")),
            info("Service", lambda: "running" if d.daemon else "not running (direct hardware access)", style=lambda v: "good" if v == "running" else "warn"),
            info("Window", lambda: "truesense gui"),
            button("Open the window", "Open", lambda: app.run("Opening the window", lambda: __import__("subprocess").Popen([sys.executable, "-m", "truesense", "gui"], start_new_session=True, stdout=-3, stderr=-3) and None), hint="The PyQt window (needs python-pyqt6)."),
            *doctor_rows(),
        ], reads=[("modver", hw.version)])

        return [dash, fans, power, light, audio_page, prof, system]

    # -- dialogs
    def confirm(self, text, then): self.modal = {"kind": "confirm", "text": text, "then": then}; self.touch()
    def prompt(self, title, initial, then): self.modal = {"kind": "prompt", "text": title, "buf": initial or "", "then": then, "fresh": True}; self.touch()
    def help(self): self.modal = {"kind": "help"}; self.touch()

    # -- drawing
    def draw(self):
        cv = self.cv; cv.clear(); self.hits = []; self.tab_hits = []; w, h = cv.w, cv.h
        if w < 44 or h < 12:
            cv.put(0, 0, "TrueSense needs", "warn"); cv.put(1, 0, "a 44x12 terminal", "warn"); cv.put(2, 0, f"(this is {w}x{h})", "dim"); return
        self.draw_header(); body_y, body_h = 2, h - 4
        self.draw_page(self.page, body_y, body_h, w)
        self.draw_footer()
        if self.modal: self.draw_modal()

    def draw_header(self):
        cv, w = self.cv, self.cv.w; g = cv.glyphs
        cv.put(0, 0, " TrueSense ", "title"); cv.put(0, 11, f"{__version__}", "dim")
        d = self.data; badge = (g.dot + " service", "good") if d.daemon else (g.dot + " direct", "warn")
        right = []
        if d.batt[0] is not None: right.append((f"{g.bolt if d.ac else ''}{d.batt[0]} %".strip() if d.ac else f"{d.batt[0]} %", "good" if d.ac or d.batt[0] > 30 else "bad"))
        right.append(badge)
        x = w - 1
        for text, st in reversed(right): x -= len(text); cv.put(0, x, text, st); x -= 2
        # tabs
        x = 1; names = [p.name for p in self.pages]; wide = w >= 82
        for i, n in enumerate(names):
            label = f" {i + 1} {n} " if wide else (f" {i + 1} {n} " if i == self.page_i else f" {i + 1} ")
            if x + len(label) >= w - 1: break
            cv.put(1, x, label, "tabsel" if i == self.page_i else "tab"); self.tab_hits.append((x, x + len(label), i)); x += len(label) + 1

    def draw_footer(self):
        cv, w, h = self.cv, self.cv.w, self.cv.h
        text, st, until = self.msg
        row = self.page.rows[self.page.sel] if self.page.rows else None
        def line(t, st): cv.put(h - 2, 1, t if len(t) <= w - 2 else t[: w - 3] + "…", st)
        if text and time.monotonic() < until: line(text, st)
        elif self.data.err: line("No device: " + self.data.err, "warn")
        elif row and row.hint: line(row.hint, "dim")
        keys = [("↑↓", "select"), ("←→", "change"), ("Enter", "apply"), ("Tab", "page"), ("?", "help"), ("q", "quit")] if cv.glyphs.unicode else [("Up/Dn", "select"), ("L/R", "change"), ("Enter", "apply"), ("Tab", "page"), ("?", "help"), ("q", "quit")]
        x = 1
        for k, t in keys:
            if x + len(k) + len(t) + 3 > w: break
            cv.put(h - 1, x, k, "key"); cv.put(h - 1, x + len(k) + 1, t, "dim"); x += len(k) + len(t) + 4

    def draw_page(self, page, y0, hh, w):
        cv = self.cv
        rows_h = hh
        lines = []
        for i, r in enumerate(page.rows):
            if r.hide and r.hide(): continue
            if r.kind == "header" and lines: lines.append((None, 0, ""))
            if r.kind == "info" and r.wrap:
                lw = min(34, w // 2, max([len(q.label) for q in page.rows if q.kind != "header"] or [12]) + 2); text = self.info_text(r); parts = textwrap.wrap(text, max(10, w - lw - 8)) or [""]
                for k, p in enumerate(parts): lines.append((i, k, p))
            else: lines.append((i, 0, ""))
        sel_line = next((n for n, (i, k, _) in enumerate(lines) if i == page.sel and k == 0), 0)
        if sel_line < page.scroll: page.scroll = sel_line
        if sel_line >= page.scroll + rows_h: page.scroll = sel_line - rows_h + 1
        page.scroll = max(0, min(page.scroll, max(0, len(lines) - rows_h)))
        lw = min(34, w // 2, max([len(r.label) for r in page.rows if r.kind != "header"] or [12]) + 2)
        y = y0
        for n in range(page.scroll, min(len(lines), page.scroll + rows_h)):
            i, k, part = lines[n]
            if i is not None: self.draw_row(page.rows[i], i == page.sel, y, w, lw, k, part)
            y += 1
        if len(lines) > rows_h:                                                      # scroll indicator
            top = page.scroll > 0; bot = page.scroll + rows_h < len(lines)
            if top: cv.put(y0, w - 2, cv.glyphs.up, "dim")
            if bot: cv.put(y0 + rows_h - 1, w - 2, "▼" if cv.glyphs.unicode else "v", "dim")
        if page.extra:
            ey = y0 + min(len(lines), rows_h) + 1
            if y0 + hh - ey >= 5: page.extra(ey, y0 + hh - ey, w)

    def info_text(self, r):
        v = r.value(); return r.show(v) if v is not None else "…"

    def draw_row(self, r, sel, y, w, lw, k=0, part=""):
        cv = self.cv; g = cv.glyphs
        if r.kind == "header":
            cv.put(y, 1, r.label, "head"); cv.put(y, 2 + len(r.label), " " + g.h * max(0, w - len(r.label) - 5), "dim"); return
        dis = r.kind not in ("info",) and not r.selectable
        if k == 0:
            if sel: cv.put(y, 0, "▸" if g.unicode else ">", "accent")
            cv.put(y, 2, r.label[: lw - 1], "dim" if dis else ("accent" if sel else "text"))
        cx = 2 + lw + 1
        if r.kind == "info":
            v = r.value(); st = (r.style(v) if r.style and v is not None else "text")
            text = part if r.wrap else r.show(v); cv.put(y, cx, text[: max(0, w - cx - 1)], st if v is not None else "dim"); return
        if dis: cv.put(y, cx, "(not available)" if r.kind != "button" else f"[ {r.text} ]", "dim"); return
        v = r.value(); self.hits.append((y, 0, w, r, "row", 0))
        if r.kind == "toggle":
            on = bool(v); s = "[ ON  ]" if v else "[ OFF ]"; st = "on" if on else "off"
            if v is None: s, st = "[  …  ]", "dim"
            cv.put(y, cx, s, st); self.hits.append((y, cx, cx + len(s), r, "toggle", 0))
        elif r.kind == "button":
            s = f"[ {r.text} ]"; cv.put(y, cx, s, "pill" if sel else "accent"); self.hits.append((y, cx, cx + len(s), r, "button", 0))
        elif r.kind == "choice":
            idx = r.index(); labels = [o[1] for o in r.options]; total = sum(len(s) + 2 for s in labels) + len(labels) - 1
            if total <= w - cx - 2 and len(labels) > 0:
                x = cx
                for i, (val, lab) in enumerate(r.options):
                    cur = i == idx; st = (r.colors.get(val, "accent") if r.colors else "accent")
                    cv.put(y, x, f" {lab} ", (st + "_sel" if r.colors and cur else "pill") if cur else "dim"); self.hits.append((y, x, x + len(lab) + 2, r, "opt", i)); x += len(lab) + 3
            else:
                lab = labels[idx] if idx is not None and idx < len(labels) else "…"; s = f"‹ {lab} ›" if g.unicode else f"< {lab} >"
                cv.put(y, cx, s, "pill" if sel else "accent"); self.hits.append((y, cx, cx + 2, r, "prev", 0)); self.hits.append((y, cx + len(s) - 2, cx + len(s), r, "next", 0))
        elif r.kind == "slider":
            vals = r.values; idx = r.index(); bw = max(8, min(36, w - cx - 14)); text = r.show(vals[idx]) if idx is not None else "…"
            pos = 0 if idx is None else round(idx / max(1, len(vals) - 1) * (bw - 1))
            for i in range(bw): cv.put(y, cx + i, g.rail if i <= pos and idx is not None else g.thin, "bar" if i <= pos and idx is not None else "bar_off")
            if idx is not None: cv.put(y, cx + pos, g.knob, "accent" if sel else "text")
            cv.put(y, cx + bw + 2, text, "text" if idx is not None else "dim"); self.hits.append((y, cx, cx + bw, r, "bar", bw))

    # -- the dashboard's read-only panels
    def draw_dashboard(self, y, h, w):
        """Panels in priority order: temperatures, fans, load, power. Two columns when wide; whatever does not fit is left out, never squeezed."""
        d, cv = self.data, self.cv; g = cv.glyphs; live = d.live; s = d.sens
        two = w >= 96; colw = (w - 3) // 2 if two else w - 2

        def gauge(yy, xx, ww, label, v, vmax, unit, style, hist=None, lo=0, hi=100, width=6):
            cv.put(yy, xx, f"{label:<4}", "dim")
            val = (f"{v}{unit}" if v is not None else "…").rjust(width); sp = hist is not None and ww >= 40
            bw = max(6, ww - 5 - len(val) - 2 - (12 if sp else 0)); f = 0 if not v else min(1.0, max(0.0, v / vmax)); n = round(f * bw)
            cv.put(yy, xx + 5, g.full * n, style); cv.put(yy, xx + 5 + n, g.empty * (bw - n), "bar_off"); cv.put(yy, xx + 6 + bw, val, style if v is not None else "dim")
            if sp: cv.put(yy, xx + 7 + bw + len(val), spark(g, hist, 10, lo, hi), style)

        tc, tg, ts = live.get("tcpu") or s.get("cpu_temp"), live.get("tgpu") or s.get("gpu_temp"), live.get("chassis") or s.get("sys_temp")
        duty = d.fanhw.get("duty_register_pct", {}); pct, _st = d.batt; mode = d.mode

        def temps(y, x, w):
            cv.box(y, x, 6, w, "Temperatures")
            gauge(y + 1, x + 2, w - 4, "CPU", tc, 100, " °C", temp_style(tc), d.hist["tcpu"], 30, 100); gauge(y + 2, x + 2, w - 4, "GPU", tg, 100, " °C", temp_style(tg), d.hist["tgpu"], 30, 100)
            gauge(y + 3, x + 2, w - 4, "SYS", ts, 100, " °C", temp_style(ts))
            act = live.get("activity"); cv.put(y + 4, x + 2, "Now  ", "dim"); cv.put(y + 4, x + 8, (live.get("mode_reason") or act or ("needs the TrueSense service" if not d.daemon else "…"))[: max(0, w - 12)], "accent" if act else "dim")
            return 6

        def fans(y, x, w):
            cv.box(y, x, 5, w, "Fans")
            gauge(y + 1, x + 2, w - 4, "CPU", s.get("cpu_fan_rpm"), 6500, " rpm", "bar", d.hist["rpm1"], 0, 6500, 8); gauge(y + 2, x + 2, w - 4, "GPU", s.get("gpu_fan_rpm"), 6500, " rpm", "bar", d.hist["rpm2"], 0, 6500, 8)
            cv.put(y + 3, x + 2, f"duty CPU {duty.get('cpu', '…')} %  GPU {duty.get('gpu', '…')} %  ·  {d.fanhw.get('mode', '…')}"[: w - 4], "dim")
            return 5

        def load(y, x, w):
            cv.box(y, x, 5, w, "Load")
            gauge(y + 1, x + 2, w - 4, "CPU", live.get("cpu"), 100, " %", "bar", d.hist["cpu"], 0, 100); gauge(y + 2, x + 2, w - 4, "GPU", live.get("gpu"), 100, " %", "bar", d.hist["gpu"], 0, 100)
            cv.put(y + 3, x + 2, "GPU power " + (f"{live['gpuw']} W" if live.get("gpuw") is not None else "…"), "dim")
            return 5

        def power(y, x, w):
            cv.box(y, x, 5, w, "Power")
            cv.put(y + 1, x + 2, "Mode", "dim"); cv.put(y + 1, x + 9, MODE_LABEL.get(mode, str(mode or "…")), MODE_STYLE.get(mode, "text"))
            src = "on charger" if d.ac else "on battery" if d.ac is not None else "…"; cv.put(y + 1, x + 10 + len(MODE_LABEL.get(mode, "…")) + 2, src, "good" if d.ac else "warn")
            cv.put(y + 2, x + 2, "Auto", "dim"); cv.put(y + 2, x + 9, f"{live.get('auto_mode', '…')} · profile {live.get('smart_profile', '…')}"[: w - 12])
            if pct is not None: gauge(y + 3, x + 2, w - 4, "Batt", pct, 100, " %", "good" if pct > 30 else "bad")
            return 5

        def activity(y, x, w, room):
            n = max(1, min(room - 2, 12, len(self.log))); cv.box(y, x, n + 2, w, "What you did")
            if not self.log: cv.put(y + 1, x + 2, "Nothing yet: your changes are listed here.", "dim")
            for i, (t, text, st) in enumerate(list(self.log)[-n:]): cv.put(y + 1 + i, x + 2, t, "dim"); cv.put(y + 1 + i, x + 11, text[: w - 14], "good" if st == "good" else "bad")
            return n + 2

        panels = [(6, temps), (5, fans), (5, load), (5, power)]
        if two:
            yy = y
            for a, b in ((panels[0], panels[1]), (panels[2], panels[3])):
                ph = max(a[0], b[0])
                if yy + ph > y + h: break
                a[1](yy, 1, colw); b[1](yy, 1 + colw + 1, colw); yy += ph
            if y + h - yy >= 5: activity(yy, 1, w - 2, y + h - yy)
        else:
            yy = y
            for ph, fn in panels:
                if yy + ph > y + h: break
                fn(yy, 1, colw); yy += ph
            if y + h - yy >= 5: activity(yy, 1, w - 2, y + h - yy)

    def draw_modal(self):
        cv = self.cv; m = self.modal; w, h = cv.w, cv.h
        if m["kind"] == "help":
            lines = ["Move            ↑ ↓  or  j k          Change a value   ← →  or  h l", "Apply / press   Enter or Space        Pages            Tab, Shift-Tab, 1-7", "Quick power     m  next power mode    Refresh          r", "Mouse           click tabs, rows, options and sliders; wheel scrolls", "Quit            q  or  Esc", "", "Every change is applied at once and shown at the bottom.", "The window has the rest: per-key colours, images, undervolting."]
            if not cv.glyphs.unicode: lines = [l.replace("↑ ↓", "Up Dn").replace("← →", "L R") for l in lines]
            bw = min(w - 4, max(len(l) for l in lines) + 4); bh = len(lines) + 4; y = max(1, (h - bh) // 2); x = (w - bw) // 2
            for i in range(bh): cv.fill(y + i, x, bw, "text")
            cv.box(y, x, bh, bw, "Keys", "accent"); [cv.put(y + 2 + i, x + 2, l[: bw - 4]) for i, l in enumerate(lines)]
        else:
            text = m["text"]; body = textwrap.wrap(text, min(60, w - 10)) or [""]; bw = min(w - 4, max(len(b) for b in body) + 6, 66); bh = len(body) + 5; y = max(1, (h - bh) // 2); x = (w - bw) // 2
            for i in range(bh): cv.fill(y + i, x, bw, "text")
            cv.box(y, x, bh, bw, "Confirm" if m["kind"] == "confirm" else "Type", "warn" if m["kind"] == "confirm" else "accent")
            for i, b in enumerate(body): cv.put(y + 1 + i, x + 3, b)
            if m["kind"] == "confirm": cv.put(y + bh - 2, x + 3, "y", "key"); cv.put(y + bh - 2, x + 5, "yes   ", "dim"); cv.put(y + bh - 2, x + 11, "n", "key"); cv.put(y + bh - 2, x + 13, "no", "dim")
            else: cv.put(y + bh - 2, x + 3, "> " + m["buf"] + "_", "accent")

    # -- input
    def key(self, k):
        """k: a curses key code or a one-character string. Returns True when the screen changed."""
        self.touch(); k = ord(k) if isinstance(k, str) else k
        K = _keys()
        if self.modal: return self.modal_key(k, K)
        pg = self.page
        if k in (ord("q"), ord("Q"), 27): self.quit = True
        elif k == ord("?"): self.help()
        elif k == 9: self.goto(self.page_i + 1)
        elif k == K["BTAB"]: self.goto(self.page_i - 1)
        elif ord("1") <= k <= ord("0") + len(self.pages): self.goto(k - ord("1"))
        elif k in (K["UP"], ord("k")): pg.move(-1)
        elif k in (K["DOWN"], ord("j")): pg.move(1)
        elif k == K["HOME"]: pg.sel = 0; pg.move(1) if not pg.rows[0].selectable else None
        elif k == K["END"]: pg.sel = len(pg.rows) - 1; pg.move(-1) if not pg.rows[-1].selectable else None
        elif k in (K["PPAGE"],): [pg.move(-1) for _ in range(5)]
        elif k in (K["NPAGE"],): [pg.move(1) for _ in range(5)]
        elif k in (K["LEFT"], ord("h")): self.adjust(-1)
        elif k in (K["RIGHT"], ord("l")): self.adjust(1)
        elif k in (10, 13, K["ENTER"], ord(" ")): self.activate()
        elif k == 12: self.repaint = True                                                              # Ctrl-L: redraw the whole screen (after another program wrote over it)
        elif k == ord("r"): self.say("Reading everything again…", "dim", 2); self.refresh()
        elif k == ord("m"): self.cycle_mode()
        return True

    def cycle_mode(self):
        row = self.pages[0].rows[0]; m = row.value()                                     # the value just chosen, not the last reading, so two quick presses go two steps
        if m not in MODE_ORDER: return
        nxt = MODE_ORDER[(MODE_ORDER.index(m) + 1) % len(MODE_ORDER)]; row.local, row.local_until = nxt, time.monotonic() + 3; row.put(nxt)

    def modal_key(self, k, K):
        m = self.modal
        if m["kind"] == "help": self.modal = None
        elif m["kind"] == "confirm":
            if k in (ord("y"), ord("Y"), 10, 13): self.modal = None; m["then"]()
            elif k in (ord("n"), ord("N"), 27, ord("q")): self.modal = None; self.say("Cancelled", "dim", 2)
        else:
            if k == 27: self.modal = None
            elif k in (10, 13): self.modal = None; m["then"](m["buf"])
            elif k in (K["BACKSPACE"], 127, 8): m["buf"] = m["buf"][:-1]; m.pop("fresh", None)
            elif 32 <= k < 127:
                m["buf"] = (chr(k) if m.pop("fresh", False) else m["buf"] + chr(k))[:24]                  # the first key replaces the suggested text; Backspace edits it instead
        return True

    def adjust(self, d):
        pg = self.page; r = pg.rows[pg.sel] if pg.rows else None
        if not r or not r.selectable: return
        if r.kind == "choice" and r.options:
            i = r.index(); i = 0 if i is None else (i + d) % len(r.options); self.choose(r, i)
        elif r.kind == "toggle": self.set_value(r, not bool(r.value()))
        elif r.kind == "slider" and r.values:
            i = r.index(); i = 0 if i is None else max(0, min(len(r.values) - 1, i + d)); self.slide(r, i)

    def activate(self):
        pg = self.page; r = pg.rows[pg.sel] if pg.rows else None
        if not r or not r.selectable: return
        if r.kind == "button": r.put()
        elif r.kind == "toggle": self.set_value(r, not bool(r.value()))
        elif r.kind == "choice" and r.options: self.adjust(1)
        elif r.kind == "slider" and r.values:
            i = r.index(); v = r.values[i if i is not None else 0]; r.local, r.local_until, r.due = v, time.monotonic() + 3, 0; r.put(v)          # Enter applies now (and confirms a fan stop)

    def choose(self, r, i):
        v = r.options[i][0]; r.local, r.local_until = v, time.monotonic() + 3; self.acting = r; r.put(v)

    def set_value(self, r, v):
        r.local, r.local_until = v, time.monotonic() + 3; self.acting = r; r.put(v)

    def slide(self, r, i):
        v = r.values[i]; r.local, r.local_until = v, time.monotonic() + 3
        if v == 0 and r.live: r.due = 0; self.say("Press Enter to stop this fan", "warn", 4); return            # a stop is always asked for
        if r.live:                                                                                             # one write when the dragging pauses
            r.due = time.monotonic() + 0.35
            if r not in self.pending: self.pending.append(r)
        else: self.acting = r; r.put(v)

    def tick(self):
        """Called every cycle: writes sliders whose arrow-key burst has paused."""
        if not self.pending: return
        now = time.monotonic(); due = [r for r in self.pending if r.due and now >= r.due]
        for r in due:
            self.pending.remove(r)
            if r.due and r.local is not None: r.due = 0; self.acting = r; r.put(r.local)

    def mouse(self, x, y, wheel=0, click=True):
        self.touch()
        if self.modal:
            if click and self.modal["kind"] == "help": self.modal = None
            return
        if wheel: self.page.move(wheel); return
        if not click: return
        if y == 1:
            for x0, x1, i in self.tab_hits:
                if x0 <= x < x1: self.goto(i); return
            return
        pg = self.page
        for (hy, x0, x1, r, zone, extra) in reversed(self.hits):
            if hy != y or not (x0 <= x < x1): continue
            idx = pg.rows.index(r)
            if r.selectable: pg.sel = idx
            if zone == "row": continue
            if zone == "toggle": self.set_value(r, not bool(r.value()))
            elif zone == "button": r.put()
            elif zone == "opt": self.choose(r, extra)
            elif zone in ("prev", "next"): self.adjust(-1 if zone == "prev" else 1)
            elif zone == "bar": i = round((x - x0) / max(1, extra - 1) * (len(r.values) - 1)); self.slide(r, max(0, min(len(r.values) - 1, i)))
            return


def run_doctor(doctor):
    def one(item):
        title, fn = item
        try: st, detail, fix = fn()
        except Exception as e: st, detail, fix = doctor.WARN, f"check crashed: {type(e).__name__}: {e}", ""
        return {"check": title, "status": st, "detail": detail, "fix": fix}
    from concurrent.futures import ThreadPoolExecutor
    with ThreadPoolExecutor(max_workers=8) as ex: return list(ex.map(one, doctor.CHECKS))


def _keys():
    import curses
    return {"UP": curses.KEY_UP, "DOWN": curses.KEY_DOWN, "LEFT": curses.KEY_LEFT, "RIGHT": curses.KEY_RIGHT, "ENTER": curses.KEY_ENTER, "BTAB": curses.KEY_BTAB, "HOME": curses.KEY_HOME,
            "END": curses.KEY_END, "PPAGE": curses.KEY_PPAGE, "NPAGE": curses.KEY_NPAGE, "BACKSPACE": curses.KEY_BACKSPACE}


# ---- the terminal loop ----------------------------------------------------------------------------------------------------------------------
def _loop(scr, page):
    import curses
    try: curses.curs_set(0)
    except curses.error: pass                                                     # a terminal without a hidden cursor (vt100): the cursor just stays visible
    scr.keypad(True); scr.timeout(100)
    try: curses.mousemask(curses.ALL_MOUSE_EVENTS)
    except curses.error: pass
    cv = CursesCanvas(scr); app = App(cv); app.data.start()
    if page is not None: app.page_i = page
    app.refresh()
    wheel_up = getattr(curses, "BUTTON4_PRESSED", 0x10000); wheel_down = getattr(curses, "BUTTON5_PRESSED", 0x200000)
    try:
        while not app.quit:
            if app.dirty:
                app.dirty = False; cv.resize(); app.draw()
                if app.repaint: app.repaint = False; scr.clearok(True)
                scr.noutrefresh(); curses.doupdate()
            k = scr.getch()
            app.tick()
            if k == -1: continue
            if k == curses.KEY_RESIZE: curses.update_lines_cols(); cv.resize(); app.touch()
            elif k == curses.KEY_MOUSE:
                try: _id, mx, my, _z, b = curses.getmouse()
                except curses.error: continue
                if b & wheel_up: app.mouse(mx, my, wheel=-1)
                elif b & wheel_down: app.mouse(mx, my, wheel=1)
                elif b & (curses.BUTTON1_CLICKED | curses.BUTTON1_PRESSED): app.mouse(mx, my)
            else: app.key(k)
    finally: app.data.stop.set(); app.data.wake.set()


def main(page=None):
    import curses
    import locale
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        print("truesense tui needs a terminal", file=sys.stderr); return 1
    locale.setlocale(locale.LC_ALL, ""); os.environ.setdefault("ESCDELAY", "25")
    try: curses.wrapper(_loop, page)
    except KeyboardInterrupt: pass
    except HwError as e: print(f"truesense tui: {e}", file=sys.stderr); return 1
    return 0
