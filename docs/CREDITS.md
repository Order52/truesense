# Credits

TrueSense stands on a lot of other people's work. Thank you to every project below. Where it says *ideas* or *facts*, no code was copied; where a project is a dependency, it is used as a normal installed program or library and is not bundled.

## Projects that helped make TrueSense

| Project | Licence | What it gave us |
|---|---|---|
| [**Linuwu-Sense**](https://github.com/0x7375646F/Linuwu-Sense) by 0x7375646F | GPL-3.0 | The map of the Acer gaming WMI interface for Linux: which GUIDs and methods exist (fans, power modes, rear light, USB charging, battery health), and how arguments are shaped. We read it, then **checked every value on the real laptop** before using it (*facts*). TrueSense's kernel module is a separate, newly written driver. |
| [**AcerUltraX**](https://github.com/fawaz7/AcerUltraX) by fawaz7 | MIT | Acer DTS sound on Linux. TrueSense integrates it (the Audio page, `truesense audio dts`); the sound processing is entirely theirs (*dependency, optional*). |
| [**RNNoise**](https://github.com/xiph/rnnoise) by Xiph.Org / Jean-Marc Valin | BSD-3-Clause | The neural-network noise suppression behind the Purified-Voice-style microphone cleanup. |
| [**noise-suppression-for-voice**](https://github.com/werman/noise-suppression-for-voice) by werman | GPL-3.0 | The LADSPA plugin that lets PipeWire run RNNoise (*dependency, optional*). |
| [**G-Helper**](https://github.com/seerge/g-helper) by seerge | GPL-3.0 | The idea of a small, fast, single-window control app instead of a heavy suite: its compact layout of mode tiles, fans and GPU on one page shaped the window (*ideas*). |
| [**PreySense**](https://github.com/hammadzaigham/PreySense) by hammadzaigham | MIT | The closest thing to this project for Acer Predator laptops on Windows: its dashboard, settings pages, pill toggles, power-limit gating, calibration-before-custom-limits and read-back habits informed the design and the safety model (*ideas*). |
| [**ph16-71-rgb**](https://github.com/Order52/ph16-71-rgb), [**Predator-Key**](https://github.com/Order52/Predator-Key), [**linuwu-sense-cli**](https://github.com/Order52/linuwu-sense-cli) | GPL-3.0, MIT, GPL-3.0 | The maintainer's own earlier tools for this laptop's keyboard lighting, PREDATOR key and Linuwu-Sense control. They proved what was possible and what to fix; TrueSense replaces them in one project. |

## The platform underneath

[The Linux kernel](https://kernel.org) and its `acer-wmi` driver (the reference for the WMI plumbing), [DKMS](https://github.com/dell/dkms), [systemd](https://systemd.io), [PipeWire](https://pipewire.org) and [WirePlumber](https://gitlab.freedesktop.org/pipewire/wireplumber), [ALSA](https://www.alsa-project.org), [Qt](https://www.qt.io) with [PyQt6](https://www.riverbankcomputing.com/software/pyqt/) (the optional window), [Python](https://www.python.org), the NVIDIA management library (NVML) for GPU clocks, and [Noctalia](https://github.com/noctalia-dev) for the bar widgets in `contrib/noctalia`.

## Method

The interface to the laptop was worked out by reading public drivers and documentation, observing this laptop's own behaviour (WMI read-backs, the keyboard's HID reports, the Windows software's own traffic on the maintainer's machine), and checking each claim against the hardware. No proprietary Acer, DTS or Realtek files are included in this repository, and no raw firmware tables are published (they contain the OEM Windows product key).

## Licensing note

TrueSense is **MIT licensed** (the kernel module is dual MIT / GPL-2.0-only). The projects above are credited for ideas, facts or optional run-time use; none of their code is part of this repository. The optional window uses PyQt6 (GPL-3.0) and the optional audio integrations use GPL-3.0 and MIT programs at run time; they are installed separately. See [LEGAL.md](LEGAL.md). If you maintain one of the projects above and something here is not right, please open an issue and we will fix it.

"PredatorSense" and "Predator" are trademarks of Acer Inc., used here only to describe compatibility. TrueSense is not affiliated with or endorsed by Acer.
