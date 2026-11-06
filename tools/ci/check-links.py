#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Every relative link and image in the Markdown files must point at a file that exists (and at a heading that exists, for #anchors). Offline: external URLs are not fetched."""
import os
import re
import sys

ROOT = os.path.realpath(os.path.join(os.path.dirname(__file__), "..", ".."))
SKIP_DIRS = {".git", "node_modules", "__pycache__", "collected-hardware-data"}
LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)|!\[[^\]]*\]\(([^)\s]+)\)")


def slug(heading):
    h = re.sub(r"[`*_]", "", heading.strip().lower()); h = re.sub(r"[^\w\- ]", "", h, flags=re.U); return h.replace(" ", "-")


def anchors(path):
    out = set()
    with open(path, encoding="utf-8") as f:
        fence = False
        for line in f:
            if line.startswith("```"): fence = not fence
            m = None if fence else re.match(r"#{1,6}\s+(.*)", line)
            if m: out.add(slug(m.group(1)))
    return out


def main():
    bad = []
    for d, dirs, files in os.walk(ROOT):
        dirs[:] = [x for x in dirs if x not in SKIP_DIRS]
        for name in files:
            if not name.endswith(".md"): continue
            path = os.path.join(d, name)
            with open(path, encoding="utf-8") as f: text = f.read()
            text = re.sub(r"```.*?```", "", text, flags=re.S)
            for m in LINK.finditer(text):
                target = m.group(1) or m.group(2)
                if re.match(r"[a-z][a-z0-9+.-]*:", target, re.I) or target.startswith("//"): continue            # http:, https:, mailto: ...
                rel, _, frag = target.partition("#")
                dest = path if not rel else os.path.normpath(os.path.join(d, rel))
                if not os.path.exists(dest): bad.append(f"{os.path.relpath(path, ROOT)}: {target} (no such file)")
                elif frag and dest.endswith(".md") and slug(frag) not in anchors(dest) and frag not in anchors(dest): bad.append(f"{os.path.relpath(path, ROOT)}: {target} (no such heading)")
    for b in bad: print("BROKEN", b)
    print(f"{len(bad)} broken link(s)"); return 1 if bad else 0


if __name__ == "__main__": sys.exit(main())
