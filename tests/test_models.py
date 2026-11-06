# SPDX-License-Identifier: MIT
"""Model tiers and the read-only probe: nothing is ever written to a laptop we have not verified."""
import json
import os
import subprocess
import tempfile
import unittest

from truesense import device, models

ROOT = os.path.realpath(os.path.join(os.path.dirname(__file__), ".."))


def dmi(product, vendor="Acer", bios="V1.18"):
    d = tempfile.mkdtemp()
    for n, v in (("product_name", product), ("sys_vendor", vendor), ("bios_version", bios), ("bios_date", "06/09/2026")): open(os.path.join(d, n), "w").write(v + "\n")
    return d


class TestTiers(unittest.TestCase):
    def test_the_verified_model_is_supported_and_a_new_bios_is_only_a_note(self):
        m = models.identify(dmi("Predator PH16-71")); self.assertEqual((m["tier"], m["bios_verified"]), ("supported", True))
        m = models.identify(dmi("Predator PH16-71", bios="V1.25")); self.assertEqual((m["tier"], m["bios_verified"]), ("supported", False)); self.assertIn("V1.18", m["message"])

    def test_similar_acer_gaming_laptops_are_unverified_not_supported(self):
        for p in ("Predator PH16-72", "Predator PH18-71", "Predator PHN16-71", "Predator PT16-51", "Nitro AN16-41", "Predator PHN18-71"):
            m = models.identify(dmi(p)); self.assertEqual(m["tier"], "unverified", p); self.assertIn("truesense probe", m["message"]); self.assertIn("new_model_report", m["message"])

    def test_other_machines_do_not_apply(self):
        for p, v in (("Inspiron 15", "Dell Inc."), ("Aspire 5", "Acer"), ("", ""), ("Predator PH16-71", "Not Acer Ltd")): self.assertEqual(models.identify(dmi(p, v))["tier"], "unsupported", (p, v))
        self.assertEqual(models.identify("/nonexistent")["tier"], "unsupported")

    def test_only_a_verified_report_can_add_a_model(self):
        self.assertEqual(set(models.SUPPORTED), {"Predator PH16-71"}); self.assertTrue(all(v["verified_bios"] for v in models.SUPPORTED.values()))


class TestProbe(unittest.TestCase):
    def sysroot(self, guids=("7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56", "05901221-D566-11D1-B2F0-00A0C9062910-1")):
        d = tempfile.mkdtemp(); os.makedirs(f"{d}/bus/wmi/devices"); [os.makedirs(f"{d}/bus/wmi/devices/{g}") for g in guids]; return d

    def test_it_only_ever_sends_reads(self):
        sent = []
        def cmd(c): sent.append(c); return {"ok": "1"}
        real = os.path.exists; os.path.exists = lambda p: True if p == "/dev/truesense" else real(p)
        try: p = models.probe(cmd=cmd, root=dmi("Predator PH18-71"), sys_root=self.sysroot())
        finally: os.path.exists = real
        self.assertEqual(sent, list(models.READ_ONLY)); self.assertEqual(p["writes"], 0)
        self.assertFalse([c for c in sent if c.startswith("set") or " set" in c or "force" in c]); self.assertTrue(all(c == "version" or c == "caps" or c == "sensors" or c.startswith("get_") for c in sent))

    def test_the_gaming_interface_is_detected_from_the_wmi_devices(self):
        p = models.probe(cmd=lambda c: {}, root=dmi("Predator PH18-71"), sys_root=self.sysroot()); self.assertTrue(p["gaming_interface_present"]); self.assertIn("7a4ddfe7-5b5d-40b4-8595-4408e0cc7f56", p["wmi_guids"])
        p = models.probe(cmd=lambda c: {}, root=dmi("Predator PH18-71"), sys_root=self.sysroot(guids=("05901221-D566-11D1-B2F0-00A0C9062910",))); self.assertFalse(p["gaming_interface_present"])

    def test_without_the_module_it_explains_how_to_read_safely_and_does_not_touch_the_firmware(self):
        called = []; p = models.probe(cmd=lambda c: called.append(c), root=dmi("Predator PH18-71"), sys_root=self.sysroot())
        if not os.path.exists("/dev/truesense"): self.assertEqual(called, []); self.assertIn("force=1", p["note"]); self.assertEqual(p["answered"], "0/0")

    def test_a_firmware_that_does_not_answer_is_reported_not_fatal(self):
        def cmd(c): raise device.HwError("no such command")
        real = os.path.exists; os.path.exists = lambda p: True if p == "/dev/truesense" else real(p)
        try: p = models.probe(cmd=cmd, root=dmi("Predator PH18-71"), sys_root=self.sysroot())
        finally: os.path.exists = real
        self.assertEqual(p["answered"], f"0/{len(models.READ_ONLY)}"); self.assertFalse(p["firmware"]["get_fan"]["ok"])

    def test_the_report_is_json_and_text_and_carries_no_serial_numbers(self):
        p = models.probe(cmd=lambda c: {"v": "1"}, root=dmi("Predator PH18-71"), sys_root=self.sysroot()); text = models.render(p); data = json.loads(models.to_json(p))
        self.assertEqual(data["model"]["product"], "Predator PH18-71"); self.assertIn("Share it", text); self.assertIn("0 writes", text)
        self.assertFalse({"serial", "uuid", "product_serial", "product_uuid"} & set(json.dumps(data).lower().replace('"', " ").replace(":", " ").split()))

    def test_the_command_runs_in_the_simulator(self):
        env = dict(os.environ, TRUESENSE_SIM="1", PYTHONPATH=os.path.join(ROOT, "src"))
        r = subprocess.run(["python3", "-m", "truesense", "probe"], capture_output=True, text=True, env=env, timeout=30); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("Verdict", r.stdout); self.assertIn("Nothing was written", r.stdout)
        r = subprocess.run(["python3", "-m", "truesense", "--json", "probe"], capture_output=True, text=True, env=env, timeout=30); self.assertEqual(json.loads(r.stdout)["writes"], 0)


class TestDoctorModel(unittest.TestCase):
    def test_doctor_grades_the_tiers(self):
        from truesense import doctor
        real = models.identify
        try:
            for product, want in (("Predator PH16-71", doctor.OK), ("Predator PH18-71", doctor.WARN), ("Latitude", doctor.FAIL)):
                models.identify = lambda root=models.DMI, p=product: real(dmi(p)); st, msg, fix = doctor.c_model(); self.assertEqual(st, want, product)
        finally: models.identify = real


if __name__ == "__main__": unittest.main()
