"""`truesense doctor`: check the whole stack and say how to fix what is wrong. Read-only. Exit code 1 if anything FAILs (WARN does not fail).

Every check returns (status, detail, fix) with status in OK / WARN / FAIL / INFO.
"""
import glob, json, os, re, subprocess, sys
from . import util
from . import __version__, config, device, ipc

OK, WARN, FAIL, INFO = "OK", "WARN", "FAIL", "INFO"
LEGACY_MODULES = ("acer_wmi", "linuwu_sense", "acer_rear_wmi")


def sh(*cmd, timeout=6):
    try: return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired): return subprocess.CompletedProcess(cmd, 127, "", "")


def loaded(mod): return os.path.isdir(f"/sys/module/{mod}")
def active(unit, user=False): return sh("systemctl", *(["--user"] if user else []), "is-active", unit).stdout.strip() == "active"


def kver(): return os.uname().release


def c_model():
    m = util.read("/sys/class/dmi/id/product_name").strip() if os.path.exists("/sys/class/dmi/id/product_name") else "unknown"
    bios = util.read("/sys/class/dmi/id/bios_version").strip() if os.path.exists("/sys/class/dmi/id/bios_version") else "?"
    if m == "Predator PH16-71": return OK, f"{m}, BIOS {bios}", ""
    return FAIL, f"'{m}' is not an Acer Predator PH16-71 (BIOS {bios}); TrueSense is only tested on the PH16-71", "do not use it on other models; the kernel module refuses to bind unless forced"


def c_python():
    v = sys.version_info
    return (OK if v >= (3, 11) else FAIL), f"Python {v.major}.{v.minor}", "Python 3.11+ is required (tomllib)"


def c_kernel():
    major, minor = (int(x) for x in re.match(r"(\d+)\.(\d+)", kver()).groups())
    return (OK if (major, minor) >= (6, 6) else WARN), f"kernel {kver()}", "kernels older than 6.6 are untested"


def c_headers():
    ok = os.path.exists(f"/lib/modules/{kver()}/build")
    return (OK if ok else FAIL), f"kernel headers for {kver()}: {'present' if ok else 'MISSING'}", "install your kernel's -headers package (e.g. linux-cachyos-headers), then: sudo dkms autoinstall"


def c_dkms():
    out = sh("dkms", "status", "truesense-wmi").stdout
    if not out.strip(): return FAIL, "truesense-wmi is not registered with DKMS", "sudo tools/install.sh"
    built = re.findall(r",\s*([^,]+),\s*x86_64:\s*installed", out)
    inst = sorted(d.split("/")[-2] for d in glob.glob("/lib/modules/*/build"))
    missing = [k for k in inst if k not in built]
    if kver() not in built: return FAIL, f"not built for the running kernel {kver()}", "sudo dkms autoinstall   (or: sudo tools/install.sh)"
    if missing: return WARN, f"built for {', '.join(built)}; missing for installed kernel(s) {', '.join(missing)}", "sudo dkms autoinstall"
    return OK, f"installed for {', '.join(built)}", ""


def c_module():
    if not loaded("truesense_wmi"): return FAIL, "truesense_wmi is not loaded", "sudo modprobe truesense_wmi   (or reinstall: sudo tools/install.sh)"
    v = util.read("/sys/module/truesense_wmi/version").strip() if os.path.exists("/sys/module/truesense_wmi/version") else "?"
    disk = sh("modinfo", "-F", "version", "truesense_wmi").stdout.strip()
    if disk and v != disk: return WARN, f"loaded {v} but {disk} is installed", "reboot (or: sudo modprobe -r truesense_wmi && sudo modprobe truesense_wmi)"
    return OK, f"truesense_wmi {v} loaded", ""


def c_device():
    if not os.path.exists(device.DEV): return FAIL, f"{device.DEV} missing", "the module did not bind: check `dmesg | grep truesense`"
    if not os.access(device.DEV, os.R_OK | os.W_OK): return WARN, f"{device.DEV} exists but you have no access", "log in on the laptop's seat (uaccess) or use sudo; check /etc/udev/rules.d/70-truesense.rules"
    try: caps = device.cmd("caps")
    except device.HwError as e: return FAIL, str(e), "see dmesg"
    missing = [k for k, v in caps.items() if v != "1"]
    return (OK if not missing else WARN), "interfaces: " + ", ".join(f"{k}={'yes' if v == '1' else 'NO'}" for k, v in caps.items()), "the firmware lacks " + ", ".join(missing) if missing else ""


def c_conflicts():
    bad = [m for m in LEGACY_MODULES if loaded(m)]
    if bad: return FAIL, "conflicting kernel module(s) loaded: " + ", ".join(bad), "sudo tools/migrate-legacy.sh   (they fight over the fans and power modes)"
    blocked = os.path.exists("/etc/modprobe.d/truesense.conf")
    legacy_dkms = [l for l in sh("dkms", "status").stdout.splitlines() if re.search(r"linuwu|acer-rear", l)]
    if legacy_dkms: return WARN, "old DKMS package(s) still registered: " + "; ".join(legacy_dkms)[:80], "sudo tools/migrate-legacy.sh"
    if not blocked: return WARN, "no modprobe block for the old drivers", "sudo tools/install.sh"
    return OK, "no conflicting driver loaded; old drivers are blocked from loading", ""


def c_daemon():
    if not active("truesense.service"): return FAIL, "truesense.service is not running", "sudo systemctl enable --now truesense.service ; journalctl -u truesense -b"
    try: st = ipc.call("state")
    except device.HwError as e: return WARN, f"service running but the control socket does not answer: {e}", "sudo systemctl restart truesense"
    live = st["live"]; age = None
    try: age = __import__("time").time() - os.path.getmtime("/run/truesense/state")
    except OSError: pass
    if live and not live.get("hw_ok", True): return WARN, "daemon cannot reach the kernel device", "check the module (above)"
    if age is not None and age > 10: return WARN, f"daemon state is {age:.0f}s old (stalled?)", "sudo systemctl restart truesense"
    s = st["settings"]
    return OK, f"running; smart_fan={'on' if s['smart_fan'] else 'off'}, auto_profile={'on' if s['auto_profile'] else 'off'}, activity={live.get('activity', '?')}", ""


def c_user_services():
    res = {u: active(u, True) for u in ("truesense-keys.service", "truesense-audio.service")}
    bad = [u for u, a in res.items() if not a]
    return (OK if not bad else WARN), ", ".join(f"{u.split('.')[0]}={'active' if a else 'inactive'}" for u, a in res.items()), "systemctl --user enable --now " + " ".join(bad) if bad else ""


def c_udev():
    ok = os.path.exists("/etc/udev/rules.d/70-truesense.rules") or os.path.exists("/usr/lib/udev/rules.d/70-truesense.rules")
    from .fnkeys import find_node
    node = find_node()
    if not ok: return FAIL, "udev rule missing", "sudo tools/install.sh"
    if not node: return WARN, "keyboard Fn-key interface not found", "is the internal keyboard (04f2:0117) present?"
    return (OK if os.access(node, os.R_OK) else WARN), f"rule installed; Fn-key interface {node} {'readable' if os.access(node, os.R_OK) else 'NOT readable'}", ""


def c_config():
    try: cfg = config.load()
    except ValueError as e: return FAIL, f"config does not parse: {e}", "fix the TOML syntax"
    problems = config.validate(cfg)
    return (OK if not problems else FAIL), ("configuration valid" if not problems else "; ".join(problems)), "edit /etc/truesense/config.toml or ~/.config/truesense/config.toml" if problems else ""


def c_secure_boot():
    sb = sh("mokutil", "--sb-state").stdout.lower()
    signer = sh("modinfo", "-F", "signer", "truesense_wmi").stdout.strip()
    if "enabled" in sb: return (OK if signer else WARN), f"Secure Boot on; module signer: {signer or 'NONE'}", "enrol the DKMS key: sudo mokutil --import /var/lib/dkms/mok.pub, then reboot"
    return INFO, f"Secure Boot off or unknown; module signer: {signer or 'none'}", ""


def c_audio():
    from . import audio
    pw = active("pipewire.service", True)
    bits = [f"PipeWire {'running' if pw else 'NOT running'}", "RNNoise plugin " + ("found" if audio.PLUGIN else "missing (Purified Voice off)"), "AcerUltraX " + ("found" if audio._dts_bin() else "not installed (optional)")]
    try: j = audio.jack_status(); bits.append("jack: headphones " + ("in" if j["headphones"] else "out") + ", headset mic " + ("detected" if j["headset_mic"] else "not detected"))
    except Exception: pass
    return (OK if pw else WARN), "; ".join(bits), "" if pw else "systemctl --user start pipewire"


def c_fans():
    try: st = device.cmd("get_fan")
    except device.HwError: return INFO, "fan state unreadable", ""
    beh = int(st["behavior"], 16)
    mode = {0x41: "firmware auto", 0x82: "MAX", 0xC3: "manual"}.get(beh, f"0x{beh:x}")
    try: smart = ipc.call("state")["settings"]["smart_fan"]
    except device.HwError: smart = None
    if beh == 0x82 and smart: return WARN, "fans are on MAX while the smart curve is on (the daemon should have taken over)", "truesense fan smart on"
    return OK, f"fans: {mode}", ""


CHECKS = [("Model", c_model), ("Python", c_python), ("Kernel", c_kernel), ("Kernel headers", c_headers), ("DKMS", c_dkms), ("Kernel module", c_module), ("Device", c_device),
          ("Conflicts", c_conflicts), ("Daemon", c_daemon), ("User services", c_user_services), ("udev / keyboard", c_udev), ("Config", c_config), ("Secure Boot", c_secure_boot),
          ("Audio", c_audio), ("Fans", c_fans)]


def run(as_json=False, out=sys.stdout):
    res = []
    for title, fn in CHECKS:
        try: st, detail, fix = fn()
        except Exception as e: st, detail, fix = WARN, f"check crashed: {type(e).__name__}: {e}", ""
        res.append({"check": title, "status": st, "detail": detail, "fix": fix})
    fails = sum(r["status"] == FAIL for r in res); warns = sum(r["status"] == WARN for r in res)
    if as_json: print(json.dumps({"version": __version__, "kernel": kver(), "ok": fails == 0, "failures": fails, "warnings": warns, "checks": res}, indent=2), file=out)
    else:
        colour = out.isatty() and not os.environ.get("NO_COLOR"); col = {OK: "32", WARN: "33", FAIL: "31", INFO: "36"}
        print(f"TrueSense {__version__} doctor ({kver()})", file=out)
        for r in res:
            tag = f"\033[{col[r['status']]}m{r['status']:<4}\033[0m" if colour else f"{r['status']:<4}"
            print(f"  [{tag}] {r['check']:<16} {r['detail']}", file=out)
            if r["fix"] and r["status"] in (WARN, FAIL): print(f"         fix: {r['fix']}", file=out)
        print(f"\n{'ALL GOOD' if not fails and not warns else 'OK with warnings' if not fails else 'PROBLEMS FOUND'}: {fails} failure(s), {warns} warning(s)", file=out)
    return 1 if fails else 0
