"""Pause/resume the rpredatorsense daemon's fan control by flipping the shared fan-mode file (daemon in 'manual' mode never writes the fans)."""
import contextlib, os, subprocess
from . import config

def _file():
    import pwd
    try: home = pwd.getpwnam(config.load()["user"]).pw_dir
    except KeyError: home = os.path.expanduser("~")
    return home + "/.cache/predator-fan-mode"

def daemon_active():
    return subprocess.run(["systemctl", "is-active", "--quiet", "rpredatorsense.service"]).returncode == 0

@contextlib.contextmanager
def hands_off():
    """Daemon leaves the fans alone inside the block; previous mode restored on exit."""
    f = _file(); old = None; auto_off = os.path.join(os.path.dirname(f), "predator-auto-off"); had_auto_off = os.path.exists(auto_off)
    try: old = open(f).read()
    except OSError: pass
    if daemon_active():
        os.makedirs(os.path.dirname(f), exist_ok=True); open(f, "w").write("manual")
        if not had_auto_off: open(auto_off, "w").close()                   # also pause automatic power-profile switching
        import time; time.sleep(2.5)                       # let the daemon's loop notice (it polls once a second)
    try: yield
    finally:
        if not had_auto_off and os.path.exists(auto_off): os.remove(auto_off)
        if old is not None: open(f, "w").write(old)
        elif os.path.exists(f): os.remove(f)


def hold_profile(minutes=30):
    """A manual power-mode choice (CLI / PREDATOR key) wins over the daemon's automatic profile for `minutes`."""
    d = os.path.dirname(_file()); os.makedirs(d, exist_ok=True)
    with open(d + "/predator-profile-hold", "w") as f: f.write(str(__import__("time").time() + minutes * 60))

def set_fan_mode(mode):
    """'manual' = daemon leaves the fans alone (e.g. while fans are forced to Max); 'smart' = daemon controls them."""
    f = _file(); os.makedirs(os.path.dirname(f), exist_ok=True); open(f, "w").write(mode)
