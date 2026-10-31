# Troubleshooting

Start with `truesense doctor`: it checks the model, kernel, headers, DKMS, module, device, conflicts, service, service version, user services, udev, config, audio, fans, window and status-bar support, and prints a fix for every problem. In the window: Settings, About, Run check (and Copy report to paste into an issue).

| Symptom | Cause and fix |
|---|---|
| "unknown command ... the running service is older than this app" | the service was not restarted after an upgrade: `sudo tools/install.sh` (or `sudo systemctl restart truesense`) |
| `/dev/truesense` missing | the module did not load: `sudo modprobe truesense_wmi`, then `dmesg | grep truesense`. After a kernel update the health timer rebuilds it within minutes (or `sudo dkms autoinstall`). Unsupported model: the module refuses unless forced |
| The window does not start | `sudo pacman -S python-pyqt6` (Arch); everything else works without it |
| The PREDATOR key does nothing | `systemctl --user status truesense-keys`; the udev rule must give your seat access to the keyboard's hidraw node (`doctor` checks) |
| Fan or mode change is ignored | another tool is fighting TrueSense (`tuned`, `auto-cpufreq`, old scripts). Stop it |
| Headset microphone missing after boot | plug the headset in again (the codec classifies it on the plug event), or set Audio, 3.5 mm jack, Headset to force it. **Never write the codec `reconfig` file** |
| A firmware value reads -1 right after a write | normal: the firmware needs a moment; the tool retries for a second |
| Lighting resets after suspend | set a lighting profile for boot (Lighting, Profiles, At boot): the service re-applies it after every resume |
| Undervolt "readback differs" | the BIOS or microcode locks undervolting on your unit; nothing was changed (it rolls back) |
| `truesense gui` crashes with a Wayland message | harmless Qt noise is silenced; for real crashes run `truesense gui` in a terminal and open an issue with the output |
| Install fails on "kernel headers" | install the headers matching `uname -r` (`linux-cachyos-headers`, `linux-headers`, ...) and re-run the installer |

## Getting help

Open an issue with the output of `truesense doctor --json` and, for service problems, `journalctl -u truesense -b --no-pager | tail -100`. Do **not** attach raw ACPI dumps: they contain firmware tables, including the OEM Windows product key.
