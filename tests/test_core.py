import os, tempfile, time, tomllib, threading, unittest
from truesense import actions, backend as hw, config, device, ipc, profiles, state, daemon
from truesense.fnkeys import Gestures


class Base(unittest.TestCase):
    def setUp(self): device.SIM.reset()


class TestDevice(Base):
    def test_parse(self):
        self.assertEqual(device.parse("ok a=1 b=0x2\n"), {"a": "1", "b": "0x2"})
        with self.assertRaises(device.HwError): device.parse("err nope (-5)")
        with self.assertRaises(device.HwError): device.parse("garbage")

    def test_invalid_commands_refused(self):
        for c in ("set_mode 3", "set_fan_manual 10 10", "set_usb 15", "set_gpu 0", "set_battery 3 1", "set_battery 1 2", "set_boot 7", "get_misc 99", "get_battery 0", "set_rear 00", "set_color 123", "set_mode", "version extra", "frobnicate"):
            with self.assertRaises(device.HwError, msg=c): device.cmd(c)


class TestBackend(Base):
    def test_every_setter_reads_back(self):
        self.assertEqual(hw.set_mode("turbo"), "turbo")
        self.assertEqual(hw.set_usb_charge(10), 10)
        self.assertTrue(hw.set_battery_limit(True)); self.assertFalse(hw.set_overdrive(False)); self.assertTrue(hw.set_boot_sound(True)); self.assertEqual(hw.set_gpu_mode("auto"), "auto")
        hw.fan_manual(60, 70); self.assertEqual(hw.fan_state()["mode"], "manual"); hw.fan_max(); self.assertEqual(hw.fan_state()["mode"], "max"); hw.fan_auto(); self.assertEqual(hw.fan_state()["mode"], "auto")

    def test_bad_values_rejected_before_the_device(self):
        for fn in (lambda: hw.set_mode("x"), lambda: hw.set_usb_charge(5), lambda: hw.fan_manual(10, 50), lambda: hw.set_gpu_mode("x"), lambda: hw.rear_set("x"), lambda: hw.rear_set("static", "zz")):
            with self.assertRaises(device.HwError): fn()

    def test_settle_retries_then_fails(self):
        calls = []
        def getter(): calls.append(1); return -1 if len(calls) < 3 else 5
        self.assertEqual(hw.settle(getter, 5, "x", tries=5, delay=0), 5)
        with self.assertRaises(device.HwError): hw.settle(lambda: -1, 5, "x", tries=3, delay=0)

    def test_rear_roundtrip(self):
        r = hw.rear_set("breathing", "00ff88", 3, 70, 1); self.assertEqual((r["mode"], r["color"], r["brightness"]), ("breathing", "00ff88", 70))


class TestConfig(Base):
    def test_defaults_valid_and_typos_caught(self):
        cfg = config.load("/nonexistent", None); self.assertEqual(config.validate(cfg), [])
        cfg["fan"]["cpu_up"] = [90, 10]; self.assertTrue(config.validate(cfg))

    def test_toml_roundtrip_and_user_write(self):
        d = {"keys": {"tap": "power:turbo", "power_cycle": ["eco", "turbo"], "long_ms": 900}, "flag": True, "a": {"b": {"c": 1}}}
        self.assertEqual(tomllib.loads(config.dumps(d)), d)
        p = tempfile.mktemp(suffix=".toml"); config.write_user({"keys": {"tap": "none"}}, p); config.write_user({"keys": {"double": "x"}, "audio": {"notify": False}}, p)
        self.assertEqual(config._read(p), {"keys": {"tap": "none", "double": "x"}, "audio": {"notify": False}}); os.remove(p)


class TestGestures(unittest.TestCase):
    def run_pattern(self, pattern, long_ms=250, multi_ms=100):
        out = []; g = Gestures(out.append, long_ms, multi_ms)
        for ev, dt in pattern:
            {"down": g.press, "up": g.release}.get(ev, lambda: None)(); time.sleep(dt)
        time.sleep(0.35); return out

    def test_gestures(self):
        tap = [("down", .03), ("up", 0)]
        self.assertEqual(self.run_pattern(tap), ["tap"])
        self.assertEqual(self.run_pattern(tap + [("w", .02)] + tap), ["double"])
        self.assertEqual(self.run_pattern(tap + [("w", .02)] + tap + [("w", .02)] + tap), ["triple"])
        self.assertEqual(self.run_pattern([("down", .5), ("up", 0)]), ["long"])
        self.assertEqual(self.run_pattern(tap + [("w", .25)] + tap), ["tap", "tap"])
        self.assertEqual(self.run_pattern([("down", .4), ("up", 0), ("w", .03)] + tap), ["long", "tap"])


class TestActions(Base):
    def test_validate(self):
        good = ["none", "power-next", "power:turbo", "power-cycle:eco,turbo", "fan:smart", "fan:60", "fan:max", "profile:gaming", "profile-next", "output-toggle", "output:speakers",
                "mic-toggle", "mic:mute", "voice-next", "touchpad-toggle", "touchpad:off", "keyboard:toggle", "keyboard-effect-next", "keyboard-profile:x", "rear:off", "rear-effect-next",
                "battery-limit-toggle", "overdrive-toggle", "exec:echo hi", "notify:hello"]
        for g in good: self.assertIsNone(actions.validate(g), g)
        for b in ("", "power", "power:ludicrous", "fan:5", "fan:101", "output:usb", "mic:x", "touchpad:toggle", "rear:dim", "exec", "frobnicate", "power-next:3", "none:x"):
            self.assertIsNotNone(actions.validate(b), b)

    def test_catalogue_covers_every_action_name(self):
        text = actions.catalogue()
        for n in actions.FIXED | actions.WITH_ARG: self.assertIn(n, text, f"{n} missing from the documented catalogue")

    def test_power_cycle_and_battery(self):
        cfg = config.load("/nonexistent", None); hw.set_mode("eco")
        self.assertIn("quiet", actions.run("power-next", cfg)); self.assertIn("eco", actions.run("power-prev", cfg)); self.assertIn("turbo", actions.run("power:turbo", cfg))
        self.assertIn("on", actions.run("battery-limit-toggle", cfg)); self.assertIn("off", actions.run("battery-limit-toggle", cfg))


class TestProfiles(Base):
    def test_shipped_profiles_load_and_apply(self):
        self.assertTrue({"gaming", "quiet", "balanced", "battery-saver"} <= set(profiles.names()))
        msg = profiles.apply("battery-saver"); self.assertIn("applied", msg); self.assertNotIn("FAILED", msg)
        self.assertEqual(hw.get_mode(), "eco"); self.assertTrue(hw.get_battery_limit()); self.assertFalse(hw.get_overdrive())

    def test_names_cannot_escape(self):
        for bad in ("../etc/passwd", "a b", "", "x/y"):
            with self.assertRaises(device.HwError): profiles.load(bad)

    def test_capture_roundtrip(self):
        hw.set_mode("turbo"); cap = profiles.capture("t"); self.assertEqual(tomllib.loads(config.dumps(cap)), cap); self.assertEqual(cap["power"]["mode"], "turbo")


class TestIpc(unittest.TestCase):
    def test_server(self):
        d = tempfile.mkdtemp(); sp = d + "/t.sock"; st = state.State({"smart_fan": True}, d + "/s.json")
        srv = ipc.Server({"ping": lambda: "pong", "set": lambda key, value: (st.set(key, value), st.snapshot())[1], "boom": lambda: 1 / 0}, [os.getuid()], sp); srv.start()
        try:
            self.assertEqual(ipc.call("ping", path=sp), "pong"); self.assertFalse(ipc.call("set", key="smart_fan", value=False, path=sp)["smart_fan"])
            with self.assertRaises(device.HwError): ipc.call("nope", path=sp)
            with self.assertRaises(device.HwError): ipc.call("boom", path=sp)
            self.assertFalse(state.State({"smart_fan": True}, d + "/s.json").get("smart_fan"))        # survived a restart
            srv.allowed = {424242}
            with self.assertRaises(device.HwError) as cm: ipc.call("ping", path=sp)
            self.assertIn("not allowed", str(cm.exception))
        finally: srv.stop()
        with self.assertRaises(device.HwError): ipc.call("ping", path=sp)                          # daemon gone: a clear error, not a crash


class TestDaemonLogic(Base):
    def make(self):
        cfg = config.load("/nonexistent", None); cfg["user"] = "no-such-user"
        st = state.State({"smart_fan": True, "auto_profile": True, "operating_follow": False, "profile_hold_until": 0}, tempfile.mkdtemp() + "/s.json")
        daemon.RUN = tempfile.mkdtemp(); b = daemon.Brain(cfg, st); b.say = lambda *a, **k: None; b.last_ac = True; return b

    def drive(self, b, n, cpu, gpu, load):
        for _ in range(n): device.SIM.temps = (cpu, 55, gpu); b.cpu_load = load; b.step()

    def test_idle_heavy_hot_and_back(self):
        b = self.make(); self.drive(b, 25, 45, 45, 2); self.assertEqual(device.SIM.fan, "auto")
        self.drive(b, 8, 70, 60, 90); self.assertEqual(device.SIM.fan, "manual")
        self.drive(b, 14, 93, 70, 99); self.assertEqual(device.SIM.fan_duty, [100, 100])         # emergency (the temperature is smoothed, so it takes a few ticks)
        self.drive(b, 45, 50, 48, 2); self.assertEqual(device.SIM.fan, "auto")

    def test_smart_fan_off_is_hands_off_and_hold_wins(self):
        b = self.make(); h = b.handlers(); h["set"](key="smart_fan", value=False); self.drive(b, 5, 95, 95, 99); self.assertEqual(device.SIM.fan, "auto")
        h["set"](key="smart_fan", value=True)
        for bad in ({"key": "bogus", "value": True}, {"key": "smart_fan", "value": "yes"}):
            with self.assertRaises(device.HwError): h["set"](**bad)
        device.SIM.mode = 6; b.cur_mode = "eco"; b.busy_hold = 20; h["hold"](minutes=30); self.drive(b, 35, 70, 60, 90); self.assertEqual(device.SIM.mode, 6)
        h["hold"](minutes=None); self.drive(b, 35, 70, 60, 90); self.assertEqual(device.SIM.mode, 5)

    def test_survives_a_missing_device(self):
        b = self.make(); self.drive(b, 30, 45, 45, 2); real = device.SIM.run
        device.SIM.run = lambda c: "err no such device\n"
        try:
            for _ in range(10): b.step()                                                    # must not raise
            self.assertFalse(b.hw_ok)
        finally: device.SIM.run = real
        self.drive(b, 5, 50, 50, 5); self.assertTrue(b.hw_ok)

    def test_shutdown_returns_fans_to_auto(self):
        b = self.make(); hw.fan_manual(80, 80)
        with self.assertRaises(SystemExit): b.shutdown()
        self.assertEqual(device.SIM.fan, "auto")


class TestModules(unittest.TestCase):
    def test_every_module_imports_and_has_no_undefined_names(self):
        import importlib, pkgutil, truesense, ast, builtins
        for m in pkgutil.iter_modules(truesense.__path__):
            if m.name in ("gui", "__main__"): continue            # gui needs GTK; __main__ runs the CLI
            importlib.import_module("truesense." + m.name)
        # a cheap static check for names used but never defined/imported (what a compiler would not catch)
        for m in pkgutil.iter_modules(truesense.__path__):
            if m.name in ("gui", "__main__"): continue
            src = truesense.util.read(os.path.join(truesense.__path__[0], m.name + ".py")); tree = ast.parse(src)
            defined = set(dir(builtins)) | {"__file__", "__name__", "__doc__"}
            for n in ast.walk(tree):
                if isinstance(n, (ast.Import, ast.ImportFrom)): defined |= {(a.asname or a.name).split(".")[0] for a in n.names}
                elif isinstance(n, (ast.FunctionDef, ast.ClassDef)): defined.add(n.name); defined |= {a.arg for a in getattr(n, "args", ast.arguments(args=[], posonlyargs=[], kwonlyargs=[])).args + getattr(getattr(n, "args", None), "kwonlyargs", [])} if isinstance(n, ast.FunctionDef) else set()
                elif isinstance(n, ast.Name) and isinstance(n.ctx, (ast.Store, ast.Del)): defined.add(n.id)
                elif isinstance(n, ast.arg): defined.add(n.arg)
                elif isinstance(n, ast.ExceptHandler) and n.name: defined.add(n.name)
                elif isinstance(n, ast.comprehension):
                    for t in ast.walk(n.target):
                        if isinstance(t, ast.Name): defined.add(t.id)
            used = {n.id for n in ast.walk(tree) if isinstance(n, ast.Name) and isinstance(n.ctx, ast.Load)}
            self.assertEqual(sorted(used - defined), [], f"undefined name(s) in {m.name}.py")


class TestCli(Base):
    def test_every_parser_formats_its_help(self):
        import argparse
        from truesense import cli
        ap = cli.build(); seen = [0]
        def walk(p):
            p.format_help(); seen[0] += 1
            for a in p._actions:
                if isinstance(a, argparse._SubParsersAction):
                    for sp in a.choices.values(): walk(sp)
        walk(ap); self.assertGreater(seen[0], 60)

    def test_json_anywhere_and_completion(self):
        import io, contextlib, json
        from truesense import cli
        for argv in (["sensors", "--json"], ["--json", "sensors"]):
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf): self.assertEqual(cli.main(argv), 0)
            self.assertIn("cpu_temp", json.loads(buf.getvalue()))
        from truesense import completion
        for sh in ("bash", "zsh", "fish"): self.assertIn("truesense", completion.generate(cli.build(), sh))

    def test_errors_exit_nonzero_cleanly(self):
        import io, contextlib
        from truesense import cli
        err = io.StringIO()
        with contextlib.redirect_stderr(err): rc = cli.main(["fan", "set", "5"])
        self.assertEqual(rc, 1); self.assertIn("20..100", err.getvalue())


if __name__ == "__main__": unittest.main()
