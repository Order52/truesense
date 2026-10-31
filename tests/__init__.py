# SPDX-License-Identifier: MIT
import os, sys, tempfile
os.environ["HOME"] = tempfile.mkdtemp(prefix="ts-test-home-")      # tests must never touch the real user state, config or lighting memory
os.environ["TRUESENSE_SOCKET"] = "/nonexistent/truesense.sock"       # ... or talk to the real daemon (they once flipped smart fan and the auto profile off)
os.environ["TRUESENSE_SIM"] = "1"                      # every test runs against the firmware simulator: no hardware needed
os.environ["TRUESENSE_PROFILES_DIR"] = os.path.join(os.path.dirname(__file__), "..", "packaging", "profiles")
os.environ["TRUESENSE_OSD"] = "none"
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))
