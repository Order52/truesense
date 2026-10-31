"""Runs every CLI command against the simulator. A command may succeed (0) or refuse cleanly (1, e.g. 'daemon not running'); it must never raise."""
import contextlib, io, os, tempfile, unittest
from truesense import cli, config, device, keyprofiles, profiles, userstate

COMMANDS = """
status|sensors|version|doctor-skip
power|power status|power list|power set turbo|power set balanced|power next|power prev|power auto status|power auto off|power hold 5|power release
fan|fan status|fan auto|fan max|fan set 60|fan set 50 70|fan smart status|fan smart on
battery|battery status|battery limit status|battery limit on|battery limit off|battery limit toggle|battery calibration on|battery calibration off|battery usb-charge|battery usb-charge 10|battery usb-charge 30
display overdrive|display overdrive on|display overdrive off|display overdrive toggle
boot sound|boot sound on|boot sound off|boot sound toggle
gpu|gpu status|gpu set auto|gpu set nvidia
rear|rear status|rear on|rear off|rear toggle|rear set breathing --color 00ff88 --brightness 70|rear set static --color ff0000 --speed 2 --direction 0
keyboard|keyboard status|keyboard on|keyboard off|keyboard toggle|keyboard restore|keyboard list-keys|keyboard effect wave|keyboard effect neon --brightness 50|keyboard effect operatingmode --color 00ff00
keyboard effect breathing|keyboard speed 3|keyboard speed 11|keyboard color 00ff00|keyboard paint w=ff0000 arrows=00ff00|keyboard layout|startup|startup show|startup set keyboard last|startup set rear off|startup set keyboard none|startup apply|rear set static --zones ff0000,00ff00,0000ff|rear set static --zones ff0000|keyboard fill ff0000|keyboard gradient ff3300 6600ff|keyboard rainbow|keyboard keys wasd=ff0000 esc=00ff00 --base 101010|keyboard brightness|keyboard brightness 30
keyboard timeout|keyboard timeout off|keyboard timeout on|keyboard operating status|keyboard profile list|keyboard profile save smoke fill 00aec7|keyboard profile save smoke2 gradient ff0000 0000ff|keyboard profile save smoke3 rainbow|keyboard profile load smoke|keyboard profile delete smoke
audio output|audio output status|audio jack|audio jack headset|audio jack headphones|audio jack auto|audio mic|audio mic status|audio mic boost|audio voice|audio voice status|audio speaker-filter status
keys|keys show|keys actions|keys set tap power:turbo|keys set double output-toggle|keys set long exec:true|keys set fn_esc none|keys reset tap|keys reset|keys test notify:hello|keys test power:eco|keys timing|keys timing --long-ms 700
profile|profile list|profile show gaming|profile save smoketest|profile apply quiet|profile apply smoketest|profile delete smoketest|profile next
cpu info|cpu limits
touchpad status
service|config|config show|config validate|config path
completion bash|completion zsh|completion fish
""".replace("|doctor-skip", "").strip()

FAILING_BY_DESIGN = [
    "keyboard speed 0", "keyboard speed 12",
    "rear set breathing --zones ff0000,00ff00,0000ff", "rear set static --zones ff0000,00ff00", "startup set keyboard bogus", "startup set rear profile:x", "keyboard paint w", "keyboard paint nokey=ff0000", "keyboard color zzz", "audio jack speakers",                       # these MUST be refused with exit code 1, never raise
    "fan set 5", "power set ludicrous-no", "keys set tap frobnicate", "keys set tap power:ludicrous", "profile apply no-such-profile", "profile delete gaming", "profile show ../x",
    "keyboard keys wasd", "keyboard profile load", "keyboard profile save bad/name fill 00aec7", "keyboard effect nonsense", "rear set static --color zz", "battery usb-charge 15-no",
]


class Smoke(unittest.TestCase):
    def setUp(self):
        device.SIM.reset(); d = tempfile.mkdtemp()
        self._saved = (config.write_user.__defaults__, profiles.USER_DIR, keyprofiles.DIR, userstate.PATH, config.USER_PATH)
        config.write_user.__defaults__ = (d + "/user.toml",); config.USER_PATH = d + "/user.toml"
        profiles.USER_DIR, keyprofiles.DIR, userstate.PATH = d + "/profiles", d + "/lighting", d + "/user.json"

    def tearDown(self):
        config.write_user.__defaults__, profiles.USER_DIR, keyprofiles.DIR, userstate.PATH, config.USER_PATH = self._saved

    def run_cli(self, argv):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            try: rc = cli.main(argv)
            except SystemExit as e: self.fail(f"argparse rejected {argv}: {err.getvalue().strip()[-120:]}") if e.code == 2 else None; rc = e.code
        return rc, out.getvalue(), err.getvalue()

    def test_every_command_runs_without_raising(self):
        n = 0
        for line in COMMANDS.splitlines():
            for cmd in line.split("|"):
                cmd = cmd.strip()
                if not cmd: continue
                try: rc, out, err = self.run_cli(cmd.split())
                except Exception as e: self.fail(f"`truesense {cmd}` raised {type(e).__name__}: {e}")
                self.assertIn(rc, (0, 1, None), f"`truesense {cmd}` exit {rc}: {err[-150:]}"); n += 1
        self.assertGreater(n, 110)

    def test_bad_input_is_refused_cleanly(self):
        for cmd in FAILING_BY_DESIGN:
            argv = cmd.replace("-no", "").split()
            try: rc, out, err = self.run_cli(argv)
            except AssertionError: continue                                  # argparse itself refused (exit 2): also a clean refusal
            self.assertEqual(rc, 1, f"`truesense {cmd}` should be refused (got {rc})")
            self.assertTrue(err.startswith("error:"), f"`truesense {cmd}`: {err[:80]!r}")

    def test_state_changes_really_happen(self):
        self.run_cli("power set turbo".split()); self.assertEqual(device.SIM.mode, 5)
        self.run_cli("battery limit on".split()); self.assertEqual(device.SIM.battery[1], 1)
        self.run_cli("battery usb-charge 20".split()); self.assertEqual(device.SIM.usb, 20)
        self.run_cli("fan set 70 80".split()); self.assertEqual((device.SIM.fan, device.SIM.fan_duty), ("manual", [70, 80]))
        self.run_cli("rear set wave --brightness 40".split()); self.assertEqual(device.SIM.rear[1], 3)
        self.run_cli("rear off".split()); self.assertEqual(device.SIM.rear[3], 0); self.run_cli("rear on".split()); self.assertEqual(device.SIM.rear[3], 40)
        self.run_cli("profile apply quiet".split()); self.assertEqual(device.SIM.mode, 0)

    def test_keys_config_is_written_and_validated(self):
        self.run_cli("keys set tap power:eco".split()); self.assertEqual(config.load("/nonexistent", config.USER_PATH)["keys"]["tap"], "power:eco")
        rc, _, err = self.run_cli("keys set tap bogus".split()); self.assertEqual(rc, 1)
        self.run_cli("keys reset tap".split()); self.assertEqual(config.load("/nonexistent", config.USER_PATH)["keys"]["tap"], "power-next")


if __name__ == "__main__": unittest.main()
