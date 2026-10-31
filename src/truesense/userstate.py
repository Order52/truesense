# SPDX-License-Identifier: MIT
"""Small per-user memory (~/.local/state/truesense/user.json): what a light was set to before `off`, last keyboard profile, ... Never required: every reader has a default."""
import fcntl
import json
import os
import threading

import tempfile
_LOCK = threading.Lock()
# The simulator (TRUESENSE_SIM=1) must never touch your real settings: the key service running on this machine would act on them.
PATH = os.environ.get("TRUESENSE_USERSTATE") or (os.path.join(tempfile.mkdtemp(prefix="truesense-sim-"), "user.json") if os.environ.get("TRUESENSE_SIM")
                                                  else os.path.expanduser("~/.local/state/truesense/user.json"))


def load():
    try:
        with open(PATH) as f: return json.load(f)
    except (OSError, ValueError): return {}


def get(key, default=None): return load().get(key, default)


def set(key, value):
    """Change one key. Read-modify-write under a lock (threads here, other processes through a lock file), so two writers (the window, the key service, the command line) never lose each other's change."""
    with _LOCK:
        lockf = None
        try:
            os.makedirs(os.path.dirname(PATH), exist_ok=True); lockf = open(PATH + ".lock", "a+"); fcntl.flock(lockf, fcntl.LOCK_EX)  # noqa: SIM115 (held until the finally below; a with-block does not fit a lock that may be unavailable)
        except OSError: lockf = None                                    # no lock possible (read-only disk): carry on, it is only a convenience file
        try:
            d = load(); d[key] = value; tmp = f"{PATH}.{os.getpid()}.tmp"
            with open(tmp, "w") as f: json.dump(d, f)
            os.replace(tmp, PATH)
        except OSError: pass
        finally:
            if lockf: lockf.close()                                     # closing releases the lock
