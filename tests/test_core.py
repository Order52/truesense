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

    def test_release_without_press_is_ignored(self):
        # Fn+F10 / Fn+F11 / Fn+F12 / Fn+Esc end with the same `04 00 00` release as the PREDATOR key; it must not count as a tap
        self.assertEqual(self.run_pattern([("up", 0), ("w", .02), ("up", 0)]), [])
        self.assertEqual(self.run_pattern([("up", 0), ("w", .02), ("down", .03), ("up", 0)]), ["tap"])


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
        daemon.RUN = tempfile.mkdtemp(); daemon.TMP_STATE = tempfile.mkdtemp() + "/ts"; b = daemon.Brain(cfg, st); b.say = lambda *a, **k: None; b.last_ac = True
        daemon.hw.on_ac = lambda: True                                           # not the real charger state
        b.read_cpu_load = lambda: None                                          # the test sets cpu_load itself; the real /proc/stat must not pull it down
        b.find_hwmon = lambda: None; b.coretemp = None; b.gpu_temp = 0          # the test drives the temperatures; real sensors must not leak in
        return b

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
        h["hold"](minutes=None); self.drive(b, 35, 70, 60, 90); self.assertEqual(device.SIM.mode, 4)      # auto takes over: performance (turbo stays blocked after the earlier overheat)

    def test_boot_look_is_stored_and_applied(self):
        import tempfile
        from truesense import looks, keyboard
        looks.BOOT_FILE = tempfile.mkdtemp() + "/boot-look.json"
        b = self.make(); h = b.handlers(); self.assertIsNone(h["boot_look"](action="get"))
        look = {"keyboard": {"mode": "effect", "name": "wave", "color": "00ff00", "speed": 4, "brightness": 20, "direction": 0}, "rear": {"mode": "breathing", "color": "ff0000", "speed": 2, "brightness": 80, "direction": 1}}
        h["boot_look"](action="set", look=look); self.assertEqual(h["boot_look"](action="get")["keyboard"]["name"], "wave")
        with self.assertRaises(device.HwError): h["boot_look"](action="set", look={})
        keyboard.SIM["packets"].clear(); b.apply_boot_look("test"); time.sleep(0.5)
        self.assertTrue(keyboard.SIM["packets"], "the boot look should have reached the keyboard")
        self.assertEqual(device.SIM.rear[1], 1)                                  # breathing
        h["boot_look"](action="clear"); self.assertIsNone(h["boot_look"](action="get"))

    def test_native_auto_follows_the_power_source_only(self):
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="native"); b.cur_mode = "eco"; device.SIM.mode = 6
        self.drive(b, 8, 40, 40, 95); self.assertEqual(device.SIM.mode, 1)                       # on AC: balanced, even under load (no heat/load logic)
        daemon.hw.on_ac = lambda: False; self.drive(b, 8, 40, 40, 95); self.assertEqual(device.SIM.mode, 6)   # on battery: eco
        daemon.hw.on_ac = lambda: True

    def test_smart_auto_escalates_and_backs_off_for_heat(self):
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="smart")
        self.drive(b, 70, 40, 40, 5); self.assertEqual(device.SIM.mode, 1)                       # idle on AC: balanced
        self.drive(b, 10, 70, 60, 90); self.assertEqual(device.SIM.mode, 4)                      # heavy load: performance first
        self.drive(b, 35, 70, 60, 90); self.assertEqual(device.SIM.mode, 5)                      # sustained and cool enough: turbo
        self.drive(b, 10, 93, 70, 95); self.assertEqual(device.SIM.mode, 4)                      # hot: back off from turbo at once
        self.drive(b, 40, 70, 60, 90); self.assertEqual(device.SIM.mode, 4)                      # and stay there during the back-off hold
        self.drive(b, 100, 40, 40, 3); self.assertIn(device.SIM.mode, (1, 4))

    def test_smart_auto_on_battery_saves_power(self):
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="smart"); daemon.hw.on_ac = lambda: False; b.last_ac = False
        self.drive(b, 40, 40, 40, 4); self.assertEqual(device.SIM.mode, 6)                       # idle on battery: eco
        self.drive(b, 40, 70, 60, 90); self.assertEqual(device.SIM.mode, 1)                      # busy on battery: balanced, not turbo
        daemon.hw.on_ac = lambda: True

    def test_fan_curve_is_followed_and_custom_curves_are_checked(self):
        from truesense import curves
        b = self.make(); h = b.handlers(); self.drive(b, 25, 45, 45, 2); self.assertEqual(device.SIM.fan, "auto")
        h["fan_curve"](action="use", name="max-cooling"); self.drive(b, 8, 65, 50, 40)
        self.assertEqual(device.SIM.fan, "manual"); self.assertGreaterEqual(device.SIM.fan_duty[0], 90)
        with self.assertRaises(device.HwError): h["fan_curve"](action="use", name="weak", data={"cpu": [[50, 0], [70, 50]], "gpu": [[50, 0], [78, 100]]})
        self.assertEqual(h["fan_curve"](action="get")["name"], "max-cooling")
        c = curves.load("truesense"); self.assertAlmostEqual(curves.interp(c["cpu"], 80), 70); self.assertEqual(curves.interp(c["cpu"], 100), 100)

    def test_smart_profile_changes_how_smart_auto_behaves(self):
        from truesense import smartprofiles as sp
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="smart")
        h["smart_profile"](action="use", name="battery-first"); self.drive(b, 70, 40, 40, 5);
        self.drive(b, 60, 70, 60, 90); self.assertEqual(device.SIM.mode, 4)                       # battery-first never uses Turbo, even when sustained
        h["smart_profile"](action="use", name="performance-first"); self.drive(b, 25, 70, 60, 90); self.assertEqual(device.SIM.mode, 5)   # performance-first: Turbo after 10 s
        with self.assertRaises(device.HwError): h["smart_profile"](action="use", name="bad", data={"ac_busy": "warp"})
        self.assertEqual(h["smart_profile"](action="get")["name"], "performance-first")
        with self.assertRaises(device.HwError): sp.save("silent", dict(sp.DEFAULTS))                # built-ins are read-only
        self.assertTrue(b.reason)

    def test_undervolt_needs_confirmation_and_boot_has_a_crash_guard(self):
        import tempfile
        from truesense import cpu, undervolt
        store = {"core": 0, "cache": 0}
        cpu.uv_get = lambda plane=0: store["core" if plane == 0 else "cache"]
        def fake_set(plane, mv, confirm=False): store["core" if plane == 0 else "cache"] = mv
        cpu.uv_set = fake_set; cpu.uv_reset = lambda: (store.update(core=0, cache=0), {"core_mv": 0, "cache_mv": 0})[1]
        daemon.UV_PENDING = tempfile.mkdtemp() + "/uv-pending"
        b = self.make(); h = b.handlers(); uv = h["undervolt"]
        with self.assertRaises(device.HwError): uv(action="apply", core=-50, cache=-40)                       # no confirmation: refused
        with self.assertRaises(device.HwError): uv(action="apply", core=-300, cache=-40, confirm=True)         # beyond the limit: refused
        self.assertEqual(store, {"core": 0, "cache": 0})
        uv(action="apply", core=-50, cache=-40, confirm=True, name="truesense"); self.assertEqual(store, {"core": -50, "cache": -40})
        uv(action="reset"); self.assertEqual(store, {"core": 0, "cache": 0})
        uv(action="boot", core=-30, cache=-30, confirm=True, name="mild"); self.assertEqual(uv(action="get")["boot"]["name"], "mild")
        b.apply_uv_boot(); self.assertEqual(store["core"], -30); self.assertTrue(os.path.exists(daemon.UV_PENDING))      # applied, guard flag set
        store.update(core=0, cache=0); b2 = self.make(); b2.st.set("uv_boot", {"name": "mild", "core": -30, "cache": -30}); b2.apply_uv_boot()
        self.assertEqual(store["core"], 0, "the previous boot never proved stable: do not apply again"); self.assertIsNone(b2.st.get("uv_boot"))
        self.assertEqual(undervolt.load("stock")["core"], 0)
        with self.assertRaises(device.HwError): undervolt.save("truesense", {"core": -10, "cache": -10})

    def test_your_pick_ends_by_itself_when_the_situation_changes(self):
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="smart")
        self.drive(b, 70, 40, 40, 5); self.assertEqual(device.SIM.mode, 1)
        h["power"](mode="eco", hold_minutes=30); self.assertEqual(device.SIM.mode, 6)
        self.drive(b, 30, 40, 40, 5); self.assertEqual(device.SIM.mode, 6, "same situation: your pick stays")
        self.drive(b, 8, 70, 60, 90); self.assertEqual(device.SIM.mode, 4, "a game starts: smart auto takes over without anyone pressing resume")
        h["power"](mode="eco", hold_minutes=30); daemon.hw.on_ac = lambda: False; self.drive(b, 8, 70, 60, 90)
        self.assertNotEqual(device.SIM.mode, 4); daemon.hw.on_ac = lambda: True                            # unplugging also ends the hold

    def test_choosing_smart_or_native_applies_immediately_even_during_a_hold(self):
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="off"); self.drive(b, 5, 40, 40, 5)
        h["power"](mode="turbo", hold_minutes=30); self.assertEqual(device.SIM.mode, 5); h["set"](key="auto_mode", value="native"); self.drive(b, 3, 40, 40, 5)
        self.assertEqual(device.SIM.mode, 1, "native auto (AC: balanced) must not wait for the old hold to run out"); self.assertEqual(h["state"]()["hold_left_s"], 0)

    def test_gpu_undervolt_needs_confirmation_and_boot_has_a_crash_guard(self):
        import tempfile
        from truesense import gpuundervolt as gu
        state = {"offset": 0, "cap": 0}
        gu.read = lambda: {"offset_mhz": state["offset"], "range_mhz": [-1000, 1000]}
        def fake_apply(o, c): state.update(offset=o, cap=c); return {"offset_mhz": o, "cap_mhz": c}
        gu.apply = fake_apply; gu.reset = lambda: (state.update(offset=0, cap=0), {"offset_mhz": 0, "cap_mhz": 0})[1]
        daemon.GPU_UV_PENDING = tempfile.mkdtemp() + "/gpu-uv-pending"
        b = self.make(); uv = b.handlers()["gpu_undervolt"]
        with self.assertRaises(device.HwError): uv(action="apply", offset=100, cap=2400)
        with self.assertRaises(device.HwError): uv(action="apply", offset=900, cap=2400, confirm=True)
        with self.assertRaises(device.HwError): uv(action="apply", offset=100, cap=50, confirm=True)
        self.assertEqual(state, {"offset": 0, "cap": 0})
        uv(action="apply", offset=120, cap=2400, confirm=True, name="truesense"); self.assertEqual(state, {"offset": 120, "cap": 2400}); self.assertEqual(uv(action="get")["applied"]["name"], "truesense")
        uv(action="reset"); self.assertEqual(state, {"offset": 0, "cap": 0})
        uv(action="boot", offset=60, cap=2500, confirm=True, name="mild"); b.apply_gpu_uv_boot(); self.assertEqual(state["offset"], 60); self.assertTrue(os.path.exists(daemon.GPU_UV_PENDING))
        state.update(offset=0, cap=0); b2 = self.make(); b2.st.set("gpu_uv_boot", {"name": "mild", "offset": 60, "cap": 2500}); b2.apply_gpu_uv_boot()
        self.assertEqual(state["offset"], 0, "the previous boot never proved stable"); self.assertIsNone(b2.st.get("gpu_uv_boot"))
        with self.assertRaises(device.HwError): gu.save("truesense", {"offset": 10, "cap": 0})

    def test_power_limits_are_kept_and_risky_values_need_force(self):
        import tempfile
        from truesense import cpu
        d = tempfile.mkdtemp() + "/"
        for n in ("constraint_0_power_limit_uw", "constraint_1_power_limit_uw"): open(d + n, "w").write("80000000")
        daemon.RAPL = d; cpu.RAPL = d
        b = self.make(); h = b.handlers()
        r = h["cpu_limits"](pl1=70, pl2=90); self.assertEqual((r["pl1_w"], r["pl2_w"]), (70, 90))
        for _ in range(5): b.step()                                                                 # the daemon must not overwrite your limits
        self.assertEqual(open(d + "constraint_0_power_limit_uw").read(), "70000000"); self.assertEqual(open(d + "constraint_1_power_limit_uw").read(), "90000000")
        self.assertEqual(h["cpu_info"]()["user"], {"pl1": 70.0, "pl2": 90.0})
        with self.assertRaises(device.HwError): h["cpu_limits"](pl1=105, pl2=118)                  # beyond the danger line: needs force
        h["cpu_limits"](pl1=105, pl2=118, force=True)
        h["cpu_limits"](reset=True)
        for _ in range(4): b.step()
        self.assertIsNone(h["cpu_info"]()["user"]); self.assertEqual(open(d + "constraint_0_power_limit_uw").read(), open(d + "constraint_1_power_limit_uw").read())
        self.assertEqual(cpu.pl_risk(65, 80)[0], "safe"); self.assertEqual(cpu.pl_risk(90, 100)[0], "caution"); self.assertEqual(cpu.pl_risk(110, 115)[0], "danger")

    def test_undervolt_risk_zones(self):
        from truesense import undervolt, gpuundervolt as gu
        self.assertEqual(undervolt.risk(0, 0)[0], "safe"); self.assertEqual(undervolt.risk(-50, -40)[0], "safe"); self.assertEqual(undervolt.risk(-75, -40)[0], "caution"); self.assertEqual(undervolt.risk(-100, -40)[0], "danger")
        self.assertEqual(gu.risk(120, 2400)[0], "safe"); self.assertEqual(gu.risk(180, 2400)[0], "caution"); self.assertEqual(gu.risk(220, 2400)[0], "danger"); self.assertEqual(gu.risk(100, 0)[0], "caution")

    def test_headset_boost_reads_the_value_line_not_the_channel_count(self):
        from truesense import audio
        header = "numid=8,iface=MIXER,name='Headset Mic Boost Volume'\n  ; type=INTEGER,access=rw---R--,values=2,min=0,max=3,step=0\n  : values=%d,%d\n  | dBscale-min=0.00dB,step=10.00dB,mute=0\n"
        state = {"v": 1}; real = audio._amixer
        def fake(*a):
            if a[0] == "cset": state["v"] = int(a[2].split(",")[0]); return ""
            return header % (state["v"], state["v"])
        audio._amixer = fake
        try:
            self.assertEqual(audio.headset_boost(), 1)
            for lv in (0, 3, 2, 1): self.assertEqual(audio.headset_boost(lv), lv)
        finally: audio._amixer = real

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
