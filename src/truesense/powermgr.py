# SPDX-License-Identifier: MIT
"""Other power managers (auto-cpufreq, TLP, tuned, power-profiles-daemon, system76-power) change CPU governors, turbo and power limits behind TrueSense's back.

"TrueSense manages power" (off by default) stops and disables the ones that are running, remembers exactly what it did, puts the CPU back to the kernel defaults
(governor powersave, energy preference balance_performance, turbo on - a manager that was stopped half way can leave turbo OFF, which caps the CPU at its base clock),
and `restore` undoes all of it. Only the fixed list below is ever touched; nothing else is run. The service re-checks every few minutes while the option is on.
"""
import glob, json, os, subprocess
from .device import HwError

UNITS = ("auto-cpufreq", "tlp", "tuned", "tuned-ppd", "power-profiles-daemon", "system76-power")
RECORD = os.environ.get("TRUESENSE_PM_RECORD", "/var/lib/truesense/power_managers.json")
SYS = "/sys/devices/system/cpu"
DEFAULTS = {"governor": "powersave", "epp": "balance_performance", "no_turbo": "0"}


def _run(*cmd, timeout=30):
    try: r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, env={**os.environ, "LC_ALL": "C"}); return r.returncode, (r.stdout or r.stderr).strip()
    except (OSError, subprocess.SubprocessError) as e: return 1, str(e)


def _read(path):
    try:
        with open(path) as f: return f.read().strip()
    except OSError: return None


def _write(path, value):
    try:
        with open(path, "w") as f: f.write(str(value))
        return True
    except OSError: return False


def status(run=_run):
    """Every known manager that is running or enabled."""
    out = []
    for u in UNITS:
        active = run("systemctl", "is-active", u + ".service")[1] == "active"; enabled = run("systemctl", "is-enabled", u + ".service")[1] == "enabled"
        if active or enabled: out.append({"unit": u, "active": active, "enabled": enabled})
    return out


def cpu_state(sys_=SYS):
    """Turbo switch, governor and energy preference as they are now (cpu0 stands for all cores)."""
    return {"no_turbo": _read(f"{sys_}/intel_pstate/no_turbo"), "governor": _read(f"{sys_}/cpu0/cpufreq/scaling_governor"), "epp": _read(f"{sys_}/cpu0/cpufreq/energy_performance_preference")}


def _set_cpu(st, sys_=SYS):
    """Write governor / energy preference to every core and the turbo switch, then read each back. Returns what could not be set."""
    bad = []
    if st.get("no_turbo") is not None and _read(f"{sys_}/intel_pstate/no_turbo") is not None:
        _write(f"{sys_}/intel_pstate/no_turbo", st["no_turbo"])
        if _read(f"{sys_}/intel_pstate/no_turbo") != str(st["no_turbo"]): bad.append("turbo")
    for key, name in (("governor", "scaling_governor"), ("epp", "energy_performance_preference")):
        if st.get(key) is None: continue
        files = glob.glob(f"{sys_}/cpu[0-9]*/cpufreq/{name}")
        for f in files: _write(f, st[key])
        if files and any(_read(f) != st[key] for f in files): bad.append(key)
    return bad


def ceiling_low(sys_=SYS):
    """Cores whose frequency ceiling (scaling_max_freq) is below what the core can do. Turning turbo off makes the kernel lower the ceiling, and turning it on again does not raise it."""
    low = []
    for f in glob.glob(f"{sys_}/cpu[0-9]*/cpufreq/cpuinfo_max_freq"):
        d = os.path.dirname(f); hw, cur = _read(f), _read(d + "/scaling_max_freq")
        if hw and cur and hw.isdigit() and cur.isdigit() and int(cur) < int(hw): low.append(d)
    return low


def fix_ceiling(sys_=SYS):
    """Raise every low ceiling to the core's own maximum (what the service also does when it stops), read back. Returns the cores that did not take it."""
    bad = []
    for d in ceiling_low(sys_):
        _write(d + "/scaling_max_freq", _read(d + "/cpuinfo_max_freq"))
        if _read(d + "/scaling_max_freq") != _read(d + "/cpuinfo_max_freq"): bad.append(os.path.basename(os.path.dirname(d)))
    return bad


def load(path=None):
    try:
        with open(path or RECORD) as f: d = json.load(f)
        return d if isinstance(d, dict) else {}
    except (OSError, ValueError): return {}


def _save(d, path=None):
    path = path or RECORD
    try:
        os.makedirs(os.path.dirname(path), exist_ok=True); tmp = path + ".tmp"
        with open(tmp, "w") as f: json.dump(d, f)
        os.replace(tmp, path)
    except OSError as e: raise HwError(f"could not remember what was stopped ({e}); nothing was changed")


def takeover(run=_run, sys_=SYS, path=None):
    """Stop and disable every running/enabled manager from the list, fix the CPU defaults, and remember it all. Returns a report dict."""
    found = status(run); rec = load(path); units = {u["unit"]: u for u in rec.get("units", [])}
    if not rec.get("cpu"): rec["cpu"] = cpu_state(sys_)                      # the state to go back to on restore, taken before the first change
    for f in found: units.setdefault(f["unit"], {"unit": f["unit"], "was_active": f["active"], "was_enabled": f["enabled"]})
    rec["units"] = list(units.values()); _save(rec, path)                    # remembered BEFORE anything is stopped
    stopped, failed = [], []
    for f in found:
        rc, msg = run("systemctl", "disable", "--now", f["unit"] + ".service")
        if rc != 0 and run("systemctl", "is-active", f["unit"] + ".service")[1] == "active": failed.append(f"{f['unit']}: {msg[:80]}")
        else: stopped.append(f["unit"])
    cpu_bad = _set_cpu(DEFAULTS, sys_) if (found or cpu_state(sys_)["no_turbo"] == "1") else []    # defaults only when a manager was in charge or turbo was left off
    cpu_bad += ["ceiling " + c for c in fix_ceiling(sys_)]                                          # a ceiling left low by turbo being off is raised whatever the reason
    return {"found": [f["unit"] for f in found], "stopped": stopped, "failed": failed, "cpu_not_set": cpu_bad, "cpu": cpu_state(sys_), "ceiling_fixed": not ceiling_low(sys_)}


def restore(run=_run, sys_=SYS, path=None):
    """Start the managers again exactly as they were, and put the CPU settings back."""
    rec = load(path)
    if not rec: return {"restored": [], "cpu": cpu_state(sys_), "note": "nothing was changed by TrueSense"}
    done = []
    for u in rec.get("units", []):
        if u.get("unit") not in UNITS: continue                            # a hand-edited record can never make us touch another service
        if u.get("was_enabled"): run("systemctl", "enable", u["unit"] + ".service")
        if u.get("was_active"): run("systemctl", "start", u["unit"] + ".service")
        done.append(u["unit"])
    bad = _set_cpu(rec["cpu"], sys_) if isinstance(rec.get("cpu"), dict) else []
    try: os.remove(path or RECORD)
    except OSError: pass
    return {"restored": done, "cpu_not_set": bad, "cpu": cpu_state(sys_)}
