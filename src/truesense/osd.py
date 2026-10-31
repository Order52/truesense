"""Short on-screen messages ("Audio output: Headphones"). One implementation for the CLI, key service and daemon.

Style (config [osd] style, env TRUESENSE_OSD overrides): native = the compositor's own notification, then the desktop notification daemon;
noctalia = Noctalia's OSD popup; none = silent. Missing helper programs are never an error.
"""
import glob, os, pwd, subprocess

ICON_NOCT = {"audio": "headphones", "speaker": "device-speaker", "power": "gauge", "fan": "gauge", "mic": "microphone"}


def _style(cfg):
    return os.environ.get("TRUESENSE_OSD") or ((cfg or {}).get("osd", {}).get("style") or "native")


def _env(user):
    """Session environment of `user` (needed when called from the root daemon)."""
    try: pw = pwd.getpwnam(user)
    except KeyError: return None, None
    rt = f"/run/user/{pw.pw_uid}"; env = {"PATH": "/usr/bin:/bin", "HOME": pw.pw_dir, "XDG_RUNTIME_DIR": rt, "DBUS_SESSION_BUS_ADDRESS": f"unix:path={rt}/bus"}
    wl = sorted(glob.glob(rt + "/wayland-*"), key=os.path.getmtime)
    if wl: env["WAYLAND_DISPLAY"] = os.path.basename([w for w in wl if not w.endswith(".lock")][-1]) if [w for w in wl if not w.endswith(".lock")] else "wayland-1"
    sig = sorted(glob.glob(rt + "/hypr/*"), key=os.path.getmtime)
    if sig: env["HYPRLAND_INSTANCE_SIGNATURE"] = os.path.basename(sig[-1])
    return env, pw


def _run(cmd, env=None, as_user=None):
    if as_user and os.geteuid() == 0: cmd = ["runuser", "-u", as_user, "--"] + cmd
    try: return subprocess.run(cmd, env=env, capture_output=True, text=True, timeout=4)
    except (OSError, subprocess.TimeoutExpired): return None


def show(text, icon="audio", cfg=None, user=None):
    style = _style(cfg)
    if style == "none": return False
    env, as_user = None, None
    if os.geteuid() == 0 and user:
        env, pw = _env(user)
        if not env: return False
        as_user = user
    if style == "noctalia":
        r = _run(["noctalia", "msg", "osd-show", ICON_NOCT.get(icon, icon), text], env, as_user); return bool(r and r.returncode == 0)
    r = _run(["hyprctl", "notify", "1", "1800", "rgb(00aec7)", text], env, as_user)
    if r and r.stdout.strip() == "ok": return True
    r = _run(["notify-send", "-a", "TrueSense", "-t", "1800", "TrueSense", text], env, as_user)
    return bool(r and r.returncode == 0)


class Quiet:
    """Context manager: silence Noctalia's own popups while a profile flip makes PipeWire recreate devices (re-enabled afterwards, even on error)."""
    depth = 0

    def __init__(self, settle=0.8): self.settle = settle

    def __enter__(self):
        Quiet.depth += 1
        if Quiet.depth == 1: _run(["noctalia", "msg", "osd-disable"])
        return self

    def __exit__(self, *a):
        if Quiet.depth == 1:
            import time; time.sleep(self.settle); _run(["noctalia", "msg", "osd-enable"])
        Quiet.depth -= 1
