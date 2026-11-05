# SPDX-License-Identifier: MIT
import os
import time
import unittest
from truesense import userstate

try:
    os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")
    from PyQt6.QtWidgets import QApplication
    from truesense import gui
except ImportError:
    gui = None


@unittest.skipIf(gui is None, "PyQt6 not installed")
class TestWindowLogic(unittest.TestCase):
    @classmethod
    def setUpClass(cls): cls.app = QApplication.instance() or QApplication([])

    def test_newest_value_always_wins(self):
        """While a write is in flight, later requests are queued and only the newest runs (the old code re-ran the OLDEST value)."""
        w = gui.Window(); seen = []
        def slow(v):
            def f(): time.sleep(0.15); seen.append(v)
            return f
        for v in (5, 10, 15, 20, 25): w.live("k", slow(v))
        time.sleep(0.8)
        self.assertEqual(seen[0], 5); self.assertEqual(seen[-1], 25, f"the last dragged value must be applied last, got {seen}")
        self.assertLessEqual(len(seen), 3)
        w.close()

    def test_refresh_does_not_move_a_dragged_slider(self):
        w = gui.Window(); w.kb_level.setSliderDown(True); w.kb_level.setValue(40)
        w._set(w.kb_level, 10); self.assertEqual(w.kb_level.value(), 40)
        w.kb_level.setSliderDown(False); w._set(w.kb_level, 10); self.assertEqual(w.kb_level.value(), 10)
        w.close()

    def test_speed_is_disabled_for_static_and_colour_for_wave(self):
        w = gui.Window()
        w.sync_enabled({"mode": "effect", "name": "static"}); self.assertFalse(w.kb_speed.isEnabled()); self.assertTrue(w.kb_color.isEnabled())
        w.sync_enabled({"mode": "effect", "name": "wave"}); self.assertTrue(w.kb_speed.isEnabled()); self.assertFalse(w.kb_color.isEnabled())
        w.sync_enabled({"mode": "perkey"}); self.assertFalse(w.kb_speed.isEnabled())
        w.close()

    def test_choosing_a_rear_effect_while_off_turns_it_on_not_off(self):
        from truesense import backend as hw
        w = gui.Window(); time.sleep(0.4); self.app.processEvents()
        hw.rear_set("static", "ff0000", 3, 0, 1)                                 # the bar is off (brightness 0)
        w.loading = True; w.rear_bright.setValue(0); w.loading = False
        w.rear_fx.btns[1].click(); time.sleep(0.6); self.app.processEvents()      # pick Breathing
        r = hw.rear_get(); self.assertEqual(r["mode"], "breathing"); self.assertGreater(r["brightness"], 0)
        w.close()

    def test_builtin_curves_are_read_only_until_copied(self):
        from truesense import curves
        w = gui.Window(); w.load_curve_in_editor("truesense")
        self.assertTrue(w.editor.locked); self.assertFalse(w.c_pre.isEnabled()); self.assertFalse(w.curve_save_btn.isEnabled()); self.assertFalse(w.del_btn.isEnabled())
        before = [list(p) for p in w.editor.curve["cpu"]]; w.editor.drag = 2
        from PyQt6.QtCore import QPointF, Qt
        from PyQt6.QtGui import QMouseEvent
        w.editor.mouseMoveEvent(QMouseEvent(QMouseEvent.Type.MouseMove, QPointF(200, 60), Qt.MouseButton.NoButton, Qt.MouseButton.LeftButton, Qt.KeyboardModifier.NoModifier))
        self.assertEqual(w.editor.curve["cpu"], before)
        w.curve_list.setCurrentIndex(w.curve_list.findText("truesense")); w.curve_copy()
        self.assertIn("truesense-copy", curves.names()); self.assertFalse(w.editor.locked); self.assertTrue(w.c_pre.isEnabled())
        curves.delete("truesense-copy"); w.close()

    def test_builtin_smart_profiles_are_read_only_until_copied(self):
        from truesense import smartprofiles as sp
        w = gui.Window(); w.load_smart_in_editor("truesense")
        self.assertFalse(w.sm_modes["ac_busy"].isEnabled()); self.assertFalse(w.sm_save.isEnabled()); self.assertFalse(w.sm_copy.isHidden())
        w.sm_list.setCurrentIndex(w.sm_list.findText("truesense")); w.smart_copy()
        self.assertIn("truesense-copy", sp.names()); self.assertTrue(w.sm_modes["ac_busy"].isEnabled())
        w.sm_modes["ac_busy"].setCurrentIndex(sp.MODES.index("turbo")); w.sm_name.setText("truesense-copy"); w.smart_save()
        self.assertEqual(sp.load("truesense-copy")["ac_busy"], "turbo"); sp.delete("truesense-copy"); w.close()

    def test_action_editor_round_trips_every_kind_of_action(self):
        import tempfile
        from truesense import actions
        ed = gui.ActionEditor()
        script = tempfile.mkdtemp() + "/my script.sh"
        with open(script, "w") as fh: fh.write("#!/bin/sh\n")
        specs = ["none", "power-next", "power:turbo", "power-cycle:eco,balanced", "fan:smart", "fan:60", "fan:max", "auto:smart", "auto-next", "profile:gaming", "output-toggle", "mic-toggle",
                 "touchpad-toggle", "keyboard:toggle", "rear-effect-next", "notify:hello there", "exec:notify-send hi", "exec:sh " + gui.shlex.quote(script)]
        for s in specs:
            ed.set_spec(s); self.assertEqual(ed.value(), s, f"{s!r} came back as {ed.value()!r}"); self.assertIsNone(actions.validate(ed.value()), s)
        self.assertEqual(ed.script_command(script), "sh " + gui.shlex.quote(script))                       # not executable: run through sh
        os.chmod(script, 0o755); self.assertEqual(ed.script_command(script), gui.shlex.quote(script))        # executable: run directly
        ed.set_spec("exec:sh " + gui.shlex.quote(script)); self.assertEqual(ed.kind(), "script")             # a chosen file is shown as a file, not as a raw command

    def test_key_profiles_builtin_read_only_and_applied_to_the_config(self):
        from truesense import keybinds, config
        w = gui.Window(); w.load_keyprofile("gaming")
        self.assertFalse(w.kp_save.isEnabled()); self.assertFalse(w.kp_edit["tap"].type.isEnabled())
        w.kp_list.setCurrentIndex(w.kp_list.findText("gaming")); w.kp_copy_do(); n = w.kp_list.currentText(); self.assertIn("gaming", n); self.assertTrue(w.kp_edit["tap"].type.isEnabled())
        w.kp_edit["tap"].set_spec("power:turbo"); w.kp_name.setText(n); w.kp_save_do(); self.assertEqual(keybinds.load(n)["tap"], "power:turbo")
        keybinds.use(n); k = config.load()["keys"]; self.assertEqual((k["tap"], k["profile"]), ("power:turbo", n))
        keybinds.use("default"); self.assertEqual(config.load()["keys"]["tap"], "power-next"); keybinds.delete(n); w.close()

    def test_per_key_picture_covers_every_key_and_edits_are_undoable(self):
        from truesense import keymap
        w = gui.Window(); b = w.board
        every = {c for n, cs in keymap.NAMES.items() if n not in ("enter", "win") for c in cs}
        self.assertEqual({c for k in b.keys for c in k[1]}, every, "every key must have a place on the picture")
        b.select_group("wasd"); self.assertEqual(len(b.sel), 4); before = list(b.cells)
        w.set_colour("ff0000"); self.assertTrue(all(b.cells[c] == "ff0000" for c in b.sel)); self.assertEqual(b.cells[keymap.NAMES["esc"][0]], before[keymap.NAMES["esc"][0]])
        w.undo(); self.assertEqual(b.cells, before); w.redo(); self.assertEqual(b.cells[list(b.sel)[0]], "ff0000")
        b.select_group("arrows"); w.g1.set_hex("000000"); w.g2.set_hex("ffffff"); w.apply_gradient(); vals = sorted(int(b.cells[c][:2], 16) for c in b.sel); self.assertLess(vals[0], vals[-1])
        w.set_colour("zzzzzz"); self.assertEqual(w.hex_edit.text(), w.cur_hex)                                  # a bad colour is ignored
        b.select_group("all"); b.select_group("invert"); self.assertEqual(b.sel, set()); w.close()

    def test_manual_fans_one_slider_when_linked_two_when_not(self):
        from truesense import device
        w = gui.Window(); w.show(); w.fan.btns[3].click(); self.app.processEvents(); time.sleep(0.4); self.app.processEvents()
        self.assertTrue(w.fan_link.isChecked()); self.assertTrue(w.fan_one.isVisible()); self.assertFalse(w.fan_two.isVisible())
        w.fan_pct.setValue(70); time.sleep(0.5); self.app.processEvents(); self.assertEqual(device.SIM.fan_duty, [70, 70])
        w.fan_link.setChecked(False); self.app.processEvents()
        self.assertTrue(w.fan_two.isVisible()); self.assertFalse(w.fan_one.isVisible()); self.assertEqual((w.fan_cpu.value(), w.fan_gpu.value()), (70, 70))
        w.fan_cpu.setValue(50); time.sleep(0.5); self.app.processEvents(); self.assertEqual(device.SIM.fan_duty[0], 50)
        w.fan_gpu.setValue(90); time.sleep(0.5); self.app.processEvents(); self.assertEqual(device.SIM.fan_duty, [50, 90])
        w.fan_link.setChecked(True); self.app.processEvents(); self.assertEqual(w.fan_pct.value(), 90); time.sleep(0.5); self.app.processEvents(); self.assertEqual(device.SIM.fan_duty, [90, 90])
        w.fan.btns[1].click(); self.app.processEvents(); self.assertFalse(w.fan_box.isVisible()); w.close()

    def _wait(self, cond, timeout=4.0):
        end = time.time() + timeout
        while time.time() < end:
            self.app.processEvents()
            if cond(): return True
            time.sleep(0.05)
        return cond()

    def test_per_key_lives_in_lighting_and_hands_animations_to_the_key_service(self):
        from truesense import perkeyfx
        w = gui.Window(); w.go("keys"); self.assertEqual(w.pages.currentIndex(), gui.PAGES["lighting"]); self.assertEqual(w.light_stack.currentIndex(), 1)
        w.board.select_group("wasd"); w.fx_cb.setCurrentIndex(w.fx_cb.findData("wave")); w.fx_speed.setValue(4); w.fx_push()
        self.assertTrue(self._wait(lambda: perkeyfx.get().get("mode") == "wave"), "the animation settings never reached the state")
        fx = perkeyfx.get(); self.assertEqual((fx["mode"], fx["speed"], len(fx["keys"]), len(fx["base"])), ("wave", 4, 4, 128))
        w.fx_cb.setCurrentIndex(0); w.fx_push(); self.assertTrue(self._wait(lambda: perkeyfx.get().get("mode") == "off")); w.close()

    def test_back_button_goes_back_through_pages_and_subtabs(self):
        from PyQt6.QtCore import QEvent, QPointF, Qt
        from PyQt6.QtGui import QMouseEvent
        w = gui.Window(); w.show(); w.go("audio"); w.go("smart"); w.go("keys")
        self.assertEqual(w.pages.currentIndex(), gui.PAGES["lighting"]); self.assertEqual(w.light_stack.currentIndex(), 1)
        w.nav_back(); self.assertEqual(w.pages.currentIndex(), gui.PAGES["lighting"]); self.assertEqual(w.light_stack.currentIndex(), 0)       # out of the sub-tab first
        w.nav_back(); self.assertEqual(w.pages.currentIndex(), gui.PAGES["smart"]); w.nav_back(); self.assertEqual(w.pages.currentIndex(), gui.PAGES["audio"])
        w.nav_forward(); self.assertEqual(w.pages.currentIndex(), gui.PAGES["smart"])
        w.go("battery"); w.nav_forward(); self.assertEqual(w.pages.currentIndex(), gui.PAGES["battery"], "a new page clears the forward history")
        def press(btn): return QMouseEvent(QEvent.Type.MouseButtonPress, QPointF(5, 5), QPointF(5, 5), btn, btn, Qt.KeyboardModifier.NoModifier)
        w.activateWindow(); self.app.processEvents()
        if w.isActiveWindow(): self.assertTrue(w.nav_filter.eventFilter(w, press(Qt.MouseButton.BackButton))); self.assertEqual(w.pages.currentIndex(), gui.PAGES["smart"])
        self.assertFalse(w.nav_filter.eventFilter(w, press(Qt.MouseButton.LeftButton)))
        for _ in range(40): w.nav_back()
        self.assertEqual(w.pages.currentIndex(), 0); w.close()

    def test_picture_shows_a_preview_and_says_what_happened(self):
        import tempfile
        from PyQt6.QtWidgets import QFileDialog
        try: from PIL import Image
        except ImportError: self.skipTest("Pillow not installed")
        w = gui.Window(); w.go("keys"); orig = QFileDialog.getOpenFileName
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "red.png"); Image.new("RGB", (44, 12), (255, 0, 0)).save(p)
            QFileDialog.getOpenFileName = staticmethod(lambda *a, **k: (p, ""))
            try:
                w.image_apply(); self.assertGreater(w.img_prev.height(), 0); self.assertIn("red.png", w.img_msg.text()); self.assertEqual(w.board.cells[5], "ff0000"); self.assertFalse(w.img_prev.pixmap().isNull())
                QFileDialog.getOpenFileName = staticmethod(lambda *a, **k: (os.path.join(d, "nope.png"), "")); w.image_apply(); self.assertIn("NOT painted", w.img_msg.text()); self.assertEqual(w.img_prev.height(), 0)
            finally: QFileDialog.getOpenFileName = orig
        w.close()

    def test_fan_sliders_are_20_to_100_and_the_off_button_asks(self):
        w = gui.Window(); self.assertTrue(hasattr(w, "fan_off_btn"), "the Turn fans off button is there")
        for sl in (w.fan_pct, w.fan_cpu, w.fan_gpu): self.assertEqual((sl.minimum(), sl.maximum()), (20, 100))
        self.assertEqual(w.manual_duty()[0] >= 20, True); w.fan_link.setChecked(False); w.loading = True; w.fan_cpu.setValue(20); w.fan_gpu.setValue(65); w.loading = False; self.assertEqual(w.manual_duty(), (20, 65)); w.fan_link.setChecked(True); time.sleep(0.4)   # (loading above: no live fan write from the slider moves; this saves the setting again)
        from PyQt6.QtWidgets import QMessageBox
        import truesense.control as control_mod
        calls = []; orig_exec, orig_fan = QMessageBox.exec, control_mod.fan
        control_mod.fan = lambda *a, **k: calls.append((a, k))
        try:
            QMessageBox.exec = lambda self_: QMessageBox.StandardButton.Cancel; w.fan_off_ask(); time.sleep(0.2); self.assertFalse(calls, "cancel does nothing")
            w.fan_asked_at = -9; QMessageBox.exec = lambda self_: QMessageBox.StandardButton.Apply; w.fan_off_ask(); self.assertTrue(self._wait(lambda: calls)); self.assertEqual((calls[0][0], calls[0][1].get("confirm")), (("off",), True))
        finally: QMessageBox.exec = orig_exec; control_mod.fan = orig_fan
        w.close()

    def test_every_page_fits_the_smallest_window_and_esc_closes(self):
        w = gui.Window(); w.show(); self.assertEqual(w.minimumSize(), w.maximumSize(), "fixed size (floats on Hyprland) is the default"); w.apply_size_mode(False); self.assertFalse(w.minimumSize() == w.maximumSize(), "the window can be resized"); self.assertLessEqual(w.minimumWidth(), 400)
        for name, i in gui.PAGES.items():
            need = w.pages.widget(i).widget().minimumSizeHint().width()
            self.assertLessEqual(need, w.minimumWidth() - 28, f"the {name} page needs {need} px, more than the smallest window gives it (it would be cut off)")
        w.resize(w.minimumWidth(), 560); self.app.processEvents(); self.assertEqual(w.width(), w.minimumWidth())
        w.apply_size_mode(True); self.assertEqual(w.minimumSize(), w.maximumSize()); userstate.set('gui_fixed', True)
        from PyQt6.QtGui import QShortcut, QKeySequence
        esc = [sc for sc in w.findChildren(QShortcut) if sc.key() == QKeySequence("Escape")]; self.assertEqual(len(esc), 1); esc[0].activated.emit(); self.assertFalse(w.isVisible(), "Esc closes the app"); w.close()

    def test_cli_features_that_were_missing_now_have_controls(self):
        from truesense import profiles, keyprofiles, config, startup
        w = gui.Window(); w.show()
        # profile bundles
        names = [w.prof_cb.itemData(i) for i in range(w.prof_cb.count())]; self.assertTrue(names, "shipped profiles are listed"); self.assertIn("gaming", names)
        w.prof_name.setText("gui-bundle"); w.prof_save(); self.assertTrue(self._wait(lambda: "gui-bundle" in profiles.names()), "saving a profile from the window works")
        w.prof_reload("gui-bundle"); self.assertEqual(w.prof_cb.currentData(), "gui-bundle"); self.assertEqual(profiles.source("gui-bundle"), "yours")
        w.prof_cb.setCurrentIndex(w.prof_cb.findData("gaming")); shown = []; w.say_report = shown.append; w.prof_show(); self.assertTrue(shown and "gaming" in shown[0])
        # per-key sets
        w.ks_nm.setText("gui-set"); w.ks_save(); self.assertTrue(self._wait(lambda: "gui-set" in keyprofiles.names())); w.ks_reload("gui-set"); w.ks_cb.setCurrentIndex(w.ks_cb.findText("gui-set")); w.ks_apply()
        # at login
        w.su_reload(); w.su_kb.setCurrentIndex(w.su_kb.findData("default")); w.su_pick("keyboard", w.su_kb); self.assertTrue(self._wait(lambda: config.load()["startup"]["keyboard"] == "default"))
        w.su_prof.setCurrentIndex(w.su_prof.findData("gui-bundle")); w.su_pick("profile", w.su_prof); self.assertTrue(self._wait(lambda: config.load()["startup"]["profile"] == "gui-bundle"))
        w.su_prof.setCurrentIndex(w.su_prof.findData("none")); w.su_pick("profile", w.su_prof); self.assertTrue(self._wait(lambda: config.load()["startup"]["profile"] == ""))
        with self.assertRaises(Exception): startup.set_option("keyboard", "look:does-not-exist")
        # the small ones exist
        for name in ("kb_oper", "spk", "micvol", "tp_sw", "pm_sw"): self.assertTrue(hasattr(w, name), name)
        self.assertEqual((w.micvol.minimum(), w.micvol.maximum()), (0, 150)); w.show_config(); w.check_config(); w.close()

    def test_profiles_page_explains_what_a_profile_changes_and_home_has_no_duplicate_picker(self):
        from truesense import profiles
        w = gui.Window(); w.show(); self.assertIn("profiles", gui.PAGES)
        self.assertFalse(hasattr(w, "hp_cb"), "Home has no second profile picker: the Profiles page is the one place")
        self.assertTrue(any(b.text() == "Profiles" for b in w.findChildren(gui.QPushButton)), "Home still has the Profiles tile")
        self.assertGreater(w.prof_cb.count(), 0)
        w.prof_cb.setCurrentIndex(w.prof_cb.findData("gaming")); w.prof_changed(); txt = w.prof_info.text(); self.assertIn("Maximum performance", txt); self.assertIn("Turbo", txt); self.assertIn("Fans: smart curve", txt); self.assertIn("overdrive on", txt)
        d = profiles.load("battery-saver"); lines = profiles.describe(d); self.assertTrue(any("Eco" in l for l in lines) and any("80 % limit on" in l for l in lines) and any("Rear light: off" in l for l in lines), lines)
        self.assertEqual(profiles.describe({}), ["(nothing)"])
        w.go("profiles"); self.assertEqual(w.pages.currentIndex(), gui.PAGES["profiles"]); w.close()

    def test_rows_never_stretch_tabs_are_as_tall_as_their_content_and_the_title_follows_the_page(self):
        from PyQt6.QtWidgets import QSizePolicy
        w = gui.Window(); w.show(); w.go("lighting"); self.app.processEvents()
        heights = []
        for t in range(4):
            w.light_tabs.set_index(t); w.light_stack.setCurrentIndex(t); self.app.processEvents(); self.app.processEvents(); heights.append(w.light_stack.minimumSizeHint().height())
        self.assertGreater(heights[1], heights[2] + 300, "the short Rear bar tab is not as tall as the Per-key tab"); self.assertLess(heights[2], 500)
        w.light_stack.setCurrentIndex(2); self.app.processEvents()
        rows = list(w.light_stack.widget(2).findChildren(gui.Row)); self.assertTrue(rows)
        for r in rows: self.assertEqual(r.sizePolicy().verticalPolicy(), QSizePolicy.Policy.Fixed, "a row never soaks up spare height")
        biggest = max(r.height() for r in rows); self.assertLess(biggest, 90, f"no row is stretched (tallest row {biggest} px)")
        self.assertEqual(w.windowTitle(), "TrueSense - Lighting"); w.go("settings"); self.assertEqual(w.windowTitle(), "TrueSense - Settings"); w.nav_back(); self.assertEqual(w.windowTitle(), "TrueSense - Lighting")
        w.pages.setCurrentIndex(0); self.assertEqual(w.windowTitle(), "TrueSense"); w.close()

    def test_icons_are_rendered_once(self):
        a = gui.icon_pixmap("fan", "#dfe5ee", 20); b = gui.icon_pixmap("fan", "#dfe5ee", 20); self.assertIs(a, b, "the same icon is rendered once, not on every repaint")
        self.assertIsNot(a, gui.icon_pixmap("fan", "#58a6ff", 20)); self.assertFalse(a.isNull())

    def test_idle_refresh_asks_the_hardware_for_little(self):
        w = gui.Window(); w.show(); self.app.processEvents(); calls = []
        names = ("get_mode", "fan_state", "rear_get", "get_kb_timeout", "get_battery_limit", "get_calibration", "get_usb_charge", "get_overdrive", "get_boot_sound", "get_gpu_mode", "on_ac")
        real = {n: getattr(gui.hw, n) for n in names}; real_ipc = gui.ipc.call; real_kb = gui.keyboard.get_brightness
        for n in names: setattr(gui.hw, n, (lambda n=n, f=real[n]: (lambda *a, **k: (calls.append(n), f(*a, **k))[1]))())
        gui.keyboard.get_brightness = lambda *a, **k: (calls.append("kb_brightness"), real_kb(*a, **k))[1]
        gui.ipc.call = lambda cmd, **k: {"live": {"power_mode": "turbo", "ac": True}, "settings": {"smart_fan": True, "auto_profile": True, "auto_mode": "smart"}} if cmd == "state" else real_ipc(cmd, **k)
        got = []; w.bus.refreshed.connect(got.append)
        try:
            time.sleep(0.5); self.app.processEvents(); calls.clear(); w.refreshing = False; w.full_refresh("home_fast"); self.assertTrue(self._wait(lambda: got))
            self.assertEqual(sorted(set(calls)), ["fan_state"], f"the quick refresh reads only the fans from the hardware, got {sorted(set(calls))}")
            self.assertEqual(got[-1]["mode"], "turbo"); self.assertTrue(got[-1]["ac"]); got.clear(); calls.clear(); w.refreshing = False; w.full_refresh("home"); self.assertTrue(self._wait(lambda: got))
            self.assertGreaterEqual(len(set(calls)), 8, "the full refresh (every 20 s, on focus, after a change) still reads everything")
        finally:
            for n in names: setattr(gui.hw, n, real[n])
            gui.ipc.call = real_ipc; gui.keyboard.get_brightness = real_kb
        w.close()

    def test_sensors_are_read_off_the_ui_thread(self):
        import threading
        w = gui.Window(); w.show(); self.app.processEvents(); seen = []; real = gui.hw.sensors
        gui.hw.sensors = lambda: (seen.append(threading.current_thread() is threading.main_thread()), real())[1]
        try:
            w.sensing = False; w.tick(); self.assertTrue(self._wait(lambda: seen)); self.assertEqual(seen, [False], "temperatures and fan speeds are never read on the UI thread")
            self.assertTrue(self._wait(lambda: "°C" in w.stat_cpu.text()), "and the labels still update")
        finally: gui.hw.sensors = real
        w.close()

    def test_automatic_refreshes_skip_the_hardware_when_the_data_is_fresh(self):
        w = gui.Window(); w.show(); self.app.processEvents(); scopes = []; real = w.full_refresh
        w.full_refresh = lambda scope=None: (scopes.append(scope), real(scope))[1]
        try:
            self.assertTrue(self._wait(lambda: not w.refreshing)); scopes.clear()
            w.last_full = time.monotonic(); w.refresh_if_stale(); self.assertEqual(scopes, [], "a full read just ran: coming back to the window or opening a page does not repeat it")
            w.go("battery"); w.go("home"); self.app.processEvents(); self.assertEqual(scopes, [], "switching pages quickly does not re-read everything each time")
            w.last_full = time.monotonic() - 60; w.refresh_if_stale(); self.assertEqual(scopes, [None], "stale data is read again")
            self.assertTrue(self._wait(lambda: not w.refreshing)); scopes.clear(); w.refresh_if_stale(scope="audio"); self.assertEqual(scopes, ["audio"], "the audio page is always read when it opens")
            self.assertTrue(self._wait(lambda: not w.refreshing)); scopes.clear(); w.last_full = time.monotonic(); w.do(lambda: None, "ok"); self.assertTrue(self._wait(lambda: scopes)); self.assertEqual(scopes[0], None, "after you change something the window always re-reads")
        finally: w.full_refresh = real
        w.close()

    def test_every_control_has_a_name_for_screen_readers(self):
        from PyQt6.QtWidgets import QAbstractButton, QAbstractSpinBox, QComboBox, QLineEdit, QSlider, QWidget
        from PyQt6.QtCore import Qt
        w = gui.Window(); w.show(); self.app.processEvents(); unnamed = []
        for name, i in gui.PAGES.items():
            w.go(name)
            if name == "lighting":
                for t in range(4): w.light_tabs.set_index(t); w.light_stack.setCurrentIndex(t)
            self.app.processEvents()
            for c in w.pages.widget(i).widget().findChildren(QWidget):
                if not isinstance(c, (QAbstractButton, QComboBox, QLineEdit, QSlider, QAbstractSpinBox)) or c.isHidden() or c.focusPolicy() == Qt.FocusPolicy.NoFocus: continue
                if isinstance(c, QLineEdit) and isinstance(c.parentWidget(), QAbstractSpinBox): continue                  # the spin box itself carries the name
                if not (c.accessibleName() or (isinstance(c, QAbstractButton) and c.text())): unnamed.append(f"{name}: {type(c).__name__} {c.toolTip()[:30]}")
        self.assertEqual(unnamed, [], "controls a screen reader would announce without a name")
        w.go("settings"); lab = [r for r in w.pages.widget(gui.PAGES["settings"]).widget().findChildren(gui.Row) if r.widgets and isinstance(r.widgets[0], QComboBox)][0]
        self.assertIs(lab.label.buddy(), lab.widgets[0], "clicking a row's name focuses its drop-down"); w.close()

    def test_the_window_shows_home_first_and_builds_the_other_pages_afterwards(self):
        from PyQt6.QtWidgets import QScrollArea
        w = gui.Window(lazy=True); w.show(); self.assertFalse(w.built); self.assertEqual(w.pages.count(), len(gui.PAGES), "placeholders keep every page number fixed")
        self.assertIsInstance(w.pages.widget(0), QScrollArea); self.assertFalse(hasattr(w, "kb_level"), "the Lighting page is not built yet"); w.tick()            # must not touch missing pages
        seen = []; orig = w._apply; w._apply = lambda d: (seen.append((w.built, dict(d))), orig(d))[1]
        w._apply({"zz_marker": 1}); self.assertIn("zz_marker", w.pending_apply, "data that arrives early is kept, not lost or crashed on")
        self.assertTrue(self._wait(lambda: w.built, timeout=10), "the rest is built in the background")
        for i in range(len(gui.PAGES)): self.assertIsInstance(w.pages.widget(i), QScrollArea, f"page {i} is real")
        self.assertIsNone(w.pending_apply, "the held-back data was handed over"); self.assertTrue(any(built and "zz_marker" in d for built, d in seen), "...and applied only once every page exists"); w.close()

    def test_navigating_before_the_pages_exist_builds_them_at_once(self):
        w = gui.Window(lazy=True); w.show(); self.assertFalse(w.built)
        w.go("settings"); self.assertTrue(w.built); self.assertEqual(w.pages.currentIndex(), gui.PAGES["settings"]); self.assertTrue(hasattr(w, "pm_sw"))
        w.go("keys"); self.assertEqual(w.pages.currentIndex(), gui.PAGES["lighting"]); w.close()
        w2 = gui.Window(lazy=True); w2.ensure_built(); self.assertTrue(w2.built); w2.ensure_built(); w2.close()                               # idempotent

    def test_the_update_module_loads_only_when_it_is_used(self):
        import subprocess
        import sys as _sys
        code = "import sys; from truesense import gui; a = 'urllib.request' in sys.modules; gui.update.parse('v0.3.11'); print(a, 'urllib.request' in sys.modules)"
        env = dict(os.environ, PYTHONPATH=os.path.join(os.path.dirname(__file__), "..", "src"), QT_QPA_PLATFORM="offscreen")
        self.assertEqual(subprocess.run([_sys.executable, "-c", code], env=env, capture_output=True, text=True).stdout.split(), ["False", "True"])

    def test_the_audio_page_shows_whether_there_is_a_sound_output_and_can_repair_it(self):
        from truesense import audio
        w = gui.Window(); w.show(); w.go("audio"); self.app.processEvents()
        w._apply({"sound": {"ok": False, "problem": "the sound card is switched off; its speaker and headphone profiles are missing"}}); self.assertIn("No sound output", w.snd_lbl.text()); self.assertIn("Fix sound", w.snd_lbl.text())
        w._apply({"sound": {"ok": True}}); self.assertIn("output is present", w.snd_lbl.text()); w._apply({"sound": {"ok": None, "problem": "x"}}); self.assertIn("No sound card", w.snd_lbl.text())
        calls = []; real = audio.repair; audio.repair = lambda: (calls.append(1), {"fixed": True, "changed": True, "steps": ["restarted WirePlumber"], "state": {"ok": True}})[1]
        msgs = []; w.bus.message.connect(msgs.append)
        try:
            w.sound_fix(); self.assertTrue(self._wait(lambda: calls and w.snd_btn.isEnabled())); self.assertTrue(any("repaired" in m for m in msgs), msgs)
        finally: audio.repair = real
        w.close()

    def test_designs_and_pictures_paint_the_picture(self):
        from truesense import designs
        w = gui.Window(); w.go("keys"); names = [w.ds_cb.itemData(i) for i in range(w.ds_cb.count())]; self.assertIn("gaming", names)
        w.ds_cb.setCurrentIndex(w.ds_cb.findData("gaming")); w.design_apply(); self.assertEqual(w.board.cells[0], "%02x%02x%02x" % designs.cells_of(designs.get("gaming"))[0])
        w.ds_name.setText("gui-test"); w.design_save(); self.assertIn("gui-test", [w.ds_cb.itemData(i) for i in range(w.ds_cb.count())]); w.close()

    def test_game_rules_page_shows_and_edits_the_rules(self):
        w = gui.Window(); w.game_show({"rules": {"hl2_linux": "performance", "x.exe": "turbo"}, "running": "turbo"})
        self.assertEqual(w.gr_list.count(), 2); self.assertEqual(w.gr_list.currentData(), "hl2_linux"); self.assertIn("turbo", w.gr_msg.text())
        w.game_show({"rules": {}, "running": None}); self.assertEqual(w.gr_list.count(), 0); self.assertIn("No game rules", w.gr_msg.text())
        calls = []; orig = gui.ipc.call; gui.ipc.call = lambda *a, **k: calls.append(k) or {"rules": {}, "running": None}
        try:
            w.gr_name.setText("Game.exe"); w.gr_mode.setCurrentIndex(4); w.game_add(); self.assertTrue(self._wait(lambda: calls)); self.assertEqual((calls[0]["action"], calls[0]["name"], calls[0]["mode"]), ("add", "Game.exe", "turbo"))
        finally: gui.ipc.call = orig
        w.close()

    def test_lighting_profiles_work_like_the_other_profile_pages(self):
        from truesense import looks
        w = gui.Window(); w.look_cb.setCurrentIndex(w.look_cb.findText("native")); w.look_selected()
        self.assertFalse(w.look_del.isEnabled()); self.assertFalse(w.look_copy.isHidden())                     # native: read-only, copy to edit
        w.look_name.setText("my-lights"); w.look_selected(); w.look_copy_do(); self.assertIn("my-lights", looks.names()); self.assertTrue(w.look_copy.isHidden()); self.assertTrue(w.look_del.isEnabled())
        looks.save("my-lights", {"keyboard": {"mode": "effect", "name": "wave", "color": "00ff00", "speed": 4, "brightness": 20, "direction": 0}, "rear": None})
        w.look_cb.setCurrentIndex(w.look_cb.findText("my-lights")); w.look_delete(); self.assertNotIn("my-lights", looks.names()); self.assertEqual(w.look_cb.currentText(), "native")
        with self.assertRaises(__import__('truesense.device', fromlist=['HwError']).HwError): looks.save("native", {"keyboard": None, "rear": None})
        w.close()

    def test_mouse_wheel_scrolls_the_page_but_never_changes_a_setting(self):
        from PyQt6.QtCore import QPoint, QPointF, Qt
        from PyQt6.QtGui import QWheelEvent
        w = gui.Window(); w.setFixedHeight(400); w.show(); w.go("battery"); self.app.processEvents()
        def wheel(widget, dy): return QWheelEvent(QPointF(5, 5), QPointF(5, 5), QPoint(0, 0), QPoint(0, dy), Qt.MouseButton.NoButton, Qt.KeyboardModifier.NoModifier, Qt.ScrollPhase.NoScrollPhase, False)
        w.go("home"); self.app.processEvents(); before = (w.kb_level.value(), w.fan_pct.value(), w.fan_cpu.value()); idx = w.kb_fx.currentIndex()
        for widget in (w.kb_level, w.fan_pct, w.fan_cpu, w.kb_fx): self.app.sendEvent(widget, wheel(widget, 240)); self.app.sendEvent(widget, wheel(widget, -240))
        self.assertEqual((w.kb_level.value(), w.fan_pct.value(), w.fan_cpu.value()), before, "the wheel must not move a slider"); self.assertEqual(w.kb_fx.currentIndex(), idx, "nor a drop-down")
        w.pl1.setValue(65); self.app.sendEvent(w.pl1, wheel(w.pl1, 240)); self.assertEqual(w.pl1.value(), 65, "nor a number box")
        w.close()

    def test_update_banner_settings_and_failure_handling(self):
        w = gui.Window(); w.show(); self.app.processEvents()
        w._upd_show({"current": "0.3.0", "latest": "0.4.0", "available": True, "error": None, "info": {"tag": "v0.4.0", "version": "0.4.0", "url": "https://example.invalid/r"}})
        self.assertTrue(w.banner.isVisible()); self.assertFalse(w.upd_btn.isHidden()); self.assertIn("0.4.0", w.upd_lbl.text()); self.assertIn("0.4.0", w.banner_lbl.text())
        w._upd_show({"current": "0.3.0", "latest": "0.3.0", "available": False, "error": None, "info": {"tag": "v0.3.0", "version": "0.3.0", "url": ""}})
        self.assertFalse(w.banner.isVisible()); self.assertTrue(w.upd_btn.isHidden()); self.assertIn("newest version", w.upd_lbl.text())
        w._upd_show({"current": "0.3.0", "latest": None, "available": False, "error": "could not reach GitHub: no network", "info": None}); self.assertIn("could not reach GitHub", w.upd_lbl.text())
        w._upd_show({"done": {"ok": False, "error": "polkit authorisation was cancelled"}}); self.assertIn("did not finish", w.upd_lbl.text()); self.assertTrue(w.upd_btn.isEnabled())
        self.assertFalse(w.auto_upd.isChecked(), "automatic update checks are off until you switch them on"); w.close()
