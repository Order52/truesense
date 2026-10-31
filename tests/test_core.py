# SPDX-License-Identifier: MIT
import os, tempfile, time, tomllib, threading, unittest
from truesense import actions, backend as hw, config, device, ipc, profiles, state, daemon
from truesense.fnkeys import Gestures


def _wr(path, text):
    with open(path, "w") as f: f.write(text)


def _rd(path):
    with open(path) as f: return f.read()


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
        self.drive(b, 14, 70, 60, 90)                                                                 # settle: busy but not hot (the hot state releases 5 degrees below its line)
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
        for n in ("constraint_0_power_limit_uw", "constraint_1_power_limit_uw"): _wr(d + n, "80000000")
        daemon.RAPL = d; cpu.RAPL = d
        b = self.make(); h = b.handlers()
        r = h["cpu_limits"](pl1=70, pl2=90); self.assertEqual((r["pl1_w"], r["pl2_w"]), (70, 90))
        for _ in range(5): b.step()                                                                 # the daemon must not overwrite your limits
        self.assertEqual(_rd(d + "constraint_0_power_limit_uw"), "70000000"); self.assertEqual(_rd(d + "constraint_1_power_limit_uw"), "90000000")
        self.assertEqual(h["cpu_info"]()["user"], {"pl1": 70.0, "pl2": 90.0})
        with self.assertRaises(device.HwError): h["cpu_limits"](pl1=105, pl2=118)                  # beyond the danger line: needs force
        h["cpu_limits"](pl1=105, pl2=118, force=True)
        h["cpu_limits"](reset=True)
        for _ in range(4): b.step()
        self.assertIsNone(h["cpu_info"]()["user"]); self.assertEqual(_rd(d + "constraint_0_power_limit_uw"), _rd(d + "constraint_1_power_limit_uw"))
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

    def test_first_install_defaults_leave_the_firmware_alone(self):
        import tempfile
        from truesense import cpu
        d = tempfile.mkdtemp() + "/"
        for n in ("constraint_0_power_limit_uw", "constraint_1_power_limit_uw"): _wr(d + n, "95000000")          # what the firmware set
        daemon.RAPL = d; cpu.RAPL = d
        cfg = config.load("/nonexistent", None); cfg["user"] = "no-such-user"
        st = state.State({"smart_fan": cfg["daemon"]["smart_fan"], "auto_profile": cfg["daemon"]["auto_profile"], "auto_mode": "native" if cfg["daemon"]["auto_profile"] else "off", "pl_user": None,
                          "profile_hold_until": 0}, tempfile.mkdtemp() + "/s.json")
        daemon.RUN = tempfile.mkdtemp(); daemon.TMP_STATE = tempfile.mkdtemp() + "/ts"; b = daemon.Brain(cfg, st); b.say = lambda *a, **k: None; b.last_ac = True; b.read_cpu_load = lambda: None
        b.find_hwmon = lambda: None; b.coretemp = None; b.gpu_temp = 0; daemon.hw.on_ac = lambda: True
        self.assertEqual((b.auto_kind(), st.get("smart_fan"), b.smart_active()), ("native", False, False))
        self.drive(b, 40, 88, 70, 95)                                                                  # hot and busy: still nothing of ours in the way
        self.assertEqual(device.SIM.fan, "auto", "fans stay on the firmware's own control until you choose smart")
        self.assertEqual(b.cap, cfg["cpu"]["cap_max_khz"], "no CPU clock cap")
        self.assertEqual(_rd(d + "constraint_0_power_limit_uw"), "95000000", "power limits stay as the firmware set them")
        self.assertEqual(device.SIM.mode, 1)                                                           # native auto: Balanced on power
        st.set("smart_fan", True); self.drive(b, 5, 60, 55, 30); self.assertTrue(b.smart_active()); self.assertEqual(_rd(d + "constraint_0_power_limit_uw"), "80000000")       # smart on: ours
        st.set("smart_fan", False); self.drive(b, 5, 60, 55, 30); self.assertEqual(_rd(d + "constraint_0_power_limit_uw"), "95000000", "switching smart off hands the firmware's limits back")

    def test_per_key_animations_only_touch_the_chosen_keys(self):
        from truesense import perkeyfx, keymap
        base = ["00aec7"] * 126 + ["000000"] * 2; w, a, esc = keymap.NAMES["w"][0], keymap.NAMES["a"][0], keymap.NAMES["esc"][0]
        for mode in perkeyfx.MODES:
            f0 = perkeyfx.render(mode, 0.0, base, [w, a], 5); seen = {tuple(perkeyfx.render(mode, 0.37 * i, base, [w, a], 5)[c] for c in (w, a)) for i in range(60)}
            self.assertEqual(len(f0), 128); self.assertEqual(f0[esc], (0x00, 0xAE, 0xC7), f"{mode}: keys that were not chosen must keep their colour")
            self.assertGreater(len(seen), 1, f"{mode} must change over time")
        self.assertEqual(sorted(perkeyfx.MODES)[:0], [])
        wave = perkeyfx.render("wave", 0.5, base, None, 5); self.assertGreater(len({wave[c] for c in range(0, 126, 6)}), 5)       # a wave over all keys shows many colours
        with self.assertRaises(device.HwError): perkeyfx.set_fx("disco")
        with self.assertRaises(device.HwError): perkeyfx.set_fx("wave", speed=50)

    def test_game_rules_raise_the_mode_while_the_game_runs(self):
        from truesense import gamerules
        b = self.make(); h = b.handlers(); self.drive(b, 40, 45, 45, 2); self.assertEqual(device.SIM.mode, 1)        # idle on power: balanced
        r = h["game_rules"](action="add", name="Game.EXE", mode="performance"); self.assertIn("game.exe", r["rules"])
        for bad in ({"action": "add", "name": "x", "mode": "warp"}, {"action": "add", "name": "../x", "mode": "turbo"}, {"action": "remove", "name": "nothing"}, {"action": "zap"}):
            with self.assertRaises(device.HwError): h["game_rules"](**bad)
        real = gamerules.running; gamerules.running = lambda rules, proc="/proc": "performance"
        try:
            self.drive(b, 40, 50, 50, 5); self.assertEqual(device.SIM.mode, 4, "a running game rule raises the mode even when the load is light")
            gamerules.running = lambda rules, proc="/proc": "turbo"; self.drive(b, 40, 93, 93, 99); self.assertNotEqual(device.SIM.mode, 5, "turbo still backs off when hot")
        finally: gamerules.running = real
        self.drive(b, 80, 45, 45, 2); self.assertEqual(device.SIM.mode, 1, "back to the automatic profile when the game ends")
        h["game_rules"](action="clear"); self.assertEqual(h["game_rules"](action="list")["rules"], {})

    def test_game_rule_matching_reads_process_names(self):
        import os as _os
        from truesense import gamerules
        with tempfile.TemporaryDirectory() as d:
            def proc(pid, comm, *args):
                _os.makedirs(f"{d}/{pid}"); open(f"{d}/{pid}/comm", "w").write(comm + "\n"); open(f"{d}/{pid}/cmdline", "wb").write(b"\0".join(a.encode() for a in args) + b"\0")
            proc(10, "bash", "/bin/bash"); proc(11, "hl2_linux", "/home/u/hl2_linux", "-game", "hl2"); proc(12, "wine64-preloader", "Z:\\games\\EldenRing.exe")
            self.assertEqual(gamerules.running({"hl2_linux": "performance"}, d), "performance")
            self.assertEqual(gamerules.running({"eldenring.exe": "turbo", "hl2_linux": "performance"}, d), "turbo")        # the highest mode wins
            self.assertIsNone(gamerules.running({"notepad.exe": "turbo"}, d)); self.assertIsNone(gamerules.running({}, d))

    def test_fans_off_is_guarded(self):
        b = self.make(); h = b.handlers(); self.drive(b, 5, 45, 45, 2)
        with self.assertRaises(device.HwError): h["fan"](mode="off")                                   # no confirmation
        self.drive(b, 10, 66, 50, 20)
        with self.assertRaises(device.HwError): h["fan"](mode="off", confirm=True)                     # too warm to start
        self.drive(b, 25, 45, 45, 2); h["fan"](mode="off", confirm=True)
        self.assertEqual((device.SIM.fan, device.SIM.fan_duty), ("manual", [0, 0])); self.assertTrue(b.fan_off)
        self.drive(b, 3, 50, 45, 2); self.assertEqual(device.SIM.fan_duty, [0, 0], "stays off while it is cool")
        self.drive(b, 12, 75, 50, 20); self.assertEqual(device.SIM.fan, "auto", "the watchdog gives the fans back to the firmware when it gets warm"); self.assertFalse(b.fan_off)

    def test_one_fan_can_be_stopped_with_the_same_guards(self):
        b = self.make(); h = b.handlers(); self.drive(b, 30, 45, 45, 2)
        for kw in ({"mode": "manual", "cpu": 0, "gpu": 60}, {"mode": "manual", "cpu": 60, "gpu": 0}):
            with self.assertRaises(device.HwError): h["fan"](**kw)                                      # needs confirmation
        for bad in ({"cpu": 10, "gpu": 60}, {"cpu": 0, "gpu": 150}):
            with self.assertRaises(device.HwError): h["fan"](mode="manual", confirm=True, **bad)
        self.assertEqual(h["fan"](mode="manual", cpu=0, gpu=60, confirm=True), "partial-off")
        self.assertEqual((device.SIM.fan, device.SIM.fan_duty), ("manual", [0, 60])); self.assertTrue(b.fan_off)
        self.drive(b, 3, 50, 45, 2); self.assertEqual(device.SIM.fan_duty, [0, 60], "stays while cool")
        self.drive(b, 12, 75, 50, 20); self.assertEqual(device.SIM.fan, "auto", "the watchdog gives ALL fans back when it gets warm"); self.assertFalse(b.fan_off)
        self.drive(b, 30, 45, 45, 2); h["fan"](mode="manual", cpu=70, gpu=0, confirm=True); self.assertEqual(device.SIM.fan_duty, [70, 0])
        h["fan"](mode="manual", cpu=50, gpu=50); self.assertFalse(b.fan_off, "a normal manual speed ends the stopped state")
        self.drive(b, 10, 66, 50, 20)
        with self.assertRaises(device.HwError): h["fan"](mode="manual", cpu=0, gpu=60, confirm=True)     # too warm to start

    def test_a_refused_fan_stop_hands_the_fans_back(self):
        from truesense import backend
        device.SIM.fan_duty = [30, 30]; real = device.SIM.c_set_fan_manual
        def stuck(a): device.SIM.fan = "manual"; return "ok fan=manual\n"                               # the firmware says yes but the register does not move
        device.SIM.c_set_fan_manual = stuck
        try:
            with self.assertRaises(device.HwError) as cm: backend.fan_manual(0, 50)
            self.assertIn("did not accept", str(cm.exception)); self.assertEqual(device.SIM.fan, "auto")
        finally: device.SIM.c_set_fan_manual = real

    def test_charge_to_full_once_restores_the_limit(self):
        b = self.make(); h = b.handlers(); b.last_ac = True; hw = daemon.hw
        hw.set_battery_limit(True); r = h["battery_full"](action="start"); self.assertTrue(r["active"]); self.assertFalse(hw.get_battery_limit())
        with self.assertRaises(device.HwError): h["battery_full"](action="start") if not hw.get_battery_limit() else None
        daemon.hw.on_ac = lambda: False; b.last_ac = False; b.charge_full_check(); self.assertTrue(hw.get_battery_limit(), "unplugging brings the 80 % limit back"); self.assertFalse(h["battery_full"](action="get")["active"])
        daemon.hw.on_ac = lambda: True; b.last_ac = True; h["battery_full"](action="start"); h["battery_full"](action="cancel"); self.assertTrue(hw.get_battery_limit())

    def test_nothing_smart_runs_by_default_and_the_pace_adapts(self):
        b = self.make(); h = b.handlers(); h["set"](key="auto_mode", value="native"); h["set"](key="smart_fan", value=False); h["set"]  # native + no smart fan = hands off
        b.st.set("pl_user", None); self.drive(b, 10, 45, 45, 2); self.assertEqual(b.interval, 2.0); self.assertFalse(b.gpu_started, "the GPU sampler is only started when a smart feature needs it")
        h["set"](key="auto_mode", value="smart"); self.drive(b, 3, 45, 45, 2); self.assertEqual(b.interval, 1.0); self.assertTrue(b.gpu_started)

    def test_update_checks_compare_versions_cache_and_ignore_user_repo(self):
        import tempfile, urllib.error
        from truesense import update, __version__
        update.CACHE = tempfile.mkdtemp() + "/update.json"
        self.assertEqual(update.parse("v1.2.3"), (1, 2, 3)); self.assertIsNone(update.parse("main")); self.assertGreater(update.parse("v0.10.0"), update.parse("v0.9.9"))
        calls = []
        def fake_latest(r=None): calls.append(1); return {"version": "9.9.9", "tag": "v9.9.9", "url": "u", "notes": ""}
        real = update.latest; update.latest = fake_latest
        try:
            r = update.check(force=True); self.assertTrue(r["available"]); self.assertEqual((r["current"], r["latest"]), (__version__, "9.9.9"))
            update.check(); update.check(); self.assertEqual(len(calls), 1, "a fresh cache is used instead of asking GitHub again")
            def offline(r=None): raise urllib.error.URLError("no network")
            update.latest = offline; r = update.check(force=True)
            self.assertIn("could not reach GitHub", r["error"]); self.assertEqual(r["latest"], "9.9.9", "the last known answer survives being offline")
            update.latest = lambda r=None: {"version": __version__, "tag": "v" + __version__, "url": "", "notes": ""}; self.assertFalse(update.check(force=True)["available"])
        finally: update.latest = real
        tmp = tempfile.mkdtemp(); user = tmp + "/user.toml"
        with open(user, "w") as f: f.write('[update]\nrepo = "evil/repo"\n')
        self.assertEqual(update.repo(), "Order52/truesense", "the repository never comes from the user config")

    def test_update_prefers_a_newer_tag_over_an_older_release_page(self):
        from truesense import update
        answers = {"releases/latest": {"tag_name": "v0.3.0", "html_url": "rel", "body": "notes"}, "tags": [{"name": "v0.3.1"}, {"name": "v0.3.0"}, {"name": "junk"}]}
        real = update._get_json; update._get_json = lambda url, timeout=8: answers["tags" if "/tags" in url else "releases/latest"]
        try: info = update.latest("o/r")
        finally: update._get_json = real
        self.assertEqual((info["version"], info["tag"]), ("0.3.1", "v0.3.1"), "a tag newer than the latest release page must win")

    def test_update_download_is_checked_before_anything_runs_as_root(self):
        import io, tarfile, tempfile, urllib.request
        from truesense import update
        def tarball(files, root="truesense-9.9.9"):
            buf = io.BytesIO()
            with tarfile.open(fileobj=buf, mode="w:gz") as tf:
                for name, data in files.items():
                    ti = tarfile.TarInfo(f"{root}/{name}" if root else name); ti.size = len(data); ti.mode = 0o644; tf.addfile(ti, io.BytesIO(data))
            return buf.getvalue()
        def opener_for(blob):
            return lambda req, timeout=30: io.BytesIO(blob)
        good = {"tools/install.sh": b"#!/bin/sh\n", "src/truesense/__init__.py": b'__version__ = "9.9.9"\n', "kernel/truesense-wmi/truesense_wmi.c": b"//"}
        root, sha = update.download("v9.9.9", "o/r", opener=opener_for(tarball(good))); self.assertTrue(os.access(root + "/tools/install.sh", os.X_OK)); self.assertEqual(len(sha), 64)
        shutil = __import__("shutil"); shutil.rmtree(os.path.dirname(os.path.dirname(root)))
        for bad, why in ((dict(good, **{"src/truesense/__init__.py": b'__version__ = "9.9.8"\n'}), "wrong version inside"), ({k: v for k, v in good.items() if k != "tools/install.sh"}, "not a release"),
                         ({"../evil.sh": b"x", **good}, "path escape")):
            with self.assertRaises(device.HwError, msg=why): update.download("v9.9.9", "o/r", opener=opener_for(tarball(bad)))
        with self.assertRaises(device.HwError): update.download("main", "o/r", opener=opener_for(b""))                     # only version tags
        ran = []; real = update.check
        update.check = lambda force=False, hours=None: {"current": "0.0.1", "latest": "9.9.9", "available": True, "checked_at": 0, "error": None, "info": {"tag": "v9.9.9", "version": "9.9.9"}}
        try:
            res = update.apply("v9.9.9", gui=False, run=lambda cmd: (ran.append(cmd), type("R", (), {"returncode": 0})())[1], fetch=lambda tag: update.download(tag, "o/r", opener=opener_for(tarball(good))))
        finally: update.check = real
        self.assertEqual(res["returncode"], 0); self.assertTrue(ran and ran[0][-1].endswith("tools/install.sh")); self.assertIn(ran[0][0], ("sudo", "pkexec", ran[0][0]))

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


class TestDesigns(unittest.TestCase):
    def test_builtin_designs_are_valid_and_user_files_win(self):
        import tempfile, json as _json, os as _os
        from truesense import designs
        with tempfile.TemporaryDirectory() as d:
            _os.environ["XDG_CONFIG_HOME"] = d
            try:
                ds = designs.load_all(); self.assertTrue({"gaming", "coding", "stealth"} <= set(ds))
                for n in ds: cv = designs.cells_of(ds[n]); self.assertEqual(len(cv), 128)
                _os.makedirs(designs.user_dir())
                with open(_os.path.join(designs.user_dir(), "gaming.json"), "w") as f: _json.dump({"base": "ffffff"}, f)
                with open(_os.path.join(designs.user_dir(), "broken.json"), "w") as f: f.write("{nope")
                with open(_os.path.join(designs.user_dir(), "badkey.json"), "w") as f: _json.dump({"keys": {"nokey": "ffffff"}}, f)
                ds = designs.load_all(); self.assertEqual(ds["gaming"]["source"], "yours"); self.assertNotIn("broken", ds); self.assertNotIn("badkey", ds)
                p = designs.save("mine", [(1, 2, 3)] * 128); self.assertTrue(_os.path.exists(p))
                with self.assertRaises(device.HwError): designs.save("../x", [])
                self.assertEqual(designs.cells_of(designs.get("mine"))[7], (1, 2, 3))
            finally: _os.environ.pop("XDG_CONFIG_HOME", None)

    def test_image_is_scaled_to_the_key_grid(self):
        import tempfile, os as _os
        from truesense import designs
        try: from PIL import Image
        except ImportError: self.skipTest("Pillow not installed")
        with tempfile.TemporaryDirectory() as d:
            p = _os.path.join(d, "a.png"); Image.new("RGB", (40, 12), (200, 10, 20)).save(p)
            cv = designs.image_cells(p); self.assertEqual(len(cv), 128); self.assertEqual(cv[5], (200, 10, 20)); self.assertEqual(cv[127], (0, 0, 0))
            with self.assertRaises(device.HwError): designs.image_cells(_os.path.join(d, "missing.png"))


class TestSimIsolation(unittest.TestCase):
    def test_the_simulator_never_uses_the_real_user_state(self):
        import subprocess, sys, os as _os
        env = dict(_os.environ, TRUESENSE_SIM="1", PYTHONPATH=_os.path.join(_os.path.dirname(__file__), "..", "src")); env.pop("TRUESENSE_USERSTATE", None)
        out = subprocess.run([sys.executable, "-c", "from truesense import userstate; print(userstate.PATH)"], env=env, capture_output=True, text=True).stdout.strip()
        self.assertNotIn(".local/state", out); self.assertIn("truesense-sim", out)


class TestPowerManagers(unittest.TestCase):
    def fake(self):
        import tempfile, os as _os
        d = tempfile.mkdtemp(); sysd = d + "/cpu"; _os.makedirs(sysd + "/intel_pstate")
        for n in range(3):
            _os.makedirs(f"{sysd}/cpu{n}/cpufreq")
            open(f"{sysd}/cpu{n}/cpufreq/scaling_governor", "w").write("performance"); open(f"{sysd}/cpu{n}/cpufreq/energy_performance_preference", "w").write("performance")
        open(sysd + "/intel_pstate/no_turbo", "w").write("1")
        for n in range(3): open(f"{sysd}/cpu{n}/cpufreq/cpuinfo_max_freq", "w").write("5000000"); open(f"{sysd}/cpu{n}/cpufreq/scaling_max_freq", "w").write("2100000")      # the ceiling turbo-off left behind
        units = {"auto-cpufreq": {"active": True, "enabled": True}, "tlp": {"active": False, "enabled": False}, "ananicy-cpp": {"active": True, "enabled": True}}; calls = []
        def run(*cmd, timeout=30):
            calls.append(cmd); verb, unit = cmd[1], cmd[-1].replace(".service", ""); u = units.get(unit, {"active": False, "enabled": False})
            if verb == "is-active": return 0, "active" if u["active"] else "inactive"
            if verb == "is-enabled": return 0, "enabled" if u["enabled"] else "disabled"
            if verb == "disable": u["active"] = u["enabled"] = False
            if verb == "enable": u["enabled"] = True
            if verb == "start": u["active"] = True
            return 0, ""
        return sysd, d + "/rec.json", run, units, calls

    def test_takeover_stops_only_known_managers_fixes_the_cpu_and_restore_undoes_it(self):
        from truesense import powermgr as pm
        sysd, rec, run, units, calls = self.fake()
        self.assertEqual([m["unit"] for m in pm.status(run)], ["auto-cpufreq"], "ananicy-cpp and tlp (not running) are not on the list")
        r = pm.takeover(run, sysd, rec); self.assertEqual(r["stopped"], ["auto-cpufreq"]); self.assertEqual(r["failed"], []); self.assertEqual(r["cpu_not_set"], [])
        self.assertFalse(units["auto-cpufreq"]["active"] or units["auto-cpufreq"]["enabled"]); self.assertTrue(units["ananicy-cpp"]["active"], "unrelated services are left alone")
        self.assertEqual(pm.cpu_state(sysd), {"no_turbo": "0", "governor": "powersave", "epp": "balance_performance"})
        self.assertFalse([c for c in calls if c[-1].replace(".service", "") not in pm.UNITS and c[-1].replace(".service", "") != "ananicy-cpp"])
        self.assertEqual(pm.load(rec)["cpu"], {"no_turbo": "1", "governor": "performance", "epp": "performance"})
        self.assertEqual(pm.ceiling_low(sysd), [], "the frequency ceilings left low by turbo-off are raised to each core's maximum")
        self.assertEqual(open(sysd + "/cpu1/cpufreq/scaling_max_freq").read(), "5000000")
        pm.takeover(run, sysd, rec); self.assertEqual(pm.load(rec)["cpu"]["governor"], "performance", "a second takeover keeps the ORIGINAL state to go back to")
        r = pm.restore(run, sysd, rec); self.assertEqual(r["restored"], ["auto-cpufreq"]); self.assertTrue(units["auto-cpufreq"]["active"] and units["auto-cpufreq"]["enabled"])
        self.assertEqual(pm.cpu_state(sysd), {"no_turbo": "1", "governor": "performance", "epp": "performance"}); self.assertEqual(pm.load(rec), {})

    def test_a_hand_edited_record_cannot_touch_other_services_and_failures_are_reported(self):
        import json as _json
        from truesense import powermgr as pm
        sysd, rec, run, units, calls = self.fake()
        open(rec, "w").write(_json.dumps({"units": [{"unit": "sshd", "was_active": True, "was_enabled": True}], "cpu": None})); calls.clear()
        pm.restore(run, sysd, rec); self.assertFalse(calls, "only units from the fixed list are ever started or enabled")
        sysd, rec, run, units, calls = self.fake()
        def stuck(*cmd, timeout=30): return (1, "Access denied") if cmd[1] == "disable" else run(*cmd)
        r = pm.takeover(stuck, sysd, rec); self.assertEqual(r["stopped"], []); self.assertTrue(r["failed"] and "auto-cpufreq" in r["failed"][0])

    def test_daemon_handler_remembers_the_choice(self):
        from truesense import powermgr as pm
        b = TestDaemonLogic.make(self); h = b.handlers(); sysd, rec, run, units, calls = self.fake()
        o = (pm.status, pm.takeover, pm.restore, pm.load)
        pm.status = lambda r=None: o[0](run); pm.takeover = lambda: o[1](run, sysd, rec); pm.restore = lambda: o[2](run, sysd, rec); pm.load = lambda p=None: o[3](rec)
        try:
            self.assertFalse(h["power_managers"](action="status")["on"]); r = h["power_managers"](action="takeover"); self.assertTrue(r["on"]); self.assertEqual(r["stopped"], ["auto-cpufreq"]); self.assertTrue(b.st.get("pm_takeover"))
            units["auto-cpufreq"]["active"] = True; b.tick = 4; b.pm_guard(); self.assertFalse(units["auto-cpufreq"]["active"], "a manager that came back is stopped again")
            r = h["power_managers"](action="restore"); self.assertFalse(r["on"]); self.assertTrue(units["auto-cpufreq"]["active"]); self.assertFalse(b.st.get("pm_takeover"))
            with self.assertRaises(device.HwError): h["power_managers"](action="rm -rf")
        finally: pm.status, pm.takeover, pm.restore, pm.load = o

    def test_a_low_ceiling_is_repaired_even_when_no_manager_is_running(self):
        from truesense import powermgr as pm
        sysd, rec, run, units, calls = self.fake(); units["auto-cpufreq"].update(active=False, enabled=False); open(sysd + "/intel_pstate/no_turbo", "w").write("0")
        for n in range(3): open(f"{sysd}/cpu{n}/cpufreq/scaling_governor", "w").write("schedutil")
        self.assertEqual(len(pm.ceiling_low(sysd)), 3); r = pm.takeover(run, sysd, rec)
        self.assertEqual(r["found"], []); self.assertTrue(r["ceiling_fixed"]); self.assertEqual(pm.cpu_state(sysd)["governor"], "schedutil", "your governor is not touched when nothing was in charge and turbo is on")


class TestUserStateLocking(unittest.TestCase):
    def test_concurrent_writers_never_lose_a_key(self):
        import threading, tempfile, os as _os
        from truesense import userstate
        d = tempfile.mkdtemp(); old = userstate.PATH; userstate.PATH = d + "/user.json"
        try:
            def w(n):
                for i in range(25): userstate.set(f"k{n}_{i}", i)
            ts = [threading.Thread(target=w, args=(n,)) for n in range(6)]; [t.start() for t in ts]; [t.join() for t in ts]
            data = userstate.load(); self.assertEqual(len(data), 6 * 25, "every key written by every thread is still there")
        finally: userstate.PATH = old


class TestUpdateWithoutPolkitAgent(unittest.TestCase):
    def fake_proc(self, *cmds):
        import tempfile, os as _os
        d = tempfile.mkdtemp()
        for i, c in enumerate(cmds, 100):
            _os.makedirs(f"{d}/{i}"); open(f"{d}/{i}/cmdline", "wb").write(c.replace(" ", "\0").encode() + b"\0")
        return d

    def test_polkitd_alone_is_not_a_password_prompt(self):
        from truesense import update
        self.assertFalse(update.polkit_agent_running(self.fake_proc("/usr/lib/polkit-1/polkitd --no-debug", "/usr/bin/zsh -c source snapshot polkit-gnome")))
        self.assertTrue(update.polkit_agent_running(self.fake_proc("/usr/bin/hyprpolkitagent")))
        self.assertTrue(update.polkit_agent_running(self.fake_proc("/usr/lib/polkit-gnome/polkit-gnome-authentication-agent-1")))

    def test_terminal_is_chosen_from_env_then_the_usual_ones(self):
        from truesense import update
        have = {"foot", "xterm"}
        which = lambda n: "/usr/bin/" + n if n in have else None
        self.assertEqual(update.terminal_command("echo hi", {"TERMINAL": "/usr/bin/foot"}, which), ["foot", "sh", "-c", "echo hi"])
        self.assertEqual(update.terminal_command("echo hi", {}, which)[0], "foot"); self.assertEqual(update.terminal_command("x", {"TERMINAL": "xterm"}, which)[0], "xterm")
        self.assertIsNone(update.terminal_command("x", {}, lambda n: None))

    def test_pkexec_only_with_an_agent_and_the_window_falls_back_to_a_terminal(self):
        import os as _os
        from truesense import update
        if _os.geteuid() == 0: self.skipTest("root")
        _w = _os.environ.get("WAYLAND_DISPLAY"); _os.environ["WAYLAND_DISPLAY"] = "wayland-test"
        try:
            self.assertEqual(update.elevate_command("/x/install.sh", gui=True, agent=True)[0], "pkexec")
            self.assertEqual(update.elevate_command("/x/install.sh", gui=True, agent=False)[0], "sudo", "no agent: pkexec would fail without a prompt")
            ran = []; run = lambda cmd: (ran.append(cmd), type("R", (), {"returncode": 0})())[1]
            info = {"error": None, "info": {"tag": "v9.9.9"}}; real_check, real_iv = update.check, update.installed_version
            update.check = lambda force=False, hours=None: info; update.installed_version = lambda path=None: "9.9.9"
            try:
                r = update.apply(None, gui=True, run=run, agent=False, term=lambda inner: ["myterm", "-e", "sh", "-c", inner])
                self.assertEqual(r["via"], "terminal"); self.assertEqual(r["returncode"], 0); self.assertEqual(ran[0][:4], ["myterm", "-e", "sh", "-c"]); self.assertIn("truesense update apply --yes --version 9.9.9", ran[0][-1])
                update.installed_version = lambda path=None: "0.3.5"                               # the terminal closed but nothing was installed: not a success
                self.assertNotEqual(update.apply(None, gui=True, run=run, agent=False, term=lambda inner: ["t", inner])["returncode"], 0)
                with self.assertRaises(device.HwError) as cm: update.apply(None, gui=True, run=run, agent=False, term=lambda inner: None)
                self.assertIn("truesense update apply", str(cm.exception))
            finally: update.check, update.installed_version = real_check, real_iv
        finally:
            if _w is None: _os.environ.pop("WAYLAND_DISPLAY", None)
            else: _os.environ["WAYLAND_DISPLAY"] = _w


class TestKeyboardSpeedScale(unittest.TestCase):
    def sent_speed_byte(self, speed):
        from truesense import keyboard
        keyboard.SIM["packets"].clear(); keyboard.set_effect("wave", "00aec7", speed, 25, 0)
        return [p for p in keyboard.SIM["packets"] if p[0] == 0x08][-1][3]

    def test_speed_scale_covers_the_keyboards_whole_range_and_never_sends_an_invalid_byte(self):
        from truesense import keyboard
        self.assertEqual((keyboard.SPEED_MIN, keyboard.SPEED_MAX), (1, 12))
        self.assertEqual(self.sent_speed_byte(1), 11, "slowest = byte 11 (two steps slower than the old slowest, 9)")
        self.assertEqual(self.sent_speed_byte(12), 0, "fastest = byte 0")
        self.assertEqual([self.sent_speed_byte(s) for s in range(1, 13)], list(range(11, -1, -1)), "every step is one byte step faster than the last")
        for s in range(1, 13): self.assertTrue(0 <= self.sent_speed_byte(s) <= 11, "the keyboard refuses bytes above 11 and turns them into a medium speed")
        for bad in (0, 13, -1, 99):
            with self.assertRaises(device.HwError): keyboard.set_effect("wave", "00aec7", bad, 25, 0)
        with self.assertRaises(device.HwError): keyboard.set_speed(13)
