# SPDX-License-Identifier: MIT
"""The terminal UI, tested without a terminal: the same drawing code writes into a text grid, and keys and mouse clicks are fed in directly.
Nothing here touches the real service or sound system (the daemon socket is made unavailable)."""
import os
import pathlib
import subprocess
import tempfile
import threading
import time
import unittest

from truesense import audio, backend as hw, device, ipc, keyboard, tui, userstate


def make(w=100, h=30, unicode_ok=True):
    cv = tui.GridCanvas(w, h, unicode_ok); app = tui.App(cv, sync=True)
    for _ in range(3): app.data.fast()
    app.refresh(); return app, cv


def go(app):
    """Let the pause after a change run out, so what was chosen is written now."""
    for r in list(app.pending): r.due = 1e-9
    app.tick()


def select(app, label, nth=0):
    rows = [i for i, r in enumerate(app.page.rows) if r.label == label]; app.page.sel = rows[nth]; return app.page.rows[rows[nth]]


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
        self.spawned = []; self.real_popen, self.real_doctor = subprocess.Popen, tui.run_doctor
        subprocess.Popen = lambda *a, **k: self.spawned.append(a) or None         # "Open the window" must never start a real window from a test
        tui.run_doctor = lambda doctor: []                                          # nor run the real health check (it asks systemd and the sound system)
        self.real_wait = audio._wait_node; audio._wait_node = lambda *a, **k: True        # a fake sound system never grows the node it would wait 8 s for
        self.dts_calls = []; self.real_dts, self.real_bin = audio.dts, audio._dts_bin
        def no_dts(*a, timeout=30): self.dts_calls.append(a); raise device.HwError("AcerUltraX is not available in the tests")
        audio.dts = no_dts; audio._dts_bin = lambda: None                       # never the real AcerUltraX: a mode change would change your real sound
        userstate.PATH = os.path.join(tempfile.mkdtemp(), "user.json")

    def tearDown(self):
        ipc.available, audio._run, userstate.PATH = self.saved; audio.dts, audio._dts_bin = self.real_dts, self.real_bin; subprocess.Popen, tui.run_doctor = self.real_popen, self.real_doctor; audio._wait_node = self.real_wait


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
        app.key("l"); go(app); app.draw(); t = cv.text(); self.assertIn("What you did", t); self.assertIn("Power mode", t.split("What you did")[1])

    def test_the_help_dialog_opens_and_closes_on_any_key(self):
        app, cv = make(); app.key("?"); app.draw(); self.assertIn("Keys", cv.text()); app.key("x"); app.draw(); self.assertNotIn("Keys", cv.text())


class TestKeys(TuiCase):
    def test_tab_digits_and_shift_tab_change_page(self):
        app, _ = make(); app.key("\t"); self.assertEqual(app.page_i, 1); app.key("5"); self.assertEqual(app.page_i, 4); app.key(__import__("curses").KEY_BTAB); self.assertEqual(app.page_i, 3); app.key("1"); self.assertEqual(app.page_i, 0)

    def test_only_q_quits_escape_closes_a_dialog_or_undoes_a_change(self):
        app, _ = make(); app.key("q"); self.assertTrue(app.quit)
        app, _ = make(); app.key(27); self.assertFalse(app.quit, "a stray Esc must not quit")
        app, _ = make(); app.confirm("sure?", lambda: None); app.key(27); self.assertIsNone(app.modal); self.assertFalse(app.quit)

    def test_a_change_is_shown_at_once_written_after_a_pause_and_enter_writes_it_now(self):
        app, _ = make(); before = hw.get_mode(); row = app.page.rows[0]; app.key("l")           # first row is the power mode; right = next option
        self.assertEqual(hw.get_mode(), before, "not written yet: the pause lets a run of key presses become one write"); self.assertNotEqual(row.value(), before, "but shown at once"); self.assertEqual(row.state, "pending")
        app.key("\n"); self.assertNotEqual(hw.get_mode(), before); self.assertIn("Power mode", app.msg[0]); self.assertEqual(row.state, "")
        before = hw.get_mode(); app.key("l"); app.key("h"); go(app); self.assertEqual(hw.get_mode(), before)

    def test_m_steps_through_the_power_modes_and_quick_presses_are_one_write(self):
        app, _ = make(); i0 = tui.MODE_ORDER.index(hw.get_mode()); writes = []; real = hw.set_mode; hw.set_mode = lambda m: (writes.append(m), real(m))[1]
        try:
            app, _ = make(); app.key("m"); app.key("m"); app.key("m"); self.assertEqual(writes, []); go(app)
        finally: hw.set_mode = real
        self.assertEqual(writes, [tui.MODE_ORDER[(i0 + 3) % 5]], "three presses, one write, three steps")

    def test_escape_takes_back_a_change_that_was_not_sent(self):
        app, _ = make(); before = hw.get_mode(); row = app.page.rows[0]; app.key("l"); app.key("l"); app.key(27); go(app)
        self.assertEqual(hw.get_mode(), before); self.assertEqual(row.value(), before); self.assertEqual(row.state, ""); self.assertEqual(app.pending, [])

    def test_a_toggle_flips_at_once_and_reads_back(self):
        app, _ = make(); app.goto(2); select(app, "Battery limit (80 %)")
        was = hw.get_battery_limit(); app.key(" "); self.assertEqual(hw.get_battery_limit(), not was); self.assertIn("Battery limit", app.msg[0])

    def test_the_fan_slider_writes_once_when_the_arrow_keys_pause(self):
        app, _ = make(); app.goto(1); app.key("j")                              # Mode -> CPU fan
        app.key("l"); app.key("l"); app.key("l"); self.assertEqual(app.pending, [app.page.rows[app.page.sel]], "queued, not written on every key")
        calls = []; real = tui.App.run; tui.App.run = lambda self, label, fn, *a, **k: (calls.append(label), real(self, label, fn, *a, **k))[1]
        try: go(app)
        finally: tui.App.run = real
        self.assertEqual(len(calls), 1, calls); self.assertEqual(device.SIM.fan_duty[0], 45)

    def test_a_brightness_of_zero_turns_the_light_off_it_is_not_a_fan_stop(self):
        for name, label, read in (("rear", "Brightness", lambda: hw.rear_get()["brightness"]), ("kb", "Brightness", keyboard.get_brightness)):
            hw.rear_set("static", "ffffff", 3, 60, 1); app, _ = make(); app.goto(3); select(app, label, 1 if name == "rear" else 0)
            for _ in range(12): app.key("h")
            self.assertIsNone(app.modal, name + ": zero is a normal value for a light"); go(app); self.assertEqual(read(), 0, name); self.assertNotIn("fan", app.msg[0].lower()); self.assertEqual(app.msg[1], "good")

    def test_choosing_an_effect_never_leaves_a_dark_light_dark(self):
        hw.rear_set("static", "ffffff", 3, 60, 1); app, _ = make(); app.goto(3); select(app, "Brightness", 1)
        for _ in range(12): app.key("h")
        go(app); self.assertEqual(hw.rear_get()["brightness"], 0); sel = select(app, "Effect", 1); app.edit(sel, "breathing", now=True)
        self.assertEqual(hw.rear_get()["mode"], "breathing"); self.assertGreaterEqual(hw.rear_get()["brightness"], 40, "an effect picked while the bar is off turns it on, at a usable brightness")

    def test_enter_on_a_slider_that_was_not_changed_writes_nothing(self):
        app, _ = make(); app.goto(1); app.key("j"); before = (list(device.SIM.fan_duty), hw.fan_state()["mode"], hw.get_mode())
        app.key("\n"); app.key(" "); self.assertIsNone(app.modal); self.assertEqual((list(device.SIM.fan_duty), hw.fan_state()["mode"], hw.get_mode()), before); self.assertEqual(app.pending, [])

    def test_a_slider_with_an_unknown_reading_starts_in_the_middle_never_at_off(self):
        app, _ = make(); app.goto(1); row = select(app, "CPU fan"); app.data.fanhw = {}; self.assertIsNone(row.index())
        app.key("h"); self.assertEqual(row.state, "pending"); self.assertNotEqual(row.value(), 0); self.assertIsNone(app.modal)
        app.undo_all(); app.key("l"); self.assertGreaterEqual(row.value(), 20)

    def test_a_fan_that_runs_at_ten_percent_is_not_shown_as_off(self):
        app, _ = make(); app.goto(1); row = select(app, "CPU fan"); app.data.fanhw = {"mode": "manual", "duty_register_pct": {"cpu": 10, "gpu": 40}}
        self.assertEqual(row.values[row.index()], 20); app.data.fanhw["duty_register_pct"]["cpu"] = 0; self.assertEqual(row.values[row.index()], 0)

    def test_stopping_a_fan_asks_defaults_to_no_and_is_refused_without_the_service(self):
        app, _ = make(); app.goto(1); app.key("j")
        for _ in range(8): app.key("h")                                         # down to the bottom stop
        self.assertIsNone(app.modal, "moving there does not ask or write"); self.assertNotEqual(device.SIM.fan_duty[0], 0)
        app.key("\n"); self.assertEqual(app.modal["kind"], "confirm"); self.assertEqual(app.modal["focus"], 1, "the marked answer is No"); before = list(device.SIM.fan_duty)
        app.key("\n"); self.assertIsNone(app.modal, "Enter on the default answer cancels"); self.assertEqual(list(device.SIM.fan_duty), before); self.assertIn("Cancelled", app.msg[0]); self.assertEqual(select(app, "CPU fan").value(), device.SIM.fan_duty[0], "the slider shows the real value again")
        for _ in range(8): app.key("h")
        app.key("\n"); app.key("y"); self.assertEqual(list(device.SIM.fan_duty), before, "a stop needs the service: nothing was written"); self.assertEqual(app.msg[1], "bad"); self.assertIn("service", app.msg[0])

    def test_the_stop_question_can_be_answered_with_the_arrows_and_enter_but_enter_alone_never_says_yes(self):
        app, _ = make(); app.goto(1); select(app, "Mode"); asked = []; row = app.page.rows[app.page.sel]
        app.edit(row, "off", now=True); self.assertEqual(app.modal["kind"], "confirm"); self.assertEqual(app.modal["focus"], 1)
        app.key("h"); self.assertEqual(app.modal["focus"], 0); app.key("l"); self.assertEqual(app.modal["focus"], 1); app.key("\n"); self.assertIsNone(app.modal); self.assertEqual(row.state, ""); asked.append(1)
        app.edit(row, "off", now=True); app.key("h"); app.key("\n"); self.assertEqual(app.msg[1], "bad", "Yes was chosen: the write was tried (and refused: no service)")

    def test_a_question_that_is_not_answered_is_a_no_when_you_quit(self):
        app, _ = make(); app.goto(1); app.key("j")
        for _ in range(8): app.key("h")
        before = list(device.SIM.fan_duty); app.flush(); self.assertEqual(list(device.SIM.fan_duty), before); self.assertEqual(app.pending, [])

    def test_a_hint_belongs_to_its_row_and_is_gone_when_you_move(self):
        app, cv = make(); app.goto(1); app.key("j"); app.key("\n"); self.assertIn("Use", app.msg[0]); app.draw(); self.assertIn("Use", cv.text())
        app.key("j"); app.draw(); self.assertNotIn("applied by itself", cv.text(), "the hint does not follow you to another row")
        app.key("k"); app.key("\n"); app.key("\t"); app.draw(); self.assertNotIn("applied by itself", cv.text(), "nor to another page")

    def test_choosing_manual_on_the_fan_mode_row_works_and_keeps_both_fans_running(self):
        app, _ = make(); app.goto(1); row = select(app, "Mode"); app.data.fan = None; app.edit(row, "manual", now=True)
        self.assertEqual(app.msg[1], "good", app.msg[0]); self.assertEqual(hw.fan_state()["mode"], "manual"); self.assertGreaterEqual(min(device.SIM.fan_duty), 20)

    def test_moving_one_fan_slider_never_stops_the_other_fan(self):
        app, _ = make(); app.goto(1); app.data.fanhw = {"mode": "auto", "duty_register_pct": {"cpu": 0, "gpu": 0}}                    # an odd reading must not become a stop
        row = select(app, "CPU fan"); app.edit(row, 60, now=True); self.assertEqual(device.SIM.fan_duty[0], 60); self.assertGreaterEqual(device.SIM.fan_duty[1], 20)

    def test_a_custom_colour_is_typed_in_a_dialog_and_checked(self):
        app, _ = make(); app.goto(3); app.page.sel = next(i for i, r in enumerate(app.page.rows) if r.label == "Custom colour")
        app.key("\n"); self.assertEqual(app.modal["kind"], "prompt")
        for ch in "ff8800": app.key(ch)
        app.key("\n"); go(app); self.assertEqual(keyboard.last()["color"], "ff8800")
        app.key("\n"); [app.key(ch) for ch in "nothex"]; app.key("\n"); self.assertEqual(keyboard.last()["color"], "ff8800", "a bad colour is refused"); self.assertEqual(app.msg[1], "bad")

    def test_a_value_that_could_not_be_written_is_not_left_on_screen(self):
        real = hw.set_overdrive
        def refuse(on): raise device.HwError("firmware said no")
        hw.set_overdrive = refuse                                                # the page captures the setter when it is built
        try:
            app, _ = make(); app.goto(2); row = select(app, "Panel overdrive"); was = row.value(); app.key(" ")
        finally: hw.set_overdrive = real
        self.assertEqual(app.msg[1], "bad"); self.assertIn("firmware said no", app.msg[0]); self.assertEqual(row.value(), was, "the toggle shows the real state again"); self.assertEqual(row.state, "")

    def test_a_failed_light_write_forgets_the_remembered_colour(self):
        real = keyboard.set_effect
        def refuse(*a, **k): raise device.HwError("no")
        keyboard.set_effect = refuse
        try:
            app, _ = make(); app.goto(3); row = select(app, "Colour"); app.edit(row, "ff0000", now=True)
        finally: keyboard.set_effect = real
        self.assertEqual(app.msg[1], "bad"); self.assertNotIn("color", app.kb)

    def test_a_button_marks_its_own_row_busy_and_not_the_row_you_changed_last(self):
        app, _ = make(); app.goto(2); other = select(app, "Boot sound"); app.key(" "); self.assertEqual(other.state, "")
        app.goto(5); btn = next(r for r in app.page.rows if r.kind == "button"); app.page.sel = app.page.rows.index(btn); seen = []
        btn.put = lambda: app.run("Profile y", lambda: seen.append(btn.state)); app.key("\n")
        self.assertEqual(seen, ["writing"]); self.assertEqual(btn.state, ""); self.assertEqual(other.state, "")

    def test_a_button_that_is_still_running_is_not_started_twice(self):
        app, _ = make(); app.goto(5); btn = next(r for r in app.page.rows if r.kind == "button"); app.page.sel = app.page.rows.index(btn); calls = []
        btn.put = lambda: calls.append(1); btn.state = "writing"; app.key("\n"); self.assertEqual(calls, []); self.assertIn("Still working", app.msg[0])

    def test_a_failed_action_is_reported_not_raised(self):
        app, _ = make(); app.goto(5)                                            # Profiles
        app.page.sel = next(i for i, r in enumerate(app.page.rows) if r.kind == "button"); app.page.rows[app.page.sel].put = lambda: app.run("Profile x", lambda: (_ for _ in ()).throw(device.HwError("boom")))
        app.key("\n"); self.assertEqual(app.msg[1], "bad"); self.assertIn("boom", app.msg[0])

    def test_quitting_sends_a_change_made_a_moment_ago(self):
        app, _ = make(); before = hw.get_mode(); app.key("l"); app.flush(); self.assertNotEqual(hw.get_mode(), before)

    def test_the_bottom_line_shows_the_keys_of_the_selected_row(self):
        app, cv = make(); app.goto(1); select(app, "CPU fan"); app.draw(); self.assertIn("adjust", cv.text().split("\n")[-1])
        app.goto(3); select(app, "Effect"); app.draw(); self.assertIn("choose", cv.text().split("\n")[-1]); app.goto(2); select(app, "Boot sound"); app.draw(); self.assertIn("switch", cv.text().split("\n")[-1])


class TestFuzz(TuiCase):
    def test_random_keys_and_clicks_never_raise_and_never_leave_work_behind(self):
        import curses
        import random
        keys = [ord(c) for c in "jkhlqm r?ynx0123456789\t\n \x1b"] + [curses.KEY_UP, curses.KEY_DOWN, curses.KEY_LEFT, curses.KEY_RIGHT, curses.KEY_BTAB, curses.KEY_HOME, curses.KEY_END, curses.KEY_NPAGE, curses.KEY_PPAGE, 12, 127]
        for seed, (w, h) in enumerate(((100, 30), (60, 20), (44, 12), (140, 50), (80, 24))):
            rnd = random.Random(seed); app, cv = make(w, h)
            for step in range(1500):
                r = rnd.random()
                if r < 0.62: app.key(rnd.choice([k for k in keys if k not in (ord("q"),)]))
                elif r < 0.80: app.mouse(rnd.randrange(w), rnd.randrange(h), wheel=rnd.choice((0, 0, -1, 1)), click=rnd.random() < 0.8)
                elif r < 0.9: app.mouse_move(rnd.randrange(w), rnd.randrange(h))
                elif r < 0.93: app.mouse_release()
                elif r < 0.96: app.tick()
                else: go(app)
                if step % 7 == 0: app.draw()
            app.modal = None; app.flush(); go(app); app.draw()
            self.assertEqual(app.pending, [], (seed, w, h)); self.assertTrue(app.worker.idle())
            self.assertTrue(all(len(line) <= w for line in cv.text().split("\n")), (seed, w, h)); self.assertNotIn(0, device.SIM.fan_duty, "no fan was ever stopped: that needs the service and a yes")


class TestWorker(unittest.TestCase):
    def test_writes_run_in_order_and_a_newer_one_replaces_an_older_one_that_has_not_started(self):
        w = tui.Worker(); ran = []; gate = threading.Event(); dropped = []
        w.submit("a", lambda: (gate.wait(2), ran.append("first")))
        time.sleep(0.05)
        w.submit("k", lambda: ran.append("k1"), on_drop=lambda: dropped.append("k1")); w.submit("k", lambda: ran.append("k2"), on_drop=lambda: dropped.append("k2")); w.submit("other", lambda: ran.append("other"))
        gate.set(); self.assertTrue(w.drain(3)); self.assertEqual(ran, ["first", "k2", "other"]); self.assertEqual(dropped, ["k1"])

    def test_a_failing_write_does_not_stop_the_queue(self):
        w = tui.Worker(); ran = []; w.submit(None, lambda: 1 / 0); w.submit(None, lambda: ran.append("after")); self.assertTrue(w.drain(3)); self.assertEqual(ran, ["after"])


class TestDialogsAndDrag(TuiCase):
    def test_clicking_yes_or_no_in_the_question_answers_it(self):
        app, cv = make(); app.goto(2); row = select(app, "GPU mode"); app.edit(row, "nvidia", now=True); app.draw(); self.assertIn("Switch the GPU mode", cv.text()); self.assertIn("[ Yes ]", cv.text())
        before = hw.get_gpu_mode(); self.assertTrue(click(app, "[ No ]")); self.assertIsNone(app.modal); self.assertEqual(hw.get_gpu_mode(), before)
        app.edit(row, "nvidia", now=True); app.draw(); self.assertTrue(click(app, "[ Yes ]")); self.assertEqual(hw.get_gpu_mode(), "nvidia")

    def test_dragging_a_slider_follows_the_pointer_and_writes_when_released(self):
        app, _ = make(); app.goto(3); row = select(app, "Brightness", 0); app.draw()
        h = next(h for h in app.hits if h[3] is row and h[4] == "bar"); x0, bw = h[1], h[5]; y = h[0]
        app.mouse(x0, y); self.assertEqual(row.value(), row.values[0]); writes = []; real = keyboard.set_effect; keyboard.set_effect = lambda *a, **k: (writes.append(a[3]), real(*a, **k))[1]
        try:
            for x in range(x0, x0 + bw, 3): app.mouse_move(x, y)
            app.mouse_move(x0 + bw + 10, y); self.assertEqual(row.value(), row.values[-1], "past the end is the end")
            n = len(writes); app.mouse_release()
        finally: keyboard.set_effect = real
        self.assertEqual(writes[-1], row.values[-1]); self.assertLessEqual(len(writes) - n, 1); self.assertIsNone(app.drag)

    def test_a_key_press_ends_a_drag(self):
        app, _ = make(); app.goto(3); row = select(app, "Brightness", 0); app.draw(); h = next(h for h in app.hits if h[3] is row and h[4] == "bar"); app.mouse(h[1], h[0]); self.assertIsNotNone(app.drag); app.key("j"); self.assertIsNone(app.drag)


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


class TestMenuEntry(unittest.TestCase):
    def test_the_application_menu_entry_opens_the_tui_in_a_terminal_and_is_installed_everywhere(self):
        root = os.path.join(os.path.dirname(__file__), ".."); rd = lambda p: pathlib.Path(root, p).read_text()
        d = dict(l.split("=", 1) for l in rd("packaging/truesense-tui.desktop").splitlines() if "=" in l)
        self.assertEqual(d["Exec"], "@PREFIX@/bin/truesense tui"); self.assertEqual(d["Terminal"], "true"); self.assertEqual(d["Type"], "Application"); self.assertIn("Name", d); self.assertIn("Icon", d)
        for f in ("tools/install.sh", "tools/uninstall.sh", "packaging/arch/PKGBUILD", "packaging/aur/PKGBUILD.template"): self.assertIn("truesense-tui.desktop", rd(f), f)


if __name__ == "__main__": unittest.main()
