# SPDX-License-Identifier: MIT
"""Service check and repair against a fake systemd: what it starts, what it leaves alone, and that a second run changes nothing."""
import os
import subprocess
import unittest

from truesense import services


class FakeSystem:
    """systemctl, modprobe and dkms as far as the repair cares. `calls` is every command issued."""
    def __init__(self, svc="active", enabled="enabled", module=True, dkms_fixes=False, restart_fixes=True, user=None):
        self.calls = []; self.module = module; self.dkms_fixes = dkms_fixes; self.restart_fixes = restart_fixes; self.socket = svc == "active"
        self.state = {}
        for u in services.SYSTEM_UNITS: self.state[(False, u)] = {"enabled": enabled, "active": svc if u == "truesense.service" else "active"}
        for u in services.USER_UNITS: self.state[(True, u)] = dict({"enabled": "enabled", "active": "active"}, **((user or {}).get(u, {})))

    def run(self, cmd, timeout=40):
        self.calls.append(list(cmd)); out = ""; rc = 0; err = ""
        if cmd[0] == "systemctl":
            user = "--user" in cmd; rest = [c for c in cmd[1:] if c != "--user"]; verb = rest[0]; unit = rest[-1]; st = self.state.get((user, unit))
            if verb in ("is-enabled", "is-active"): out = (st or {}).get("enabled" if verb == "is-enabled" else "active", "not-found"); rc = 0 if st else 4
            elif verb in ("enable", "start", "restart") and st is None: rc = 1; err = f"Unit {unit} does not exist"
            elif verb == "enable" and st is not None:
                st["enabled"] = "enabled"
                if "--now" in rest: st["active"] = "active"
            elif verb == "start" and st is not None: st["active"] = "active"
            elif verb == "restart" and st is not None:
                st["active"] = "active" if self.restart_fixes else "failed"; self.socket = self.restart_fixes
        elif cmd[0] == "dkms" and self.dkms_fixes: self.module_after_dkms = True
        elif cmd[0] == "modprobe" and getattr(self, "module_after_dkms", False): self.module = True
        return subprocess.CompletedProcess(cmd, rc, out + "\n" if out else "", err)

    def exists(self, p): return {"/dev/truesense": self.module, services.SOCKET: self.socket}.get(p, False)


def kinds(calls): return {c[0] for c in calls}


class TestCheck(unittest.TestCase):
    def test_a_healthy_system_is_all_ok(self):
        f = FakeSystem(); rows = services.check(f.run, f.exists, ping=lambda: True)
        self.assertTrue(all(r["ok"] for r in rows), rows); self.assertEqual(len(rows), len(services.SYSTEM_UNITS) + len(services.USER_UNITS) + 2)
        self.assertEqual({c[0] for c in f.calls}, {"systemctl"}); self.assertFalse([c for c in f.calls if c[1] in ("start", "restart", "enable")], "check never changes anything")

    def test_every_problem_is_listed_with_its_fix(self):
        f = FakeSystem(svc="failed", module=False, user={"truesense-keys.service": {"active": "inactive"}}); rows = services.check(f.run, f.exists, ping=lambda: True)
        bad = {r["unit"]: r for r in rows if not r["ok"]}; self.assertIn("truesense.service", bad); self.assertIn("kernel module (/dev/truesense)", bad); self.assertIn("truesense-keys.service", bad)
        self.assertIn("sudo truesense service repair", bad["truesense.service"]["fix"]); self.assertEqual(bad["truesense-keys.service"]["fix"], "truesense service repair", "a user service is fixed without sudo")
        self.assertIn("!!", services.render(rows))

    def test_a_socket_that_exists_but_does_not_answer_is_flagged(self):
        f = FakeSystem()
        def silent(): raise OSError("timeout")
        rows = services.check(f.run, f.exists, ping=silent); sock = next(r for r in rows if r["unit"] == "control socket"); self.assertFalse(sock["ok"]); self.assertEqual(sock["active"], "silent")


class TestRepairSystem(unittest.TestCase):
    def repair(self, f, **kw): return services.repair_system(f.run, f.exists, sleep=lambda s: None, kernel="6.1.0-test", ping=kw.pop("ping", lambda: True), **kw)

    def test_a_healthy_system_is_left_alone(self):
        f = FakeSystem(); steps = self.repair(f); self.assertTrue(all(ok for _, ok in steps), steps)
        self.assertFalse([c for c in f.calls if c[:2] in (["systemctl", "restart"], ["systemctl", "enable"], ["systemctl", "start"]) or c[0] in ("dkms", "modprobe")], f.calls)
        self.assertTrue(any("left alone" in s for s, _ in steps))

    def test_a_dead_daemon_is_restarted_and_awaited(self):
        f = FakeSystem(svc="failed"); answers = iter([False, False, True, True, True, True, True, True])
        steps = self.repair(f, ping=lambda: next(answers)); self.assertTrue(all(ok for _, ok in steps), steps); self.assertTrue([c for c in f.calls if c[:2] == ["systemctl", "restart"]])
        self.assertTrue(any("answers" in s for s, _ in steps))

    def test_a_disabled_unit_is_enabled(self):
        f = FakeSystem(enabled="disabled"); self.repair(f); self.assertIn(["systemctl", "enable", "truesense.service"], f.calls); self.assertIn(["systemctl", "enable", "truesense-health.timer"], f.calls)

    def test_a_missing_module_is_rebuilt_with_dkms_for_this_kernel_then_loaded(self):
        f = FakeSystem(module=False, dkms_fixes=True); steps = self.repair(f)
        self.assertIn(["dkms", "autoinstall", "-k", "6.1.0-test"], f.calls); self.assertTrue(f.module); self.assertTrue(any("kernel module loaded" in s and ok for s, ok in steps))

    def test_a_module_that_never_appears_is_a_clear_failure(self):
        f = FakeSystem(module=False, dkms_fixes=False); steps = self.repair(f); bad = [s for s, ok in steps if not ok]; self.assertTrue(any("kernel module still missing" in s for s in bad)); self.assertTrue(any("supported laptop" in s for s in bad))

    def test_a_daemon_that_cannot_start_is_reported_with_where_to_look(self):
        f = FakeSystem(svc="failed", restart_fixes=False); steps = self.repair(f, ping=lambda: False); self.assertTrue(any("journalctl -u truesense" in s and not ok for s, ok in steps))

    def test_the_second_run_changes_nothing(self):
        f = FakeSystem(svc="failed", enabled="disabled"); first = self.repair(f); n = len(f.calls); f.calls.clear(); self.repair(f)
        self.assertFalse([c for c in f.calls if c[:2] in (["systemctl", "restart"], ["systemctl", "enable"], ["systemctl", "start"]) or c[0] in ("dkms", "modprobe")], "idempotent"); self.assertTrue(first and n)

    def test_the_repair_only_ever_runs_systemctl_modprobe_and_dkms(self):
        f = FakeSystem(svc="failed", enabled="disabled", module=False, dkms_fixes=True); self.repair(f); services.repair_user(f.run); self.assertLessEqual(kinds(f.calls), {"systemctl", "modprobe", "dkms"})


class TestRepairUser(unittest.TestCase):
    def test_stopped_services_are_enabled_and_started_and_running_ones_left_alone(self):
        f = FakeSystem(user={"truesense-keys.service": {"active": "inactive", "enabled": "disabled"}}); steps = services.repair_user(f.run)
        self.assertIn(["systemctl", "--user", "enable", "--now", "truesense-keys.service"], f.calls); self.assertEqual(sum(1 for c in f.calls if "enable" in c), 1, "only the broken one"); self.assertTrue(all(ok for _, ok in steps))
        f.calls.clear(); steps = services.repair_user(f.run); self.assertFalse([c for c in f.calls if "enable" in c]); self.assertTrue(any("already running" in s for s, _ in steps))

    def test_a_unit_that_is_not_installed_is_reported_not_raised(self):
        f = FakeSystem(); del f.state[(True, "truesense-audio.service")]; steps = services.repair_user(f.run); self.assertTrue(any("truesense-audio.service" in s and not ok for s, ok in steps))


class TestCli(unittest.TestCase):
    @unittest.skipIf(os.geteuid() == 0, "needs a non-root user")
    def test_the_system_part_refuses_without_root(self):
        env = dict(os.environ, TRUESENSE_SIM="1", PYTHONPATH=os.path.join(os.path.dirname(__file__), "..", "src"))
        r = subprocess.run(["python3", "-m", "truesense", "service", "repair", "--system"], capture_output=True, text=True, env=env, timeout=30); self.assertEqual(r.returncode, 1); self.assertIn("needs root", r.stderr + r.stdout)


if __name__ == "__main__": unittest.main()
