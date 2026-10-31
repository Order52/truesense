# Hardware data findings (collected on Linux)

- DMI: Acer Predator PH16-71, BIOS V1.18.
- Kernel 7.3.0-4-cachyos. ALC245 quirk active: driver_pin_configs 0x19=0x03a1103c; COEF 0x45=0x5289.
- The gaming WMI device is a PNP0C14 device with `_UID` "APGe"; a harmless kernel message says another WMI block's execution control method is not found.
- Codec GPIO: the ALC245 function group has 3 GPIOs (mask, direction and data all 0); the F8 mic-mute LED is none of GPIO0 to 2 and is not exposed in /sys/class/leds.
- RAPL: package-0 PL1 = PL2 = 80 W, both enabled, lock bit 63 = 0 (not locked).
- MSR 0x150 undervolt reads work (core and cache planes 0 mV); writes had not been attempted when this was recorded.
