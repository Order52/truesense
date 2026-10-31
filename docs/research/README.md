# Research notes (from the Windows reverse-engineering phase)

These three documents were written while the PredatorSense behaviour of this laptop was captured and decoded on Windows (WMI method IDs, the keyboard USB lighting protocol, fan and power-mode encodings, headset-jack findings, the Fn-key reports). They are the source of the protocol facts the code uses.

* `LINUX_HANDOFF_rear_rgb.md`: WMI interface, rear light, power modes, fans, LCD overdrive, battery limit, USB charger, boot sound, GPU mode, keyboard lighting wire protocol.
* `LINUX_HANDOFF_realtek_headset.md`: the headset/aux-jack microphone investigation.
* `LINUX_HANDOFF_extras.md`: AcerUltraX, PreySense comparison, Fn keys and PREDATOR key, Purified Voice options, CPU limits.

Caveats:
* They reference evidence files (`re/analysis/...`, captures, registry exports) that are **not** included: that folder contains Acer's own program files, which must not be redistributed.
* Where a note and a later measurement on Linux disagree, **[../STATUS.md](../STATUS.md) wins**. Known corrections: BIOS here is V1.18 (not V1.16); the fan RPM field is 13 bits; the rear-light read-back has a leading status byte; Fn+F11/F12 brightness is done by the keyboard firmware, not software; the Fn-key HID interface is found by searching the descriptor for usage page 0x88, not by its first usage.
