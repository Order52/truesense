import os, sys
os.environ["TRUESENSE_SIM"] = "1"                      # every test runs against the firmware simulator: no hardware needed
os.environ["TRUESENSE_PROFILES_DIR"] = os.path.join(os.path.dirname(__file__), "..", "packaging", "profiles")
os.environ["TRUESENSE_OSD"] = "none"
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))
