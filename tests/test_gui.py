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
