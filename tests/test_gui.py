import os, time, unittest

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
        from truesense import update
        w = gui.Window(); w.show(); self.app.processEvents()
        w._upd_show({"current": "0.3.0", "latest": "0.4.0", "available": True, "error": None, "info": {"tag": "v0.4.0", "version": "0.4.0", "url": "https://example.invalid/r"}})
        self.assertTrue(w.banner.isVisible()); self.assertFalse(w.upd_btn.isHidden()); self.assertIn("0.4.0", w.upd_lbl.text()); self.assertIn("0.4.0", w.banner_lbl.text())
        w._upd_show({"current": "0.3.0", "latest": "0.3.0", "available": False, "error": None, "info": {"tag": "v0.3.0", "version": "0.3.0", "url": ""}})
        self.assertFalse(w.banner.isVisible()); self.assertTrue(w.upd_btn.isHidden()); self.assertIn("newest version", w.upd_lbl.text())
        w._upd_show({"current": "0.3.0", "latest": None, "available": False, "error": "could not reach GitHub: no network", "info": None}); self.assertIn("could not reach GitHub", w.upd_lbl.text())
        w._upd_show({"done": {"ok": False, "error": "polkit authorisation was cancelled"}}); self.assertIn("did not finish", w.upd_lbl.text()); self.assertTrue(w.upd_btn.isEnabled())
        self.assertFalse(w.auto_upd.isChecked(), "automatic update checks are off until you switch them on"); w.close()
