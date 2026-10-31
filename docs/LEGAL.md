# Legal notes

> This page explains how TrueSense is licensed and what was done to keep it clean. **It is not legal advice.** If you need certainty (for example before distributing commercially), ask a lawyer in your country.

## Licence

TrueSense is released under the **MIT licence** ([LICENSE](../LICENSE)). The kernel module in `kernel/truesense-wmi/` is **dual licensed MIT / GPL-2.0-only** (`MODULE_LICENSE("Dual MIT/GPL")`) so that the Linux kernel accepts it as GPL-compatible; you may use it under either licence. Every source file carries an SPDX identifier.

Contributions are accepted under the same terms. Please do not submit code you do not have the right to publish.

## Where the code and data come from

- The code is original work written for this project. Facts about how the laptop behaves (WMI GUIDs and method numbers, argument encodings, keyboard packet formats; see [PROTOCOL.md](PROTOCOL.md)) were gathered by reading public Linux drivers and documentation and by checking every value against the maintainer's own laptop. Facts about an interface are not copyrightable expression, and no code was copied from the projects listed in [CREDITS.md](CREDITS.md). If you believe something here is derived from your work, please say so (see "Contact").
- The key map (`src/truesense/data/keymap_ph16-71_verified.json`) is the maintainer's own measurement of which lighting cell is which key.
- The logo, icons and the picture of the keyboard in the window are drawn by the project; no vendor image, photograph, font or logo is included.
- **No vendor material is included**: no Acer, Realtek, DTS, Intel, NVIDIA or Microsoft software, drivers, firmware, firmware tables, documentation or captured vendor files. Raw firmware dumps are never published (they can contain licence keys).

## Interoperability, not circumvention

TrueSense uses the laptop's documented-by-observation interfaces (ACPI-WMI, USB HID, standard Linux interfaces) to control hardware you own. It does not defeat access controls or copy protection, does not modify or redistribute firmware, and does not require or ship any vendor software. Many jurisdictions permit studying the interfaces of hardware you own in order to make your own software work with it (for example EU Directive 2009/24/EC Article 6 and 17 U.S.C. § 1201(f)); rules differ by country and by contract, so check yours.

## Trademarks

"Acer", "Predator" and "PredatorSense" are trademarks of Acer Inc. They appear in this project only to say which laptop it works with and what it resembles in behaviour (nominative use). TrueSense is not affiliated with, endorsed by or sponsored by Acer, and its name and logo are its own. Other names belong to their owners.

## Software you install separately

TrueSense does not bundle these; they are installed by you and keep their own licences:

| Software | Licence | Used for |
|---|---|---|
| PyQt6 | GPL-3.0 or commercial | the optional window |
| AcerUltraX | MIT | optional Acer DTS sound; it can fetch Acer's own DTS tuning package, which is between you and that package's terms. TrueSense neither ships nor downloads it |
| noise-suppression-for-voice | GPL-3.0 | optional microphone noise reduction |
| RNNoise | BSD-3-Clause | the model behind it |
| NVIDIA driver (NVML) | NVIDIA's licence | GPU clock control, read at run time |

If you redistribute a bundle that includes any of them, follow their licences.

## Warranty and risk

The MIT licence provides the software without warranty. Changing fans, power limits and voltages can damage hardware or void a manufacturer's warranty; read [SAFETY.md](SAFETY.md) first. You use TrueSense at your own risk.

## Privacy

TrueSense has no telemetry and collects nothing. The optional update check (off until you switch it on) requests the public release number from GitHub; nothing else is sent.

## Contact

For licence, trademark or takedown questions, or to report something that should not be here, use GitHub's private vulnerability reporting for this repository (Security tab) or contact the maintainer privately. Reports are handled promptly and in good faith.
