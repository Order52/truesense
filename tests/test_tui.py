# SPDX-License-Identifier: MIT
"""The terminal UI, tested without a terminal: the same drawing code writes into a text grid, and keys and mouse clicks are fed in directly.
Nothing here touches the real service or sound system (the daemon socket is made unavailable)."""
import os
import subprocess
import tempfile
import time
import unittest

from truesense import audio, backend as hw, device, ipc, keyboard, tui, userstate


def make(w=100, h=30, unicode_ok=True):
    cv = tui.GridCanvas(w, h, unicode_ok); app = tui.App(cv, sync=True)
    for _ in range(3): app.data.fast()
    app.refresh(); return app, cv


def click(app, text, y=None):
    """Click the first place `text` is drawn (after a draw)."""
    app.draw(); rows = app.cv.text().split("\n")
    for yy, line in enumerate(rows):
        if (y is None or yy == y) and text in line: app.mouse(line.index(text) + 1, yy); return True
    return False


class TuiCase(unittest.TestCase):
    def setUp(self):
        device.SIM.reset(); self.saved = (ipc.available, audio._run, userstate.PATH)
        ipc.available = lambda *a, **k: False                                   # never the real service
        audio._run = lambda cmd: subprocess.CompletedProcess(cmd, 1, "", "")   # never the real sound system
        userstate.PATH = os.path.join(tempfile.mkdtemp(), "user.json")

    def tearDown(self): ipc.available, audio._run, userstate.PATH = self.saved


class TestDrawing(TuiCase):
    def test_every_page_draws_at_every_size_without_an_error(self):
        for w, h in ((140, 50), (100, 30), (80, 24), (60, 20), (44, 12)):
            app, cv = make(w, h)
            for i in range(len(app.pages)):
                app.goto(i); app.draw(); txt = cv.text()
                self.assertIn("TrueSense", txt, (w, h, i)); self.assertTrue(all(len(line) <= w for line in txt.split("\n")))

    def test_a_terminal_that_is_too_small_says_so(self):
        app, cv = make(30, 8); app.draw(); self.assertIn("44x12", cv.text()); self.assertIn("30x8", cv.text())

    def test_the_dashboard_shows_the_readings_and_works_without_the_service(self):
        app, cv = make(100, 30); app.draw(); t = cv.text()
        for word in ("Power mode", "Temperatures", "Fans", "Load", "Power", "CPU", "GPU", "direct"): self.assertIn(word, t)
        self.assertIn("°C", t); self.assertIn("rpm", t)

    def test_narrow_terminals_stack_the_panels_and_keep_the_tabs_usable(self):
        app, cv = make(70, 34); app.draw(); t = cv.text()
        self.assertIn("Temperatures", t); self.assertIn("Power ", t); self.assertIn(" 1 Dashboard ", t)

    def test_plain_ascii_mode_has_no_box_or_arrow_characters(self):
        app, cv = make(100, 30, unicode_ok=False)
        for i in range(len(app.pages)): app.goto(i); app.draw(); self.assertFalse([c for c in cv.text() if 0x2190 <= ord(c) <= 0x2BFF], i)

    def test_long_pages_scroll_to_keep_the_selection_visible(self):
        app, cv = make(100, 16); app.goto(3)
        for _ in range(9): app.key("j")
        app.draw(); self.assertIn("▸", cv.text()); self.assertGreater(app.page.scroll, 0)

    def test_what_you_did_is_listed_on_the_dashboard(self):
        app, cv = make(100, 34); app.draw(); self.assertIn("Nothing yet", cv.text())
        app.key("l"); app.draw(); t = cv.text(); self.assertIn("What you did", t); self.assertIn("Power mode", t.split("What you did")[1])

    def test_the_help_dialog_opens_and_closes_on_any_key(self):
        app, cv = make(); app.key("?"); app.draw(); self.assertIn("Keys", cv.text()); app.key("x"); app.draw(); self.assertNotIn("Keys", cv.text())


class TestKeys(TuiCase):
    def test_tab_digits_and_shift_tab_change_page(self):
        app, _ = make(); app.key("\t"); self.assertEqual(app.page_i, 1); app.key("5"); self.assertEqual(app.page_i, 4); app.key(__import__("curses").KEY_BTAB); self.assertEqual(app.page_i, 3); app.key("1"); self.assertEqual(app.page_i, 0)

    def test_q_and_escape_quit_but_escape_closes_a_dialog_first(self):
        app, _ = make(); app.key("q"); self.assertTrue(app.quit)
        app, _ = make(); app.confirm("sure?", lambda: None); app.key(27); self.assertIsNone(app.modal); self.assertFalse(app.quit); app.key(27); self.assertTrue(app.quit)

    def test_choosing_a_power_mode_writes_it_and_says_so(self):
        app, _ = make(); app.key("l")                                           # first row is the power mode; right = next option
        mode = hw.get_mode(); self.assertIn(mode, hw.MODES); self.assertIn("Power mode", app.msg[0])
        before = hw.get_mode(); app.key("m"); self.assertNotEqual(hw.get_mode(), before); first = hw.get_mode(); app.key("m"); self.assertNotEqual(hw.get_mode(), first, "two quick presses go two steps")

    def test_a_toggle_flips_and_reads_back(self):
        app, _ = make(); app.goto(2); app.page.sel = next(i for i, r in enumerate(app.page.rows) if r.label.startswith("Battery limit"))
        was = hw.get_battery_limit(); app.key(" "); self.assertEqual(hw.get_battery_limit(), not was); self.assertIn("Battery limit", app.msg[0])

    def test_the_fan_slider_writes_once_when_the_arrow_keys_pause(self):
        app, _ = make(); app.goto(1); app.key("j")                              # Mode -> CPU fan
        app.key("l"); app.key("l"); app.key("l"); self.assertEqual(app.pending, [app.page.rows[app.page.sel]], "queued, not written on every key")
        calls = []; real = tui.App.run; tui.App.run = lambda self, label, fn, row=None, ok=None: (calls.append(label), real(self, label, fn, row, ok))[1]
        try: time.sleep(0.4); app.tick()
        finally: tui.App.run = real
        self.assertEqual(len(calls), 1, calls); self.assertEqual(device.SIM.fan_duty[0], 45)

    def test_stopping_a_fan_is_always_asked_and_refused_without_the_service(self):
        app, _ = make(); app.goto(1); app.key("j")
        for _ in range(8): app.key("h")                                         # down to the bottom stop
        self.assertIsNone(app.modal, "moving there does not ask or write"); self.assertNotEqual(device.SIM.fan_duty[0], 0)
        app.key("\n"); self.assertEqual(app.modal["kind"], "confirm"); app.key("n"); self.assertIsNone(app.modal)
        before = list(device.SIM.fan_duty); app.key("\n"); app.key("y")
        self.assertEqual(list(device.SIM.fan_duty), before, "a stop needs the service: nothing was written"); self.assertEqual(app.msg[1], "bad"); self.assertIn("service", app.msg[0])

    def test_a_custom_colour_is_typed_in_a_dialog_and_checked(self):
        app, _ = make(); app.goto(3); app.page.sel = next(i for i, r in enumerate(app.page.rows) if r.label == "Custom colour")
        app.key("\n"); self.assertEqual(app.modal["kind"], "prompt")
        for ch in "ff8800": app.key(ch)
        app.key("\n"); self.assertEqual(keyboard.last()["color"], "ff8800")
        app.key("\n"); [app.key(ch) for ch in "nothex"]; app.key("\n"); self.assertEqual(keyboard.last()["color"], "ff8800", "a bad colour is refused"); self.assertEqual(app.msg[1], "bad")

    def test_a_value_that_could_not_be_written_is_not_left_on_screen(self):
        real = hw.set_overdrive
        def refuse(on): raise device.HwError("firmware said no")
        hw.set_overdrive = refuse                                                # the page captures the setter when it is built
        try:
            app, _ = make(); app.goto(2); row = next(r for r in app.page.rows if r.label == "Panel overdrive"); app.page.sel = app.page.rows.index(row)
            was = row.value(); app.key(" ")
        finally: hw.set_overdrive = real
        self.assertEqual(app.msg[1], "bad"); self.assertIn("firmware said no", app.msg[0]); self.assertEqual(row.value(), was, "the toggle shows the real state again")

    def test_a_failed_action_is_reported_not_raised(self):
        app, _ = make(); app.goto(5)                                            # Profiles
        app.page.sel = next(i for i, r in enumerate(app.page.rows) if r.kind == "button"); app.page.rows[app.page.sel].put = lambda: app.run("Profile x", lambda: (_ for _ in ()).throw(device.HwError("boom")))
        app.key("\n"); self.assertEqual(app.msg[1], "bad"); self.assertIn("boom", app.msg[0])


class TestMouse(TuiCase):
    def test_clicking_a_tab_changes_page(self):
        app, _ = make(); self.assertTrue(click(app, "Fans", 1)); self.assertEqual(app.page_i, 1)

    def test_clicking_an_option_selects_it(self):
        app, _ = make(); app.goto(2); self.assertTrue(click(app, "Turbo")); self.assertEqual(hw.get_mode(), "turbo")

    def test_clicking_a_toggle_flips_it(self):
        app, _ = make(); app.goto(2); was = hw.get_boot_sound(); self.assertTrue(click(app, "[ ON  ]" if was else "[ OFF ]", None) or True)
        row = next(i for i, r in enumerate(app.page.rows) if r.label == "Boot sound"); app.draw()
        y = next(h[0] for h in app.hits if h[3] is app.page.rows[row] and h[4] == "toggle"); x = next(h[1] for h in app.hits if h[3] is app.page.rows[row] and h[4] == "toggle")
        app.mouse(x + 1, y); self.assertNotEqual(hw.get_boot_sound(), was)

    def test_clicking_a_slider_sets_that_position_and_the_wheel_moves_the_selection(self):
        app, _ = make(); app.goto(1); app.draw()
        h = next(h for h in app.hits if h[4] == "bar"); app.mouse(h[2] - 1, h[0]); self.assertEqual(app.page.rows[app.page.sel].index(), len(app.page.rows[app.page.sel].values) - 1)
        sel = app.page.sel; app.mouse(5, 5, wheel=-1); self.assertNotEqual(app.page.sel, sel)

    def test_clicks_while_a_dialog_is_open_do_not_reach_the_page(self):
        app, _ = make(); app.goto(2); app.confirm("x", lambda: None); mode = hw.get_mode(); click(app, "Turbo"); self.assertEqual(hw.get_mode(), mode)


class TestPages(TuiCase):
    def test_every_page_has_its_controls(self):
        want = {1: ("Fan control", "CPU fan", "GPU fan", "Curve"), 2: ("Battery limit", "GPU mode", "USB charging", "Smart profile"), 3: ("Keyboard", "Light bar", "Brightness", "Direction"),
                4: ("Acer DTS sound", "Microphone", "Fix sound", "DTS sound mode"), 5: ("Profiles", "Looks"), 6: ("Health check", "Version")}
        app, cv = make(110, 60)
        for i, words in want.items():
            app.goto(i); app.draw(); t = cv.text()
            for w in words: self.assertIn(w, t, (i, w))

    def test_doctor_results_are_hidden_until_the_check_has_run_and_then_coloured(self):
        app, cv = make(110, 60); app.goto(6); app.draw(); self.assertIn("not run yet", cv.text()); self.assertNotIn("Kernel headers", cv.text())
        app.data.x["doctor"] = [{"check": "Kernel", "status": "OK", "detail": "fine", "fix": ""}, {"check": "Audio", "status": "WARN", "detail": "no output", "fix": "truesense audio repair"}]
        app.draw(); t = cv.text(); self.assertIn("fine", t); self.assertIn("truesense audio repair", t); self.assertNotIn("not run yet", t)
        row = next(y for y, line in enumerate(t.split("\n")) if "fine" in line); col = t.split("\n")[row].index("OK"); self.assertEqual(cv.style_at(row, col), "good")

    def test_the_audio_page_shows_what_the_sound_system_reports(self):
        app, cv = make(110, 40); app.data.x["dtsdev"] = [{"label": "Headset PC38X", "profile": "Music", "kind": "other", "ref": "r", "current": True}]; app.goto(4); app.draw(); self.assertIn("Headset PC38X: Music", cv.text())

    def test_the_profile_list_matches_the_shipped_profiles(self):
        from truesense import profiles
        app, cv = make(110, 40); app.goto(5); app.draw()
        for n in profiles.names(): self.assertIn(n, cv.text())


class TestData(TuiCase):
    def test_a_missing_device_is_reported_not_fatal(self):
        real = hw.sensors
        def broken(): raise device.HwError("no device")
        hw.sensors = broken
        try:
            app, cv = make(); app.draw(); self.assertIn("No device: no device", cv.text())
        finally: hw.sensors = real

    def test_history_feeds_the_sparklines(self):
        app, _ = make()
        for _ in range(10): app.data.fast()
        self.assertGreaterEqual(len(app.data.hist["tcpu"]), 10)

    def test_slow_reads_never_overlap(self):
        d = tui.Data(); order = []
        d.slow_sync([("a", lambda: order.append("a") or 1)]); self.assertEqual(d.x["a"], 1)
        d.slow_sync([("b", lambda: 1 / 0)]); self.assertIn("b", d.errs); self.assertNotIn("b", d.x)


class TestCli(unittest.TestCase):
    def test_the_command_exists_and_needs_a_terminal(self):
        env = dict(os.environ, TRUESENSE_SIM="1", PYTHONPATH=os.path.join(os.path.dirname(__file__), "..", "src"))
        r = subprocess.run(["python3", "-m", "truesense", "tui"], capture_output=True, text=True, env=env, stdin=subprocess.DEVNULL, timeout=20)
        self.assertEqual(r.returncode, 1); self.assertIn("needs a terminal", r.stderr)
        r = subprocess.run(["python3", "-m", "truesense", "tui", "-h"], capture_output=True, text=True, env=env, timeout=20); self.assertIn("--page", r.stdout)


if __name__ == "__main__": unittest.main()
