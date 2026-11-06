# Troubleshooting

Start with `truesense doctor`: it checks the model, kernel, headers, DKMS, module, device, conflicts, service, service version, user services, udev, config, audio, fans, window and status-bar support, and prints a fix for every problem. In the window: Settings, About, Run check (and Copy report to paste into an issue).

| Symptom | Cause and fix |
|---|---|
| "unknown command ... the running service is older than this app" | the service was not restarted after an upgrade: `sudo tools/install.sh` (or `sudo systemctl restart truesense`) |
| `/dev/truesense` missing | the module did not load: `sudo modprobe truesense_wmi`, then `dmesg | grep truesense`. After a kernel update the health timer rebuilds it within minutes (or `sudo dkms autoinstall`). Unsupported model: the module refuses unless forced |
| The window does not start | `sudo pacman -S python-pyqt6` (Arch); everything else works without it |
| The PREDATOR key does nothing | `systemctl --user status truesense-keys`; the udev rule must give your seat access to the keyboard's hidraw node (`doctor` checks) |
| Fan or mode change is ignored | another tool is fighting TrueSense (`tuned`, `auto-cpufreq`, old scripts). Stop it |
| No sound at all (only an `AcerUltraX` output, `doctor` says NO SOUND OUTPUT) | `truesense audio repair`: WirePlumber built the sound card without its speaker profiles at boot; details below |
| Headset microphone missing after boot | plug the headset in again (the codec classifies it on the plug event), or set Audio, 3.5 mm jack, Headset to force it. **Never write the codec `reconfig` file** |
| A firmware value reads -1 right after a write | normal: the firmware needs a moment; the tool retries for a second |
| Lighting resets after suspend | set a lighting profile for boot (Lighting, Profiles, At boot): the service re-applies it after every resume |
| Undervolt "readback differs" | the BIOS or microcode locks undervolting on your unit; nothing was changed (it rolls back) |
| `truesense gui` crashes with a Wayland message | harmless Qt noise is silenced; for real crashes run `truesense gui` in a terminal and open an issue with the output |
| Install fails on "kernel headers" | install the headers matching `uname -r` (`linux-cachyos-headers`, `linux-headers`, ...) and re-run the installer |


## There is no sound at all (the DTS sink is the only output)

**Symptom:** nothing plays; `wpctl status` shows only `AcerUltraX` under Sinks, `pactl list cards` shows the `sof-hda-dsp` card on profile `off` (it offers only `off` and `pro-audio`), `truesense doctor` says `NO SOUND OUTPUT`, and the kernel log has `sof-audio-pci-intel-tgl ... ipc tx error ... STREAM_PCM_PARAMS ipc failed` around the time the card was probed at boot.

**Cause (seen on the PH16-71):** WirePlumber probed the card while the sound DSP rejected a stream, so it built the card without its HiFi (speaker / headphone) profiles. The hardware and the ALSA UCM files were fine: direct playback with `aplay -D hw:1,0 ...` worked.

**Fix:** `truesense audio repair` (or Audio, *Fix sound* in the window). It restarts WirePlumber so it probes the card again, then verifies that a hardware output exists. The login audio service does the same by itself 20 s and 90 s after login when the output is missing. Nothing is written to the sound chip, and `reconfig` is never touched.

If it does not help: `journalctl -k -b | grep -i sof` for the real error, a reboot, and check that `sof-firmware`, `alsa-ucm-conf` and your kernel were updated together.

## Getting help

Open an issue with the output of `truesense doctor --json` and, for service problems, `journalctl -u truesense -b --no-pager | tail -100`. Do **not** attach raw ACPI dumps: they contain firmware tables, including the OEM Windows product key.

## DTS does nothing on my USB or Bluetooth headset

`acerultrax-cli status` says "DTS: on", but `truesense audio dts devices` shows the headset on **Plain**: AcerUltraX only gives the speakers, the 3.5 mm jack and known Bluetooth headphones a profile by default. TrueSense fixes this by itself (at login and when you plug the headset in); to do it now run `truesense audio dts all`. Pick the sound with `truesense audio dts mode music|movie|voice|game1|game2|game3` (it applies to every output).
