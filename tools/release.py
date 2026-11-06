#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Release tooling: one place that knows where the version number lives.

  tools/release.py check [--tag vX.Y.Z]     every file agrees on one version (and it matches the tag, when given); CI runs this on every push and on release
  tools/release.py notes X.Y.Z              the release notes of that version (from RELEASE_NOTES.md) as GitHub release text
  tools/release.py prepare X.Y.Z --title T  bump everything, turn "## Unreleased" into this version, run the gate, commit and tag  (--push also pushes: CI then publishes)

`prepare` refuses a dirty tree, a version that is not newer, or empty "Unreleased" notes. Nothing is pushed unless you say --push.
"""
import argparse
import datetime
import os
import re
import subprocess
import sys

ROOT = os.path.realpath(os.path.join(os.path.dirname(__file__), ".."))
SEMVER = r"\d+\.\d+\.\d+"
# (file, regex whose group 1 is a version, how many times it must match)
VERSIONED = [
    ("src/truesense/__init__.py", rf'__version__ = "({SEMVER})"', 1),
    ("README.md", rf"badge/version-({SEMVER})-", 1),
    ("docs/INSTALL.md", rf"TRUESENSE_VERSION=({SEMVER})", 1),
    ("kernel/truesense-wmi/dkms.conf", rf'PACKAGE_VERSION="({SEMVER})"', 1),
    ("kernel/truesense-wmi/truesense_wmi.c", rf'#define TS_VERSION\s+"({SEMVER})"', 1),
    ("tools/aur-publish.sh", rf"tools/aur-publish\.sh ({SEMVER})", 1),
    ("tools/get.sh", rf"TRUESENSE_VERSION=({SEMVER}) \(pin", 1),
    ("packaging/arch/PKGBUILD", rf"^pkgver=({SEMVER})$", 1),
    ("packaging/arch/PKGBUILD", rf"truesense-wmi-({SEMVER})", 2),
    ("packaging/aur/PKGBUILD.template", rf"^pkgver=({SEMVER})$", 1),
    ("packaging/aur/PKGBUILD.template", rf"truesense-wmi-({SEMVER})", 2),
]
FOOTER = """
**Install / update:** `curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash`, or from the window: Settings, Update (on a desktop without a polkit agent a terminal window opens for the password), or `truesense update apply` in a terminal.

Tested on one machine (PH16-71, BIOS V1.18, CachyOS). What is verified and what is not is listed in docs/STATUS.md. Every release asset is listed in SHA256SUMS and carries a build provenance attestation (`gh attestation verify <file> --repo Order52/truesense`).
"""


def read(root, rel):
    with open(os.path.join(root, rel), encoding="utf-8") as f: return f.read()


def write(root, rel, text):
    with open(os.path.join(root, rel), "w", encoding="utf-8") as f: f.write(text)


def parse(v):
    m = re.fullmatch(rf"v?({SEMVER})", v.strip())
    if not m: raise SystemExit(f"not a version: {v!r} (expected X.Y.Z)")
    return tuple(int(x) for x in m.group(1).split("."))


def current(root=ROOT):
    m = re.search(VERSIONED[0][1], read(root, VERSIONED[0][0]))
    if not m: raise SystemExit("cannot find __version__ in src/truesense/__init__.py")
    return m.group(1)


def check(root=ROOT, tag=None):
    """Returns a list of problems (empty = consistent)."""
    want = current(root); problems = []
    for rel, rx, count in VERSIONED:
        try: found = re.findall(rx, read(root, rel), re.M)
        except OSError as e: problems.append(f"{rel}: cannot read ({e.strerror})"); continue
        if len(found) != count: problems.append(f"{rel}: expected {count} version mention(s) like {rx!r}, found {len(found)}")
        problems += [f"{rel}: says {v}, __version__ says {want}" for v in found if v != want]
    notes = read(root, "RELEASE_NOTES.md")
    if not re.search(rf"^## {re.escape(want)} \(", notes, re.M) and "## Unreleased" not in notes: problems.append(f"RELEASE_NOTES.md: no section for {want}")
    if tag and parse(tag) != parse(want): problems.append(f"tag {tag} does not match version {want}")
    return problems


def section(root, version):
    """The notes of one version without its heading, or None."""
    notes = read(root, "RELEASE_NOTES.md")
    m = re.search(rf"^## {re.escape(version)} \([^\n]*\)[^\n]*\n(.*?)(?=^## |\Z)", notes, re.M | re.S)
    return m.group(1).strip("\n") if m else None


def title(root, version):
    m = re.search(rf"^## {re.escape(version)} \(([^)]*)\)(?::\s*(.*))?$", read(root, "RELEASE_NOTES.md"), re.M)
    return (m.group(2) or "").strip() if m else ""


def notes(root, version):
    body = section(root, version)
    if not body: raise SystemExit(f"RELEASE_NOTES.md has no notes for {version}")
    head = f"TrueSense {version} for the Acer Predator Helios 16 (PH16-71)" + (f": {title(root, version)}." if title(root, version) else ".")
    return f"{head}\n\n{body}\n{FOOTER}".rstrip("\n")


def bump(root, new):
    """Rewrite the version only where the table says it lives (never a stray mention of the same number elsewhere in a file)."""
    old = current(root)
    for rel, rx, _count in VERSIONED:
        text = read(root, rel)
        write(root, rel, re.sub(rx, lambda m: m.group(0).replace(m.group(1), new), text, flags=re.M))
    return old


def unreleased(root):
    m = re.search(r"^## Unreleased\n(.*?)(?=^## |\Z)", read(root, "RELEASE_NOTES.md"), re.M | re.S)
    return m.group(1).strip() if m else ""


def retitle(root, new, ttl, today=None):
    today = today or datetime.date.today().isoformat()
    text = read(root, "RELEASE_NOTES.md"); head = f"## {new} ({today})" + (f": {ttl}" if ttl else "")
    write(root, "RELEASE_NOTES.md", text.replace("## Unreleased", head, 1))


def sh(cmd, root=ROOT, check_=True):
    print("  $", " ".join(cmd), flush=True)
    r = subprocess.run(cmd, cwd=root)
    if check_ and r.returncode: raise SystemExit(f"failed: {' '.join(cmd)}")
    return r.returncode


def prepare(root, new, ttl, gate=True, push=False, dry=False):
    old = current(root); parse(new)
    if parse(new) <= parse(old): raise SystemExit(f"{new} is not newer than {old}")
    if not unreleased(root): raise SystemExit('RELEASE_NOTES.md has no notes under "## Unreleased": write what changed first')
    if not ttl: raise SystemExit('give the release a short title: --title "..."')
    git = lambda *a: subprocess.run(["git", *a], cwd=root, capture_output=True, text=True).stdout.strip()
    if git("status", "--porcelain"): raise SystemExit("the working tree has uncommitted changes: commit or stash them first")
    if subprocess.run(["git", "rev-parse", "-q", "--verify", f"refs/tags/v{new}"], cwd=root, capture_output=True).returncode == 0: raise SystemExit(f"tag v{new} already exists")
    print(f"release {old} -> {new}: {ttl}")
    if dry: print("(dry run: nothing changed)"); return
    bump(root, new); retitle(root, new, ttl)
    problems = check(root)
    if problems: raise SystemExit("after bumping:\n  " + "\n  ".join(problems))
    if gate:
        sh(["make", "check"], root); sh([sys.executable, "-m", "unittest", "discover", "-s", "tests", "-t", "."], root)
        if subprocess.run(["ruff", "--version"], capture_output=True).returncode == 0: sh(["ruff", "check", "."], root)
    sh(["git", "add", "-A"], root); sh(["git", "commit", "-q", "-m", f"Release {new}: {ttl}"], root); sh(["git", "tag", "-a", f"v{new}", "-m", f"TrueSense {new}"], root)
    if push: sh(["git", "push", "origin", "HEAD", f"v{new}"], root); print(f"pushed: CI publishes https://github.com/Order52/truesense/releases/tag/v{new}")
    else: print(f"committed and tagged v{new}. Publish with:  git push origin HEAD v{new}")


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter); sub = p.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("check"); c.add_argument("--tag")
    n = sub.add_parser("notes"); n.add_argument("version")
    r = sub.add_parser("prepare"); r.add_argument("version"); r.add_argument("--title", default=""); r.add_argument("--no-gate", action="store_true"); r.add_argument("--push", action="store_true"); r.add_argument("--dry-run", action="store_true")
    a = p.parse_args(argv)
    if a.cmd == "check":
        problems = check(ROOT, a.tag)
        if problems: print("version problems:\n  " + "\n  ".join(problems), file=sys.stderr); return 1
        print(f"version {current()}: every file agrees" + (f" (tag {a.tag})" if a.tag else "")); return 0
    if a.cmd == "notes": print(notes(ROOT, a.version.lstrip("v"))); return 0
    prepare(ROOT, a.version.lstrip("v"), a.title, not a.no_gate, a.push, a.dry_run); return 0


if __name__ == "__main__": sys.exit(main())
