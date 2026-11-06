# SPDX-License-Identifier: MIT
"""Which laptops TrueSense knows, and a probe that learns about the rest WITHOUT changing anything.

Three tiers, because firmware control must never be guessed:
  supported    verified on real hardware (a maintainer or a user report): everything works and is tested
  unverified   an Acer Predator/Nitro we have no verified report for. The kernel module refuses to bind (it can be forced, at your own risk). `truesense probe` only READS, and its output is
               what a maintainer needs to move the model to "supported" in a release
  unsupported  not an Acer: TrueSense has nothing to do here
"""
import glob
import json
import os
import re

from . import __version__

# the verified models: DMI product name -> what was checked. Add a model here only with a verified hardware report (docs/STATUS.md).
SUPPORTED = {
    "Predator PH16-71": {"family": "Predator Helios 16", "verified_bios": ["V1.18"], "report": "docs/STATUS.md"},
}
PREDATOR = re.compile(r"^(Predator|Nitro)\s+[A-Z]{1,4}\d{2}-\d{2}", re.I)
GUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
GAMING_WMI_GUID = "7a4ddfe7-5b5d-40b4-8595-4408e0cc7f56"       # the Acer gaming WMI interface TrueSense talks to (its presence is the first sign a model has it)
DMI = "/sys/class/dmi/id"
# everything the probe may ask the firmware: reads only. A test proves that nothing outside this list is ever sent.
READ_ONLY = ("version", "caps", "sensors", "get_fan", "get_mode", "get_modes", "get_overdrive", "get_boot", "get_kbtimeout", "get_usb", "get_battery 1", "get_battery 2", "get_gpu", "get_rear")
REPORT_URL = "https://github.com/Order52/truesense/issues/new?template=new_model_report.yml"


def _dmi(name, root):
    try:
        with open(os.path.join(root, name)) as f: return f.read().strip()
    except OSError: return ""


def identify(root=DMI):
    """{"vendor", "product", "bios", "bios_date", "tier", "family", "bios_verified", "message"}"""
    vendor, product, bios, date = _dmi("sys_vendor", root), _dmi("product_name", root), _dmi("bios_version", root), _dmi("bios_date", root)
    out = {"vendor": vendor, "product": product or "unknown", "bios": bios or "?", "bios_date": date, "family": "", "bios_verified": None}
    known = SUPPORTED.get(product) if vendor.lower().startswith("acer") else None            # the kernel module matches vendor AND name: so does this
    if known:
        out.update(tier="supported", family=known["family"], bios_verified=bios in known["verified_bios"])
        out["message"] = f"{product}, BIOS {out['bios']}" + ("" if out["bios_verified"] else f" (verified on BIOS {', '.join(known['verified_bios'])}: yours is newer or older, which is expected to work: tell us if it does not)")
    elif vendor.lower().startswith("acer") and PREDATOR.match(product):
        out.update(tier="unverified", family="similar Acer gaming laptop")
        out["message"] = (f"{product} is a similar Acer laptop that TrueSense has not been verified on. Nothing is written to its firmware. `truesense probe` reads what it answers (read-only); "
                          f"share the report so it can be supported: {REPORT_URL}")
    else:
        out.update(tier="unsupported")
        out["message"] = f"'{product or 'unknown'}' ({vendor or 'unknown vendor'}) is not an Acer Predator: TrueSense does not apply"
    return out


def probe(cmd=None, root=DMI, sys_root="/sys"):
    """Everything worth knowing about this machine, read-only. `cmd` is the firmware call (device.cmd; replaced in the tests). Never raises."""
    info = identify(root)
    out = {"truesense": __version__, "model": info, "kernel": os.uname().release, "writes": 0,
           "module": {"loaded": os.path.isdir(f"{sys_root}/module/truesense_wmi"), "device": os.path.exists("/dev/truesense")},
           "wmi_guids": sorted({g for g in (os.path.basename(p)[:36].lower() for p in glob.glob(f"{sys_root}/bus/wmi/devices/*")) if GUID.fullmatch(g)})}
    out["gaming_interface_present"] = GAMING_WMI_GUID in out["wmi_guids"]
    out["firmware"] = {}
    if cmd is None:
        from . import device
        cmd = device.cmd
    if not out["module"]["device"]: out["note"] = "the kernel module is not bound (expected on an unverified model). To let the probe read the firmware: sudo modprobe truesense_wmi force=1   (reads only; undo with: sudo modprobe -r truesense_wmi)"
    else:
        for name in READ_ONLY:
            try: out["firmware"][name] = {"ok": True, "value": cmd(name)}
            except Exception as e: out["firmware"][name] = {"ok": False, "error": str(e)[:120]}
    answered = sum(1 for v in out["firmware"].values() if v["ok"])
    out["answered"] = f"{answered}/{len(READ_ONLY)}" if out["firmware"] else "0/0"
    out["verdict"] = {"supported": "supported: verified on this model", "unverified": "unverified: share this report", "unsupported": "not an Acer Predator"}[info["tier"]]
    return out


def render(p):
    """The probe as text for a person."""
    m = p["model"]; lines = [f"Model     {m['product']} ({m['vendor'] or 'unknown vendor'}), BIOS {m['bios']} {m['bios_date']}".rstrip(), f"Verdict   {p['verdict']}", f"Kernel    {p['kernel']}   module bound: {'yes' if p['module']['device'] else 'no'}",
                             f"WMI       Acer gaming interface {'PRESENT' if p['gaming_interface_present'] else 'not seen'} ({len(p['wmi_guids'])} WMI devices)"]
    for k, v in p["firmware"].items(): lines.append(f"  {k:<14} " + (f"{v['value']}" if v["ok"] else f"(no answer: {v['error']})"))
    if p.get("note"): lines += ["", p["note"]]
    lines += ["", f"Nothing was written ({p['writes']} writes). " + (f"Share it: truesense probe --json, then {REPORT_URL}" if m["tier"] == "unverified" else "")]
    return "\n".join(lines)


def to_json(p): return json.dumps(p, indent=2, default=str)
