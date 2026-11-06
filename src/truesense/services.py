# SPDX-License-Identifier: MIT
"""Service handling: `truesense service check` (read-only) and `truesense service repair` (idempotent: running it twice changes nothing the second time).

The system part (the daemon and its health timer, the kernel module) needs root; the user part (key service, audio service, start-up, update timer) runs as you.
Every step says what it did, and the repair never touches the fans or any setting: it only gets the machinery running again.
"""
import os
import subprocess
import time

SYSTEM_UNITS = ("truesense.service", "truesense-health.timer")
USER_UNITS = ("truesense-keys.service", "truesense-audio.service", "truesense-startup.service", "truesense-update.timer")
SOCKET = "/run/truesense/truesense.sock"


def run(cmd, timeout=40):
    try: return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as e: return subprocess.CompletedProcess(cmd, 127, "", str(e))


def _sc(user): return ["systemctl", "--user"] if user else ["systemctl"]


def unit_state(unit, user=False, runner=run):
    enabled = runner([*_sc(user), "is-enabled", unit]).stdout.strip() or "unknown"
    active = runner([*_sc(user), "is-active", unit]).stdout.strip() or "unknown"
    return {"unit": unit, "scope": "user" if user else "system", "enabled": enabled, "active": active}


def _wanted(st):
    """What a healthy unit looks like: enabled (or static/linked), and for a service active; a timer is active when waiting."""
    return st["enabled"] in ("enabled", "static", "linked", "enabled-runtime", "alias") and st["active"] == "active"


def check(runner=run, exists=os.path.exists, ping=None):
    """Read-only: [{"unit", "scope", "enabled", "active", "ok", "fix"}] plus the module and the control socket."""
    rows = []
    for user, units in ((False, SYSTEM_UNITS), (True, USER_UNITS)):
        for u in units:
            st = unit_state(u, user, runner); st["ok"] = _wanted(st)
            st["fix"] = "" if st["ok"] else ("truesense service repair" if user else "sudo truesense service repair"); rows.append(st)
    mod = exists("/dev/truesense"); rows.append({"unit": "kernel module (/dev/truesense)", "scope": "system", "enabled": "-", "active": "present" if mod else "missing", "ok": mod, "fix": "" if mod else "sudo truesense service repair"})
    sock = exists(SOCKET); answered = None
    if sock:
        try: answered = (ping or _ping)()
        except Exception: answered = False
    rows.append({"unit": "control socket", "scope": "system", "enabled": "-", "active": "answers" if answered else ("silent" if sock else "missing"), "ok": bool(answered), "fix": "" if answered else "sudo truesense service repair"})
    return rows


def _ping():
    from . import ipc
    return ipc.call("ping", timeout=3) == "pong"


def repair_system(runner=run, exists=os.path.exists, sleep=time.sleep, kernel=None, ping=None, wait_s=15):
    """As root. Returns [(what, ok)]; never raises."""
    steps = []; kernel = kernel or os.uname().release

    def do(what, cmd, accept=(0,)):
        r = runner(cmd); ok = r.returncode in accept; steps.append((what + ("" if ok else f" ({(r.stderr or r.stdout).strip()[:100] or 'failed'})"), ok)); return ok
    do("systemd re-reads the unit files", ["systemctl", "daemon-reload"])
    if not exists("/dev/truesense"):
        runner(["modprobe", "truesense_wmi"])
        if not exists("/dev/truesense"):
            do(f"rebuilt the kernel module for {kernel} (DKMS)", ["dkms", "autoinstall", "-k", kernel]); runner(["modprobe", "truesense_wmi"])
        steps.append(("kernel module loaded" if exists("/dev/truesense") else "kernel module still missing: is this a supported laptop? (truesense doctor)", exists("/dev/truesense")))
    for u in SYSTEM_UNITS:
        st = unit_state(u, False, runner)
        if st["enabled"] not in ("enabled", "static", "linked", "alias"): do(f"enabled {u}", ["systemctl", "enable", u])
    runner(["systemctl", "reset-failed", "truesense.service"])
    svc = unit_state("truesense.service", False, runner); timer = unit_state("truesense-health.timer", False, runner)
    if timer["active"] != "active": do("started the health timer", ["systemctl", "start", "truesense-health.timer"])
    healthy = svc["active"] == "active" and exists(SOCKET) and _answers(ping)
    if healthy: steps.append(("truesense.service is running and answers: left alone", True))
    else:
        do("restarted truesense.service", ["systemctl", "restart", "truesense.service"])
        for _ in range(int(wait_s / 0.5)):
            if exists(SOCKET) and _answers(ping): break
            sleep(0.5)
        steps.append(("the service answers on its control socket" if (exists(SOCKET) and _answers(ping)) else "the service does not answer: see  journalctl -u truesense -b", bool(exists(SOCKET) and _answers(ping))))
    return steps


def _answers(ping):
    try: return bool((ping or _ping)())
    except Exception: return False


def repair_user(runner=run):
    """As the user. Enables what is not enabled and starts what is not running; a running unit is left alone."""
    steps = []
    r = runner(["systemctl", "--user", "daemon-reload"]); steps.append(("user systemd re-reads the unit files", r.returncode == 0))
    for u in USER_UNITS:
        st = unit_state(u, True, runner)
        if _wanted(st): continue
        r = runner(["systemctl", "--user", "enable", "--now", u]); ok = r.returncode == 0 or unit_state(u, True, runner)["active"] == "active"
        steps.append((f"enabled and started {u}" if ok else f"could not start {u} ({(r.stderr or r.stdout).strip()[:90] or 'is it installed?'})", ok))
    if len(steps) == 1: steps.append(("every user service is already running", True))
    return steps


def render(rows):
    out = []
    for r in rows: out.append(f"  [{'ok ' if r['ok'] else '!! '}] {r['unit']:<34} {r['scope']:<6} {r['enabled']:<9} {r['active']}" + (f"   -> {r['fix']}" if r["fix"] else ""))
    return "\n".join(out)
