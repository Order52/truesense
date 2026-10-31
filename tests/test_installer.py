# SPDX-License-Identifier: MIT
"""Installer logic: distribution detection and package naming, using fake os-release files (no root, nothing installed)."""
import os, subprocess, tempfile, unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CASES = {
    "arch":   ('ID=arch\n', 'arch'),
    "cachy":  ('ID=cachyos\nID_LIKE=arch\n', 'arch'),
    "manjaro": ('ID=manjaro\nID_LIKE=arch\n', 'arch'),
    "ubuntu": ('ID=ubuntu\nID_LIKE=debian\n', 'debian'),
    "mint":   ('ID=linuxmint\nID_LIKE="ubuntu debian"\n', 'debian'),
    "debian": ('ID=debian\n', 'debian'),
    "fedora": ('ID=fedora\n', 'fedora'),
    "nobara": ('ID=nobara\nID_LIKE="rhel centos fedora"\n', 'fedora'),
    "suse":   ('ID=opensuse-tumbleweed\nID_LIKE="opensuse suse"\n', 'suse'),
    "weird":  ('ID=gentoo\n', 'unknown'),
}

def run(expr, osr, kernel="6.1.0-test"):
    with tempfile.NamedTemporaryFile("w", suffix=".osr") as f:
        f.write(osr); f.flush()
        env = dict(os.environ, TRUESENSE_LIB_ONLY="1", TRUESENSE_OS_RELEASE=f.name, TRUESENSE_KERNEL=kernel)
        return subprocess.run(["bash", "-c", f'source "{ROOT}/tools/install.sh"; detect_os; {expr}'], capture_output=True, text=True, env=env).stdout.strip()

class Installer(unittest.TestCase):
    def test_family_detection(self):
        for name, (osr, fam) in CASES.items():
            with self.subTest(name): self.assertEqual(run('echo $FAMILY', osr), fam)
    def test_header_packages(self):
        self.assertEqual(run('headers_pkg', CASES["ubuntu"][0]), "linux-headers-6.1.0-test")
        self.assertEqual(run('headers_pkg', CASES["fedora"][0]), "kernel-devel-6.1.0-test")
        self.assertEqual(run('headers_pkg', CASES["suse"][0], "6.1.0-1-default"), "kernel-default-devel")
        self.assertEqual(run('headers_pkg', CASES["arch"][0]), "linux-headers")
    def test_package_names(self):
        self.assertEqual(run('pkg python', CASES["arch"][0]), "python")
        self.assertEqual(run('pkg python', CASES["ubuntu"][0]), "python3")
        self.assertEqual(run('pkg gui', CASES["suse"][0]), "python3-qt6")
    def test_check_runs_without_root(self):
        r = subprocess.run(["bash", f"{ROOT}/tools/install.sh", "--check"], capture_output=True, text=True)
        self.assertIn("prerequisites", r.stdout); self.assertIn(r.returncode, (0, 1))
    def test_unknown_option(self):
        r = subprocess.run(["bash", f"{ROOT}/tools/install.sh", "--nope"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 2)

if __name__ == "__main__": unittest.main()
