"""Optional window for TrueSense (GTK4 + libadwaita). A thin layer over the same functions the CLI uses; nothing else depends on it.

Run: truesense gui        Needs python-gobject, gtk4 and libadwaita (all common on desktops).
"""
import threading
from . import actions, audio, backend as hw, config, ipc, keyboard, keymap, profiles
from .device import HwError

try:
    import gi
    gi.require_version("Gtk", "4.0"); gi.require_version("Adw", "1")
    from gi.repository import Adw, GLib, Gtk, Gdk
except (ImportError, ValueError) as _e:
    Gtk = None; _IMPORT_ERROR = _e

FAN_MODES = ["Smart curve", "Firmware auto", "Max", "Manual"]
KB_EFFECTS = ["static", "breathing", "wave", "snake", "ripple", "neon", "raindrop", "lightning", "spot", "star", "fireball", "snow", "heartbeat"]
VOICE = ["off", "conference", "personal"]
GESTURES = [("tap", "Tap"), ("double", "Double tap"), ("triple", "Triple tap"), ("long", "Long press"), ("fn_esc", "Fn + Esc")]


def rgba(hexcolor):
    c = Gdk.RGBA(); c.parse("#" + hexcolor.lstrip("#")); return c

def to_hex(c): return "%02x%02x%02x" % (round(c.red * 255), round(c.green * 255), round(c.blue * 255))


class Window(Adw.ApplicationWindow if Gtk else object):
    def __init__(self, app):
        super().__init__(application=app, title="TrueSense", default_width=720, default_height=640)
        self.updating = False; self.cfg = config.load()
        self.toasts = Adw.ToastOverlay(); self.stack = Adw.ViewStack()
        switcher = Adw.ViewSwitcher(policy=Adw.ViewSwitcherPolicy.WIDE, stack=self.stack)
        header = Adw.HeaderBar(); header.set_title_widget(switcher)
        view = Adw.ToolbarView(); view.add_top_bar(header); view.set_content(self.stack); self.toasts.set_child(view); self.set_content(self.toasts)
        self.build_overview(); self.build_lighting(); self.build_battery(); self.build_audio(); self.build_keys()
        self.refresh(); GLib.timeout_add_seconds(2, self.tick)

    # ---- plumbing ------------------------------------------------------------------------------------------------------------------
    def toast(self, text): self.toasts.add_toast(Adw.Toast.new(text))

    def do(self, fn, ok=None, slow=False):
        """Run a backend call; show its result or its error. `slow` runs in a thread (the output switch takes ~1 s)."""
        def work():
            try: r = fn(); GLib.idle_add(lambda: (self.toast(ok if isinstance(ok, str) else (ok(r) if ok else str(r or "done"))), self.refresh(), False)[-1])
            except Exception as e: GLib.idle_add(lambda: (self.toast(f"Error: {e}"), self.refresh(), False)[-1])
        threading.Thread(target=work, daemon=True).start() if slow else work()

    def live(self, key, fn):
        """Apply a colour while it is being picked: one write at a time, the newest value always wins."""
        busy = self.__dict__.setdefault("_busy", {})
        if busy.get(key): busy[key] = "again"; return
        busy[key] = True
        def work():
            try: fn()
            except Exception as e: GLib.idle_add(lambda: (self.toast(f"Error: {e}"), False)[-1])
            again = busy.get(key) == "again"; busy[key] = None
            if again: GLib.idle_add(lambda: (self.live(key, fn), False)[-1])
        threading.Thread(target=work, daemon=True).start()

    def page(self, name, title, icon):
        pg = Adw.PreferencesPage(); self.stack.add_titled_with_icon(pg, name, title, icon); return pg

    @staticmethod
    def group(page, title, desc=None):
        g = Adw.PreferencesGroup(title=title)
        if desc: g.set_description(desc)
        page.add(g); return g

    def switch(self, group, title, subtitle, on_change):
        r = Adw.SwitchRow(title=title, subtitle=subtitle)
        r.connect("notify::active", lambda w, _p: None if self.updating else on_change(w.get_active())); group.add(r); return r

    def combo(self, group, title, items, on_change, subtitle=""):
        r = Adw.ComboRow(title=title, subtitle=subtitle, model=Gtk.StringList.new(items))
        r.connect("notify::selected", lambda w, _p: None if self.updating else on_change(w.get_selected())); group.add(r); return r

    def spin(self, group, title, lo, hi, step, on_change, subtitle=""):
        r = Adw.SpinRow.new_with_range(lo, hi, step); r.set_title(title); r.set_subtitle(subtitle)
        r.connect("notify::value", lambda w, _p: None if self.updating else on_change(int(w.get_value()))); group.add(r); return r

    def button_row(self, group, title, subtitle, label, cb):
        r = Adw.ActionRow(title=title, subtitle=subtitle); b = Gtk.Button(label=label, valign=Gtk.Align.CENTER); b.connect("clicked", lambda *_: cb()); r.add_suffix(b); group.add(r); return r

    def color_row(self, group, title, initial):
        r = Adw.ActionRow(title=title); b = Gtk.ColorDialogButton(dialog=Gtk.ColorDialog(), valign=Gtk.Align.CENTER); b.set_rgba(rgba(initial)); r.add_suffix(b); group.add(r); return b

    # ---- Overview ----------------------------------------------------------------------------------------------------------------------
    def build_overview(self):
        pg = self.page("overview", "Overview", "computer-symbolic")
        g = self.group(pg, "Live"); self.live = Adw.ActionRow(title="Temperatures and fans"); g.add(self.live)
        self.daemon_row = Adw.ActionRow(title="Daemon"); g.add(self.daemon_row)
        g = self.group(pg, "Profile", "A saved bundle: power mode, fans, lighting, battery, display, audio.")
        self.profile_row = self.combo(g, "Profile", profiles.names() or ["(none)"], lambda i: None)
        self.button_row(g, "Apply the selected profile", "", "Apply", lambda: self.do(lambda: profiles.apply(profiles.names()[self.profile_row.get_selected()]), lambda r: r, slow=True))
        g = self.group(pg, "Power mode")
        self.modes = hw.supported_modes() if self.safe(hw.supported_modes) else list(hw.MODES)
        self.power_row = self.combo(g, "Mode", self.modes, lambda i: self.do(lambda: (hw.set_mode(self.modes[i]), ipc.call("hold", minutes=30))[0], lambda r: f"Power mode: {r}"), "A manual choice is held for 30 minutes against the automatic profile")
        self.auto_row = self.switch(g, "Automatic profile", "Switches mode by AC / battery / load", lambda v: self.do(lambda: ipc.call("set", key="auto_profile", value=v), "Saved"))
        g = self.group(pg, "Fans")
        self.fan_row = self.combo(g, "Control", FAN_MODES, self.on_fan_mode)
        self.fan_pct = self.spin(g, "Manual speed (%)", 20, 100, 5, lambda v: self.fan_mode_apply(3) if self.fan_row.get_selected() == 3 else None)

    @staticmethod
    def safe(fn):
        try: fn(); return True
        except Exception: return False

    def on_fan_mode(self, i): self.fan_mode_apply(i)

    def fan_mode_apply(self, i):
        def go():
            if i == 0: ipc.call("set", key="smart_fan", value=True); return "Fans: smart curve"
            ipc.call("set", key="smart_fan", value=False); import time; time.sleep(1.2)
            if i == 1: hw.fan_auto(); return "Fans: firmware auto"
            if i == 2: hw.fan_max(); return "Fans: MAX"
            p = int(self.fan_pct.get_value()); hw.fan_manual(p, p); return f"Fans: manual {p} %"
        self.do(go, lambda r: r, slow=True)

    # ---- Lighting ----------------------------------------------------------------------------------------------------------------------
    def build_lighting(self):
        pg = self.page("lighting", "Lighting", "weather-clear-night-symbolic")
        g = self.group(pg, "Rear light bar")
        self.rear_on = self.switch(g, "On", "", lambda v: self.do(lambda: actions.run("rear:on" if v else "rear:off"), lambda r: r))
        self.rear_fx = self.combo(g, "Effect", list(hw.REAR_MODES), lambda i: None)
        self.rear_color = self.color_row(g, "Colour", "00aec7")
        self.rear_speed = self.spin(g, "Speed", 1, 5, 1, lambda v: None); self.rear_bright = self.spin(g, "Brightness", 0, 100, 10, lambda v: None)
        self.button_row(g, "Apply to the rear light", "Static uses the three part colours below", "Apply", lambda: self.do(lambda: self.rear_apply(), lambda r: "Rear light updated"))
        self.zone_colors = [self.color_row(g, f"Static: {n} part", "00aec7") for n in ("left", "middle", "right")]
        g = self.group(pg, "Keyboard", "Fn+F11 / Fn+F12 also change the brightness (done by the keyboard itself).")
        self.kb_on = self.switch(g, "On", "", lambda v: self.do(lambda: actions.run("keyboard:on" if v else "keyboard:off"), lambda r: r))
        self.kb_bright = self.spin(g, "Brightness (0 to 50)", 0, 50, 5, lambda v: self.do(lambda: keyboard.set_brightness(v), "Brightness set"))
        self.kb_fx = self.combo(g, "Built-in effect", KB_EFFECTS, lambda i: None); self.kb_color = self.color_row(g, "Colour", "00aec7")
        self.kb_color.connect("notify::rgba", lambda *_: None if self.updating else self.live("kb", lambda: keyboard.set_color(to_hex(self.kb_color.get_rgba()))))
        self.button_row(g, "Apply the effect", "neon needs high brightness", "Apply", lambda: self.do(lambda: keyboard.set_effect(KB_EFFECTS[self.kb_fx.get_selected()], to_hex(self.kb_color.get_rgba()), 5, max(int(self.kb_bright.get_value()), 50 if self.kb_fx.get_selected() == 5 else 1)), "Keyboard effect applied"))
        self.button_row(g, "Change colour only", "Keeps the current effect, changes its colour", "Apply", lambda: self.do(lambda: keyboard.set_color(to_hex(self.kb_color.get_rgba())), "Colour changed"))
        names = ["all", "wasd", "arrows", "fkeys", "numrow", "letters", "numpad", "esc", "space", "enter", "predator", "lshift"]
        self.paint_group = self.combo(g, "Per-key: keys", names, lambda i: None, "Pick keys, then paint them with the colour above; other keys stay as they are")
        self.paint_names = names
        self.button_row(g, "Paint the chosen keys", "", "Paint", lambda: self.do(lambda: keyboard.paint({self.paint_names[self.paint_group.get_selected()]: to_hex(self.kb_color.get_rgba())}), "Keys painted", slow=True))
        self.button_row(g, "Per-key colours", "Fill every key with the colour above, or a rainbow", "Fill", lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.canvas(keymap.hexcolor(to_hex(self.kb_color.get_rgba())))), None), "Keyboard filled", slow=True))
        self.button_row(g, "", "", "Rainbow", lambda: self.do(lambda: keyboard.per_key(keymap.to_list(keymap.rainbow()), None), "Rainbow", slow=True))
        self.button_row(g, "Back to the stock cyan", "", "Restore", lambda: self.do(lambda: keyboard.restore_default(None), "Restored", slow=True))
        self.kb_timeout = self.switch(g, "Backlight off after 30 s", "", lambda v: self.do(lambda: hw.set_kb_timeout(v), "Saved"))
        self.build_startup(pg)

    def rear_apply(self):
        mode = list(hw.REAR_MODES)[self.rear_fx.get_selected()]; speed = int(self.rear_speed.get_value()); br = int(self.rear_bright.get_value())
        if mode == "static": return hw.rear_set("static", direction=1, speed=speed, brightness=br, zones=[to_hex(b.get_rgba()) for b in self.zone_colors])
        return hw.rear_set(mode, to_hex(self.rear_color.get_rgba()), speed, br)

    def build_startup(self, pg):
        g = self.group(pg, "At login", "Lighting is saved automatically on every change; this chooses what comes back when you log in.")
        opts = ["last", "default", "off", "none"]; s = self.cfg["startup"]
        for key, title in (("keyboard", "Keyboard"), ("rear", "Rear light")):
            r = self.combo(g, title, ["Last used", "Stock look", "Off", "Leave as the firmware has it"], lambda i, k=key: self.do(lambda: config.write_user({"startup": {k: opts[i]}}), f"{k} at login: {opts[i]}"))
            r.set_selected(opts.index(s[key]) if s[key] in opts else 0)

    # ---- Battery and display -----------------------------------------------------------------------------------------------------------
    def build_battery(self):
        pg = self.page("battery", "Battery & Display", "battery-good-symbolic")
        g = self.group(pg, "Battery")
        self.limit = self.switch(g, "80 % charge limit", "Keeps the battery healthier on AC", lambda v: self.do(lambda: hw.set_battery_limit(v), lambda r: f"80 % limit: {'on' if r else 'off'}"))
        self.calib = self.switch(g, "Calibration", "Needs AC power and takes hours; switch off to cancel", lambda v: self.do(lambda: hw.set_calibration(v), lambda r: f"Calibration: {'on' if r else 'off'}"))
        self.usb = self.combo(g, "USB charging while off", ["Off", "Stop at 10 %", "Stop at 20 %", "Stop at 30 %"], lambda i: self.do(lambda: hw.set_usb_charge(hw.USB_LEVELS[i]), lambda r: f"USB charge level: {r}"))
        g = self.group(pg, "Display and boot")
        self.od = self.switch(g, "LCD overdrive", "Faster pixel response", lambda v: self.do(lambda: hw.set_overdrive(v), lambda r: f"Overdrive: {'on' if r else 'off'}"))
        self.boot = self.switch(g, "Boot animation and sound", "", lambda v: self.do(lambda: hw.set_boot_sound(v), lambda r: f"Boot sound: {'on' if r else 'off'}"))
        g = self.group(pg, "GPU", "Takes effect after a reboot.")
        self.gpu = self.combo(g, "Mode", ["Optimus (hybrid)", "NVIDIA only", "Automatic"], lambda i: self.do(lambda: hw.set_gpu_mode(list(hw.GPU_MODES)[i]), lambda r: f"GPU mode: {r} (reboot to apply)"))

    # ---- Audio -------------------------------------------------------------------------------------------------------------------------
    def build_audio(self):
        pg = self.page("audio", "Audio", "audio-speakers-symbolic")
        g = self.group(pg, "Output", "Speakers and the headphone jack cannot play at once on this laptop, so this switches between them.")
        self.out_row = Adw.ActionRow(title="Current output"); g.add(self.out_row)
        self.button_row(g, "Laptop speakers", "", "Use", lambda: self.do(lambda: audio.set_output("speakers"), "Output: speakers", slow=True))
        self.button_row(g, "Headphones (3.5 mm jack)", "", "Use", lambda: self.do(lambda: audio.set_output("headphones"), "Output: headphones", slow=True))
        g = self.group(pg, "Microphone")
        self.mute = self.switch(g, "Mute every microphone", "", lambda v: self.do(lambda: audio.mic_set_mute(v), lambda r: "Microphones muted" if r else "Microphones live"))
        self.jack_row = Adw.ActionRow(title="3.5 mm jack"); g.add(self.jack_row)
        self.boost = self.spin(g, "Headset mic boost (0 to 3)", 0, 3, 1, lambda v: self.do(lambda: audio.headset_boost(v), lambda r: f"Boost: {r}"))
        g = self.group(pg, "Purified Voice (AI noise reduction)", "Conference is gentle; Personal is strict and can clip very soft words.")
        self.v_head = self.combo(g, "Headset microphone", VOICE, lambda i: self.do(lambda: (audio.voice_set("headset", VOICE[i]), audio.apply_mic_policy(False))[0], lambda r: f"Headset mic: {r}", slow=True))
        self.v_arr = self.combo(g, "Built-in microphone array", VOICE, lambda i: self.do(lambda: (audio.voice_set("array", VOICE[i]), audio.apply_mic_policy(False))[0], lambda r: f"Mic array: {r}", slow=True))
        self.v_note = self.switch(g, "Show a notification when the microphone mode changes", "", lambda v: self.do(lambda: config.write_user({"audio": {"notify": v}}), "Saved"))

    # ---- Keys --------------------------------------------------------------------------------------------------------------------------
    def build_keys(self):
        pg = self.page("keys", "PREDATOR key", "input-keyboard-symbolic")
        g = self.group(pg, "What each press does", "Type any action (see `truesense keys actions`), e.g. power:turbo, profile:gaming, exec:firefox. Restart the key service to apply: systemctl --user restart truesense-keys")
        self.key_rows = {}
        for gesture, label in GESTURES:
            r = Adw.EntryRow(title=label); r.set_text(self.cfg["keys"][gesture]); r.set_show_apply_button(True)
            r.connect("apply", lambda row, gst=gesture: self.save_key(gst, row)); g.add(r); self.key_rows[gesture] = r
        g = self.group(pg, "Try an action now")
        self.try_row = Adw.EntryRow(title="Action"); self.try_row.set_text("power-next"); self.try_row.set_show_apply_button(True)
        self.try_row.connect("apply", lambda row: self.do(lambda: actions.run(row.get_text()) or "ok", lambda r: r)); g.add(self.try_row)

    def save_key(self, gesture, row):
        err = actions.validate(row.get_text())
        if err: self.toast(err); return
        self.do(lambda: config.write_user({"keys": {gesture: row.get_text()}}), f"{gesture}: saved")

    # ---- refresh (reads real state; never writes) --------------------------------------------------------------------------------------
    def tick(self): self.refresh(); return True

    def refresh(self):
        self.updating = True
        try:
            try: s = hw.sensors(); self.live.set_subtitle(f"CPU {s['cpu_temp']} C   GPU {s['gpu_temp']} C   board {s['sys_temp']} C   fans {s['cpu_fan_rpm']} / {s['gpu_fan_rpm']} rpm")
            except HwError as e: self.live.set_subtitle(str(e))
            try:
                st = ipc.call("state"); self.daemon_row.set_subtitle(f"running, activity: {st['live'].get('activity', '?')}"); self.auto_row.set_active(st["settings"]["auto_profile"])
                self.fan_row.set_selected(0 if st["settings"]["smart_fan"] else self.fan_row.get_selected() or 1)
            except HwError as e: self.daemon_row.set_subtitle(str(e))
            for fn in (self.r_power, self.r_fan, self.r_light, self.r_battery, self.r_audio): self.safely(fn)
        finally: self.updating = False

    def safely(self, fn):
        try: fn()
        except Exception: pass

    def r_power(self):
        m = hw.get_mode()
        if m in self.modes: self.power_row.set_selected(self.modes.index(m))
    def r_fan(self):
        f = hw.fan_state()
        if self.fan_row.get_selected() != 0: self.fan_row.set_selected({"auto": 1, "max": 2, "manual": 3}.get(f["mode"], 1))
    def r_light(self):
        r = hw.rear_get(); self.rear_on.set_active(r["brightness"] > 0)
        if r["mode"] in hw.REAR_MODES: self.rear_fx.set_selected(list(hw.REAR_MODES).index(r["mode"]))
        self.rear_color.set_rgba(rgba(r["color"])); self.rear_speed.set_value(r["speed"]); self.rear_bright.set_value(r["brightness"])
        self.kb_bright.set_value(keyboard.get_brightness()); self.kb_timeout.set_active(bool(hw.get_kb_timeout()))
    def r_battery(self):
        self.limit.set_active(hw.get_battery_limit()); self.calib.set_active(hw.get_calibration()); self.usb.set_selected(hw.USB_LEVELS.index(hw.get_usb_charge()))
        self.od.set_active(bool(hw.get_overdrive())); self.boot.set_active(hw.get_boot_sound()); self.gpu.set_selected(hw.GPU_MODES[hw.get_gpu_mode()] - 1)
    def r_audio(self):
        o = audio.output_status(); self.out_row.set_subtitle(o["active"] + (" (headphone jack in use)" if o["jack_plugged"] else ""))
        j = audio.jack_status(); self.jack_row.set_subtitle(("headphones plugged" if j["headphones"] else "nothing plugged") + (", headset microphone detected" if j["headset_mic"] else ", no headset microphone"))
        self.mute.set_active(audio.mic_state()["muted"]); b = audio.headset_boost()
        if b is not None: self.boost.set_value(b)
        v = audio.voice_modes(); self.v_head.set_selected(VOICE.index(v["headset"]) if v["headset"] in VOICE else 0); self.v_arr.set_selected(VOICE.index(v["array"]) if v["array"] in VOICE else 0)
        self.v_note.set_active(config.load()["audio"]["notify"])


class App(Adw.Application if Gtk else object):
    def __init__(self): super().__init__(application_id="io.github.truesense"); self.connect("activate", lambda a: Window(a).present())


def main():
    if Gtk is None:
        print(f"The window needs python-gobject, GTK 4 and libadwaita ({_IMPORT_ERROR}). Everything works from the command line without it.")
        return 1
    return App().run([])
