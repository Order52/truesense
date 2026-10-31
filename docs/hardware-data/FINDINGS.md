# Hardware data findings (collected on Linux, 2026-10-31)
- DMI: Acer Predator PH16-71 / Discovery_RTX, **BIOS V1.18 (06/09/2026)** (handoff docs assumed V1.16).
- Kernel 7.3.0-4-cachyos. ALC245 quirk active: driver_pin_configs 0x19=0x03a1103c; COEF 0x45=0x5289.
- ACPI: WMI device `\_SB.PC00.WMID` (PNP0C14, _UID "APGe") in ssdt11.dsl, methods WMAA (line 3587) and WMBH (3948); a second WMAA in ssdt13 (NVIDIA/NPCF side). EC scope `\_SB.PC00.LPCB.EC0`.
  dmesg: "[Firmware Bug] WMBF method block execution control method not found" (harmless, other WMI blocks).
- Codec GPIO: ALC245 function group has 3 GPIOs (io=3); mask/dir/data all 0 -> F8 LED is GPIO0/1/2. LED not exposed in /sys/class/leds (no platform::micmute).
- RAPL: package-0 PL1=PL2=80 W. MSR 0x610 = 0x42828000df8280 -> PL1=80 W, PL2=80 W, both enabled, **lock bit 63 = 0 (not locked)**.
- MSR 0x194 = 0x1f0000 (undervolt allowed? decide with uv-check; no 0x150 writes performed).
- Loaded: linuwu_sense, acer_rear_wmi (DKMS, updates/dkms). acer_rear_wmi binds 7A4DDFE7... (AcerGamingFunction).
