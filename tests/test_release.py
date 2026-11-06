# SPDX-License-Identifier: MIT
"""tools/release.py: the one place that knows where the version number lives. Everything runs on a throw-away copy of the tree, never on the real repository."""
import contextlib
import importlib.util
import io
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = os.path.realpath(os.path.join(os.path.dirname(__file__), ".."))
spec = importlib.util.spec_from_file_location("release_tool", os.path.join(ROOT, "tools", "release.py")); rel = importlib.util.module_from_spec(spec); spec.loader.exec_module(rel)


def tree(unreleased="**New thing**\n- it works\n"):
    """A minimal copy of the repo: every versioned file, release notes and a git history."""
    d = tempfile.mkdtemp(prefix="ts-release-")
    for f in sorted({r for r, _, _ in rel.VERSIONED}):
        os.makedirs(os.path.dirname(os.path.join(d, f)), exist_ok=True); shutil.copy(os.path.join(ROOT, f), os.path.join(d, f))
    cur = rel.current(d)
    with open(os.path.join(d, "RELEASE_NOTES.md"), "w") as f: f.write(f"# Release notes\n\n## Unreleased\n\n{unreleased}\n## {cur} (2026-01-01): the old one\n\n- old stuff, see also 0.1.0\n\n## 0.0.1 (2025-01-01)\n\n- first\n")
    git = lambda *a: subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@t", *a], cwd=d, check=True, capture_output=True)
    git("init", "-q", "-b", "main"); git("config", "user.name", "t"); git("config", "user.email", "t@t"); git("config", "commit.gpgsign", "false"); git("config", "tag.gpgsign", "false"); git("add", "-A"); git("commit", "-q", "-m", "start"); return d


class TestVersionTable(unittest.TestCase):
    def test_the_real_repository_is_consistent(self): self.assertEqual(rel.check(ROOT), [])

    def test_a_file_that_is_out_of_step_is_named(self):
        d = tree(); p = os.path.join(d, "tools/get.sh"); s = open(p).read(); open(p, "w").write(s.replace(rel.current(d), "0.0.9", 1))
        problems = rel.check(d); self.assertEqual(len(problems), 1); self.assertIn("tools/get.sh", problems[0]); self.assertIn("0.0.9", problems[0])

    def test_a_tag_must_match_the_version(self):
        d = tree(); self.assertEqual(rel.check(d, "v" + rel.current(d)), []); self.assertIn("does not match", rel.check(d, "v9.9.9")[0])

    def test_a_missing_mention_is_a_problem_too(self):
        d = tree(); p = os.path.join(d, "packaging/arch/PKGBUILD"); s = open(p).read(); open(p, "w").write(s.replace(f"pkgver={rel.current(d)}", "pkgver=banana"))
        self.assertTrue(any("PKGBUILD" in x and "expected 1" in x for x in rel.check(d)))


class TestBumpAndNotes(unittest.TestCase):
    def test_bump_changes_every_listed_place_and_nothing_else(self):
        d = tree(); old = rel.current(d); p = os.path.join(d, "README.md"); s = open(p).read(); open(p, "w").write(s + f"\nSee release {old} for details.\n")
        rel.bump(d, "9.8.7"); self.assertEqual(rel.current(d), "9.8.7"); self.assertEqual(rel.check(d), [])
        self.assertIn(f"See release {old} for details.", open(p).read(), "a mention outside the version table is left alone")
        for f in ("kernel/truesense-wmi/dkms.conf", "kernel/truesense-wmi/truesense_wmi.c", "packaging/arch/PKGBUILD", "packaging/aur/PKGBUILD.template"): self.assertIn("9.8.7", open(os.path.join(d, f)).read(), f)

    def test_notes_of_a_version_are_cut_out_with_the_install_footer(self):
        d = tree(); cur = rel.current(d); n = rel.notes(d, cur)
        self.assertTrue(n.startswith(f"TrueSense {cur} for the Acer Predator Helios 16 (PH16-71): the old one.")); self.assertIn("old stuff", n); self.assertNotIn("first", n); self.assertNotIn("New thing", n)
        self.assertIn("get.sh | sudo bash", n); self.assertIn("SHA256SUMS", n)
        with self.assertRaises(SystemExit): rel.notes(d, "0.0.2")

    def test_the_notes_of_the_real_current_release_exist_and_are_not_empty(self): self.assertGreater(len(rel.notes(ROOT, rel.current(ROOT))), 200)


class TestPrepare(unittest.TestCase):
    def run_prepare(self, d, new, title="a title", **kw):
        with contextlib.redirect_stdout(io.StringIO()): return rel.prepare(d, new, title, gate=False, **kw)

    def test_prepare_bumps_retitles_commits_and_tags(self):
        d = tree(); self.run_prepare(d, "9.8.7", "shiny")
        self.assertEqual(rel.current(d), "9.8.7"); self.assertEqual(rel.check(d, "v9.8.7"), [])
        notes = open(os.path.join(d, "RELEASE_NOTES.md")).read(); self.assertIn("## 9.8.7 (", notes); self.assertIn(": shiny", notes); self.assertNotIn("## Unreleased", notes)
        self.assertEqual(subprocess.run(["git", "tag"], cwd=d, capture_output=True, text=True).stdout.split(), ["v9.8.7"])
        self.assertIn("Release 9.8.7: shiny", subprocess.run(["git", "log", "-1", "--format=%s"], cwd=d, capture_output=True, text=True).stdout)
        self.assertEqual(subprocess.run(["git", "status", "--porcelain"], cwd=d, capture_output=True, text=True).stdout, "")

    def test_it_refuses_what_would_make_a_bad_release(self):
        d = tree()
        with self.assertRaises(SystemExit) as c: self.run_prepare(d, rel.current(d)); self.assertIn("not newer", str(c.exception))
        with self.assertRaises(SystemExit) as c: self.run_prepare(d, "0.0.1"); self.assertIn("not newer", str(c.exception))
        with self.assertRaises(SystemExit) as c: self.run_prepare(d, "9.8.7", title=""); self.assertIn("title", str(c.exception))
        with self.assertRaises(SystemExit): self.run_prepare(d, "banana")
        open(os.path.join(d, "dirty.txt"), "w").write("x")
        with self.assertRaises(SystemExit) as c: self.run_prepare(d, "9.8.7"); self.assertIn("uncommitted", str(c.exception))
        e = tree(unreleased="")
        with self.assertRaises(SystemExit) as c: self.run_prepare(e, "9.8.7"); self.assertIn("Unreleased", str(c.exception))

    def test_an_existing_tag_is_refused_and_a_dry_run_changes_nothing(self):
        d = tree(); subprocess.run(["git", "tag", "v9.8.7"], cwd=d, check=True)
        with self.assertRaises(SystemExit) as c: self.run_prepare(d, "9.8.7"); self.assertIn("exists", str(c.exception))
        e = tree(); before = rel.current(e); self.run_prepare(e, "9.8.8", dry=True); self.assertEqual(rel.current(e), before)


if __name__ == "__main__": unittest.main()
