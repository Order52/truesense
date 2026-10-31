# SPDX-License-Identifier: MIT
"""Small per-user memory (~/.local/state/truesense/user.json): what a light was set to before `off`, last keyboard profile, ... Never required: every reader has a default."""
import json, os

import tempfile
# The simulator (TRUESENSE_SIM=1) must never touch your real settings: the key service running on this machine would act on them.
PATH = os.environ.get("TRUESENSE_USERSTATE") or (os.path.join(tempfile.gettempdir(), f"truesense-sim-{os.getuid()}", "user.json") if os.environ.get("TRUESENSE_SIM")
                                                  else os.path.expanduser("~/.local/state/truesense/user.json"))


def load():
    try:
        with open(PATH) as f: return json.load(f)
    except (OSError, ValueError): return {}


def get(key, default=None): return load().get(key, default)


def set(key, value):
    d = load(); d[key] = value
    try:
        os.makedirs(os.path.dirname(PATH), exist_ok=True)
        with open(PATH + ".tmp", "w") as f: json.dump(d, f)
        os.replace(PATH + ".tmp", PATH)
    except OSError: pass
