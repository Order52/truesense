# Rear RGB light on Acer Predator Helios 16 (PH16-71): how to control it from Linux

Everything here was derived on this laptop from PredatorSense 5.2.109 and verified by sending the commands and reading the state back. Windows is not needed again for the rear RGB.

## 1. Key finding: the rear light is NOT a USB device
- USBPcap captures (`rear_off`, `rear_on`, `keyboard_control`) show no lighting traffic on USB, for the rear light or for the keyboard.
- PredatorSense talks to a local service (AcerLightingService). It loads `AcerECLightbarController.dll`, which sends commands through **ACPI WMI**.
- Acer's own config (`RGBDevice.ini`) maps `EC\ACPI2` to `AcerECLightbarController`. `LightingProfile.ini` lists two devices on this laptop: `AcerUSBKeyboard Device` and `AcerECRearLightbar Device`.
- Internal rear-light name: "AcerECRearLightbar Device" ("infinity_mirror"). In the PredatorSense log it is `device:2`.

## 2. The WMI interface
- Class `AcerGamingFunction` in `root\wmi` (ACPI instance `ACPI\PNP0C14\APGe_0`), version string "2.88".
- **WMI GUID `7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56`**. This is the GUID Linux `acer-wmi` calls `WMID_GUID4`. On Linux it appears as `/sys/bus/wmi/devices/7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56-*`.
- Method IDs (from the MOF; full list in `re/reference/AcerGamingFunction_methods.txt`):

| id | method | in | out |
|---|---|---|---|
| 2 | SetGamingLED | u8[] | u32 |
| 4 | GetGamingLED | u32 | u8[] + ret |
| 6 | **SetGamingRgbKb** | u64 | u32 |
| 7 | GetGamingRgbKb | u32 | u64 |
| 10 | SetGamingLEDBehavior | u64 | u32 |
| 11 | GetGamingLEDBehavior | u32 | u64 |
| 12 | SetGamingLEDColor | u64 | u32 |
| 13 | GetGamingLEDColor | u32 | u64 |
| 20 | **SetGamingKBBacklight** | u8[16] | u32 |
| 21 | **GetGamingKBBacklight** | u32 | u8[] + ret |

## 3. VERIFIED commands for the rear light
Output 0 means success. Readback was checked after each step.

### 3a. Effect, speed, brightness, direction and colour: method 20, `SetGamingKBBacklight`
Input is exactly **16 bytes**:

```
[0]  mode        0=STATIC  1=BREATHING  2=NEON  3=WAVE  7=TWINKLING
[1]  speed       1..5 (default 3)
[2]  brightness  0..100  (0 = off; 100 = max)
[3]  0
[4]  direction   1 (default; 0 also seen)
[5]  R           effect colour (modes 1 and 7 use it; NEON/WAVE ignore it, send 0)
[6]  G
[7]  B
[8]  0x03
[9]  0x02
[10..15] 0
```
- Verified readback for modes 1, 2, 3 and 7: the colour, speed and brightness read back exactly as sent.
- Examples (hex): Breathing `#3723c7` = `01 03 64 00 01 37 23 c7 03 02 00 00 00 00 00 00`; Neon = `02 03 64 00 01 00 00 00 03 02 00..`; Wave = `03 03 64 00 01 00 00 00 03 02 00..`; Twinkling green = `07 03 64 00 01 00 ff 00 03 02 00..`.
- **Mode 0 (STATIC) ignores the colour bytes in this buffer.** Only brightness and speed apply there. Set the colour with 3b.
- Setting brightness to 0 in mode 0 turned the light off (readback brightness 0). PredatorSense's own OFF may use a separate path (see 5).

### 3b. Static colour: method 6, `SetGamingRgbKb`, u64 input
```
value = (zoneBit << 40) | (0x08 << 32) | (B << 24) | (G << 16) | (R << 8)
        zoneBit = 1        0x08 = rear lightbar index
```
- Little-endian bytes: `[0]=0, [1]=R, [2]=G, [3]=B, [4]=0x08, [5]=0x01, [6..7]=0`.
- Static red `0x000001080000ff00`, green `0x0000010800ff0000`, blue `0x00000108ff000000`, white `0x00000108ffffff00`.
- Verified: after each call the active-state readback (method 21, input 2) showed the new colour.
- Use **zone bit 1 only**. The rear has one zone; zone bits 2, 4 and 8 returned success but changed nothing, and mask 0xF was not accepted for use.
- Index 8 is the rear. Index 1 returned 1 (rejected on this laptop). Lightbar indices from the DLL: front=1, left=2, right=4, rear=8.
- To show the static colour, first send 3a with mode 0 (for example `00 01 64 00 01 00 00 00 03 02 00..`), then 3b.

### 3c. Reading the state back
`GetGamingKBBacklight` (method 21) with input `2` returns the current rear state in the same layout as 3a (15 bytes; ignore the missing byte 15). Input `1` returns the stored static slot. Input `0`, `4` and `8` return an error (ret=1).

### 3d. UI to wire values (from the PredatorSense log, `re/analysis/predatorsense_rear_commands.txt`)
- UI brightness 1..5 maps to a 0..100 wire value. Only 5 to 100 is confirmed; 1..4 are probably 20/40/60/80 (not confirmed).
- UI speed 1..5 maps directly to wire speed 1..5 (3 confirmed).
- UI effect names: STATIC, BREATHING, NEON, WAVE, TWINKLING. Per `LightingProfile.ini` the wire values are 0, 1, 2, 3, 7.
- Profile limits: brightness_max=100, speed 1..5, colour used only by STATIC (colors 1), BREATHING (1) and TWINKLING (1). NEON and WAVE take no colour.

## 4. How the answers were obtained (for traceability)
- `AcerECLightbarController.dll` (copy in `re/bin/predatorservice/x64/`) contains the debug strings and code builders:
  - `AcerECLightBarController::SetMode` builds the 16-byte buffer (3a) and calls WMI `SetGamingKBBacklight`.
  - `AcerECLightBarController::SetLightBarColors` builds the u64 (3b) and calls `SetGamingRgbKb`.
  - `AcerECRearLightbarController` sets lightbar index 8.
- Disassembly output: `re/analysis/lightbar_ctrl.txt`, `lightbar_dll.txt`, `lightbar_callers.txt`. Tools in `re/tools/` (Node + capstone-wasm).
- WMI probe logs: `wmi_read.txt`, `wmi_write_test.txt`, `wmi_write_test2.txt`.

## 5. Not verified (best-effort notes)
- **PredatorSense's explicit OFF/ON button** probably uses `SetLightBarOnOff`: a 12-byte (older firmware) or 16-byte (newer) `SetGamingLED` (method 2) array with `[0]=0x10`, bytes 6..9 = per-lightbar bitmask (positions 1/2/4/8), others 0xFF. Not tested. Brightness 0 is the verified way to turn the rear light off.
- A `SetGamingLEDBehavior(0xFFF5000000000010)` call exists as an init/probe step. Unknown semantics, do not need it.
- `SetGamingLEDBehavior`/`SetGamingLEDColor` u64 formats (decoded from the *logo* LED code, same DLL): colour `[1][R][G][B][brightness][speed]`, behavior `(realMode<<16)|1`. They did not appear on the rear path.

## 6. Calling it from Linux
The in-kernel `acer-wmi` driver binds this GUID but only exposes fixed features. Options:
1. **Kernel module / patch `acer-wmi`**: call `wmi_evaluate_method("7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56", 0, <method id>, &in, &out)` with a 16-byte buffer (method 20) or an 8-byte little-endian u64 (method 6). Expose it via sysfs/debugfs. Newer kernels use `wmidev_evaluate_method()` with a `struct wmi_device *`. Untested sketch below.
2. **`acpi_call`**: call the ACPI WMI method for this device. Get the exact ACPI path by running `sudo acpidump -b` and `iasl -d` on Linux and looking at the `PNP0C14` device whose `_UID` is `APGe` (method `WMBH`/`WQ..`, arguments: instance 0, method id, input buffer). The Windows ACPI firmware API did not return the DSDT, so dump it on Linux.

Kernel-module sketch (**untested**):
```c
#include <linux/module.h>
#include <linux/wmi.h>
#include <linux/acpi.h>
#define GUID4 "7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56"
static int rear_kb_set(const u8 buf[16]) {
    struct acpi_buffer in = { 16, (void *)buf }, out = { ACPI_ALLOCATE_BUFFER, NULL };
    acpi_status s = wmi_evaluate_method(GUID4, 0, 20, &in, &out);
    kfree(out.pointer);
    return ACPI_FAILURE(s) ? -EIO : 0;
}
static int rear_rgb_set(u8 r, u8 g, u8 b) {
    u64 v = (1ULL << 40) | (8ULL << 32) | ((u64)b << 24) | ((u64)g << 16) | ((u64)r << 8);
    struct acpi_buffer in = { sizeof(v), &v }, out = { ACPI_ALLOCATE_BUFFER, NULL };
    acpi_status s = wmi_evaluate_method(GUID4, 0, 6, &in, &out);
    kfree(out.pointer);
    return ACPI_FAILURE(s) ? -EIO : 0;
}
```
Test order on Linux: (1) read: call method 21 input 2 and compare with section 3c; (2) send `01 03 64 00 01 37 23 c7 03 02 00..` (Breathing, should look as before); (3) mode 0 + 3b for a static colour.

## 7. State left on this laptop
The rear light was restored to its original PredatorSense setting: BREATHING, speed 3, brightness 100, colour #3723c7 (readback `01 03 64 00 01 37 23 c7 03`). The static colour slot was changed by test 2 and set back to #00aec7 (its original).

## 8. POWER MODES (operating mode): VERIFIED on this laptop
Same WMI GUID `7A4DDFE7-5B5D-40B4-8595-4408E0CC7F56`, method **`SetGamingMiscSetting` (id 22)** / **`GetGamingMiscSetting` (id 23)**, 64-bit values. Code: `AcerHardwareService.exe` (`set_operation_mode` at RVA 0xd7d0, `OperatingAction::GetOperatingMode` at 0x17070).

| action | input | result |
|---|---|---|
| read current mode | method 23, input `0x0B` | u64; **byte0 = status (0 = ok), byte1 = mode** (`mode = (out >> 8) & 0xff`) |
| read supported modes | method 23, input `0x0A` | here `0x7300`: byte1 `0x73` = bitmask of supported modes (bits 0,1,4,5,6) |
| set mode | method 22, input `(mode << 8) \| 0x0B` | u32, 0 = success |

Mode values (PredatorSense's enum; all five were set and read back on this laptop):
`0` QUIET, `1` DEFAULT (balanced), `4` EXTREME (performance), `5` TURBO, `6` ECO.
Examples: Quiet `0x000B`, Default `0x010B`, Extreme `0x040B`, Turbo `0x050B`, Eco `0x060B`.
- Verified sequence: 0, 1, 5, 6, 4, then back to 1; every set returned 0 and the readback matched.
- PredatorSense also does a follow-up (`0xda50`) after each set that maps the mode to a small index (0→0, 6→0, 4→2, 5→3, 1→1). It is probably the turbo-key LED/indicator colour. It was not needed for the mode to change and was not investigated.
- Other `GetGamingMiscSetting` inputs on this laptop (unidentified, for reference): 1→0x0, 2→0x200, 3→0x1, 4→0x1, 5→0x1, 6→0x0, 7→0xff00, 8→0x100, 9→0x700, 0xC→1, 0xD→1, 0xE→2, 0xF→1, 0x10→1.
- Linux: check first whether your kernel's `acer-wmi` already offers `platform_profile` for this model (`cat /sys/firmware/acpi/platform_profile`). If not, use the table above through the same WMI path as the rear light (u64 in, u32/u64 out).
- Related but not decoded: `SetGamingProfile` (1) / `SetGamingProfileSetting` (8) / fan methods 14..19 (`kSvcCmdWMISetGamingFanGroupBehavior/Speed/Table`). The model INI `Predator PH16-71.ini` holds GPU overclock deltas per mode (Turbo: +100 MHz GPU, +200 MHz RAM; others 0) applied by the Windows service via NVAPI; there is no equivalent hardware command.

## 9. KEYBOARD BACKLIGHT "OFF AFTER 30 SECONDS": VERIFIED on this laptop
This is **USB HID on the keyboard (04f2:0117, interface 3, usage page 0xFF02)**, not WMI. Code: `AcerHardwareService.exe` (`acer::KYD100::KeyboardHID::SetKeyboardBacklightingOff30s` at RVA 0x20840, `GetKeyboardBacklightingOff30s` at 0x20a10).
- Windows report descriptor for that interface: feature report = 1 report-ID byte (0) + **8 data bytes**. On Linux this is exactly your existing path: HID class `SET_REPORT`, bRequest 9, wValue `0x0300`, wIndex 3, **8 data bytes** (with hidraw: `HIDIOCSFEATURE` with a 9-byte buffer whose first byte is 0).
- **SET** (8 bytes): `30 01 <flag> 00 00 00 00 <chk>`, where `flag` = 1 enables "backlight off after 30 s", 0 disables, and `chk = 0xFF - sum(first 7 bytes)`.
  - Enable: `30 01 01 00 00 00 00 cd`.  Disable: `30 01 00 00 00 00 00 ce`.
- **GET**: first send (SET_REPORT) the query `b0 00 00 00 00 00 00 4f`, then read the feature report back (GET_REPORT, bRequest 1, wValue `0x0300`, 8 bytes). The reply is `b0 01 <flag> 00 00 00 00 <chk>`; **byte 2 is the flag**.
- Verified: 0 → 1 → 0 round trip, reply byte 2 followed the writes (`b0 01 01 … cd`, then `b0 01 00 … ce`). The original value was 0 (PredatorSense also showed `BACKLIGHT_30S` status 0) and was restored.
- On Windows the Acer lighting service writes this same interface concurrently, so a GET sometimes returned a stale buffer (for example `12 00 00 08 00 00 00 e5`). On Linux there is no such service, but still query-then-read promptly and retry if the reply does not start with `b0`.
- Not functionally timed: I did not wait 30 idle seconds to watch the light go off. The PredatorSense toggle in the UI uses the same call.
- There is a second variant, `hid_set_keyborad_backlight30s(seconds, brightness)` (handler RVA 0x11040 in the service), not decoded.

## 10. BLUE LIGHT SHIELD: software only, no firmware command
- Levels (from Acer Quick Access `AcerQAAgent.exe`): `0` off, `1` low reduction, `2` medium, `3` high, `4` Long Hour Mode, `5` customized (read-only). Stored in `C:\ProgramData\Acer\QA\settings.json` as `"BluelightShield": <0..4>` (currently 0).
- Implementation: `AQAUserPS.exe` (user session) calls `SetGraphicCpp.exe`/`acerGraphic.dll` (`SetGraphicCscAndGammaLut`, Intel IGCL and NVAPI) to load a colour-matrix + gamma LUT into the display controller (`DisplayColorProfile.bin`, `calibration.bin` in `C:\Windows\oem\RGBProfile`; that `calibration.bin` is empty on this laptop). No WMI/EC/USB command is involved, so there is nothing to port at the firmware level.
- The exact per-level RGB factors are not stored in any readable file here and I did not extract them from the code. On Linux, use a software filter that reduces blue: `gammastep -O <K>` / `redshift -O <K>` (X11/Wayland wlroots), GNOME/KDE "Night Light", or set the DRM `CTM`/`GAMMA_LUT` properties directly. Suggested starting points (my approximation, not Acer's values): low ≈ 4500 K, medium ≈ 3800 K, high ≈ 3200 K, long hour ≈ 2800 K.

## 11. Windows state at the end
Rear light: Breathing #3723c7 (restored). Power mode: Default (mode 1, restored to the value found). 30-second backlight flag: 0 (as found). Blue light shield: unchanged (0).

---
# PART 2: other PredatorSense features (added after the first handoff)

How these were found: Acer's services only write call logs if `HKLM\SOFTWARE\OEM\AcerAgentService\Log` has DWORD values `ACERHARDWARESERVICE` and `ACERAGENTSERVICE` (found by reversing the binaries); with that set and the services restarted, `C:\ProgramData\OEM\AcerAgentService\Log\SYSTEM_AcerHardwareService.log` records every WMI/HID call with its exact input and output. I then drove the PredatorSense window (screenshots + clicks) and read the logs. The key was removed again afterwards. Logs: `re/logs/`. Tools: `re/tools/` (`enable_acer_logging.ps1`, `disable_acer_logging.ps1`, `ps_ui.ps1`, `probe.ps1`, `parse_acer_logs.js`).

Confidence labels: **LOG** = exact call PredatorSense's own service logged while I changed the setting in the UI (not separately re-sent by me). **VERIFIED** = I sent it myself and read it back. **DERIVED** = inferred from values, not independently tested.

## 12. LCD overdrive: LOG
WMI `AcerGamingFunction.SetGamingProfile` (method id 1, u64; PredatorSense calls this "SetGamingProfileConfiguration"):
- overdrive **ON**: `0x0001000000000010`;  overdrive **OFF**: `0x0000000000000010`.
- i.e. `(value << 48) | 0x10`, where `0x10` selects the LCD overdrive setting. This laptop reports `LCD_Overdrive_support=1`.
- Read side not confirmed: at startup `GetGamingProfile(0)` returned `0x1000000` while the UI showed overdrive off, so do not rely on that getter to read the state.

## 13. Fan mode (Auto / Max): LOG; fan RPM and temperatures: VERIFIED against the UI
- `SetGamingFanBehavior` (method id 14, u64): **Auto = `0x410009`**, **Max = `0x820009`**.
  Layout: low byte `0x09` = mask of fans (bit0 and bit3 = CPU and GPU fan), then 2 bits per fan at position `2*index`: `01` = auto, `10` = max. DERIVED: `11` is probably custom.
- `GetGamingFanBehavior` (id 15, input `0x09`) returned `0x4100` in Auto (the value shifted left by 8).
- Custom fan curves: not captured. `GetGamingFanSpeed` (id 17) inputs 1 and 4 returned `0x1E00` (30%, probably the stored custom duty). The set formats for `SetGamingFanSpeed` (id 16) and `SetGamingFanTable` (id 18) are not decoded.
- **Monitoring** via `GetGamingSysInfo` (method id 5, input `u32`), value = `(output >> 8)`:

| input | meaning | UI check |
|---|---|---|
| `0x0101` | CPU temperature, deg C | UI 56 -> read 0x38 = 56 |
| `0x0301` | system temperature, deg C | UI 56 |
| `0x0A01` | GPU temperature, deg C | UI 53 -> read 0x35 = 53 |
| `0x0201` | CPU fan RPM | UI 1833 -> read 0x729 = 1833 |
| `0x0601` | GPU fan RPM | UI 1854 -> read 0x734 = 1844 |
| `0x0801` | extra fan | reads 0 (not fitted) |
| `0x0000` | capability word | `0x227000000` |
| `0x0002` | capability word | `0x10000000000` |

(GPU usage/frequency/idle come from NVAPI on Windows, not WMI.)

## 14. Keyboard section details
- Backlight-off-after-30s: confirmed in the service log as `usb_set_keyborad_backlight30s state: 0/1` (see section 9). Extra behavior: when the 30 s timer fires the lighting service also sets the **rear light brightness to 0** (a normal `SetGamingKBBacklight` with brightness 0, speed/colour kept) and restores it on the next key press. If you want the same behavior on Linux you must do that yourself.
- Sticky keys toggle (`kSvcCmdSetStickyKey`, state 0/1) is a Windows accessibility setting. Nothing to port.
- No Windows-key-lock control exists on this model's UI.

## 15. Battery and charging: NOT decoded
- UI (Personal Settings > Battery & Charge): "Battery Charge Mode" (Optimized = 80% limit / Full), "Power-off USB charger" toggle, "Charge limit while using battery power" (30%).
- These are handled by Acer Quick Access (`AcerQAAgent.exe`), not by the hardware service, so they leave no call in the service log. State is stored in `C:\ProgramData\Acer\QA\settings.json` (`USBChargeState`, `USBChargeLimit`).
- WMI class `BatteryControl` exists (GUID `79772EC5-04B1-4bfd-843C-61E7F77B6CC9`, methods 19 `GetBattInfoInterface`, 20 `GetBatteryHealthControlStatus`, 21 `SetBatteryHealthControl`, 22/23 battery calibration; this is the interface the Linux `acer-wmi-battery` driver uses for the 80% health mode). On this laptop `GetBattInfoInterface` answered (index 0..6 -> `0x8031, 1, 1, 0x6001, 1, 1, 1`) but `GetBatteryHealthControlStatus` and `GetBatteryFunctionData` returned empty. Quick Access also contains an `AcerHID2025::SetBatteryBoundary` path, so the PH16-71 may use an EC-HID interface for the charge limit instead. Next step on Linux: try `acer-wmi-battery`; if it does not work, the HID boundary path would need a capture.

## 16. Software-only features (no firmware interface)
- Bluelight Shield (section 10), Acer TrueHarmony / DTS audio profiles (Shooter, RPG, Strategy, Movies, Music, Voice, Automatic, Custom), sticky keys, ProCam/App Center/Scenario profiles: Windows software only.

## 17. NOT done in Part 2
- **Keyboard RGB** (zones, effects, per-key): not decoded here. `LightingProfile.ini` lists the effect names and ranges for `AcerUSBKeyboard Device` (use that together with your existing USB packet knowledge).
- Custom fan curves, overclocking-mode switch (the UI has no such page on this model), GPU mode / MUX switch (deliberately not touched: can need a reboot), system boot animation/sound and boot-logo customization (deliberately not touched), power-off USB charger and charge-limit write formats.
- Not separately re-sent by me from this PC: LCD overdrive and fan Auto/Max (LOG only). Re-test them on Linux with a read-back.

## 18. State left on this laptop after Part 2
Rear light Breathing #3723c7, power mode Default, keyboard 30 s flag 1 (it was 1 when I started this round), fan Auto, LCD overdrive off, Bluelight Shield off, sticky keys off, battery mode Full, USB charger on. Acer service logging key removed. PredatorSense is left open.

---
# PART 3: keyboard RGB (USB HID) and battery findings

## 19. Keyboard lighting wire protocol: DECODED from `AcerUSBController.dll` and CONFIRMED against a real capture
Device: keyboard `04f2:0117`, **interface 3 (usage page 0xFF02)**. Everything is HID **feature reports** (class SET_REPORT, bRequest 9, wValue `0x0300`, wIndex 3, **8 data bytes**), exactly the transport you already use. The only non-feature traffic is the per-key frames on **interrupt OUT endpoint 4** (64 bytes each).
Checksum rule for every 8-byte packet: `chk = 0xFF - (sum of bytes 0..6) & 0xFF`.

### 19a. Effect modes (`AcerUSBKeyboardController::SetMode`)  [DECODED from code]
Send three feature reports in this order:
1. prelude: `b1 00 00 00 00 00 00 4e`
2. colour: `14 00 00 R G B 00 chk`   (chk = `~(0x14 + R + G + B)`)
3. effect: `08 02 <effect> <10 - speed> <brightness> 01 <direction + 1> chk`   (chk = `~(0x08+0x02+effect+(10-speed)+brightness+0x01+(direction+1))`)

- `speed` is 1..11 in the UI (profile `speed_min=1, speed_max=11`); the byte sent is `(10 - speed) & 0xFF`.
- `brightness` 0..50 (profile `brightness_max=50`).
- `direction` 0,1,.. in the app; the byte sent is direction + 1.
- Effect ids (from `LightingProfile.ini`, `value=`; names match the UI): **OFF 0, STATIC 1, BREATHING 2, WAVE 3, SNAKE 5, RIPPLE 6, NEON 8, RAINDROP 10, LIGHTNING 18, SPOT 37, STAR 38, FIREBALL 39, SNOW 40, HEARTBEAT 41, OPERATINGMODE 34 (0x22), Direct/per-key 255 (0xFF)**.
- Effects that take a colour (`colors_max=1`): STATIC, BREATHING, SNAKE, RIPPLE, RAINDROP, LIGHTNING, SPOT, STAR, FIREBALL, SNOW, HEARTBEAT, OPERATINGMODE. WAVE and NEON ignore it.
- **OFF (mode 0)** is sent as: `b1 00 00 00 00 00 00 4e`, then `08 02 00 00 00 00 00 f5`, then `14 00 00 00 00 00 00 eb`.
- **OPERATINGMODE (0x22)** has fixed constants: no prelude; colour packet `14 00 01 R G B 00 chk'` (chk' = `~(0x15+R+G+B)`); effect packet `08 03 22 05 32 02 01 chk`; speed range 1..5. This is the mode where the keyboard colour reflects the power mode.
- Mode 0xFF (Direct) does not use SetMode; it uses the per-key path (19b).

### 19b. Per-key (Direct) upload  [CAPTURED on this laptop, USBPcap]
Real sequence PredatorSense sent when it applied a static colour `#00aec7` in Direct mode (`re/battery_ui.pcapng`, `re/battery_ui.txt`):
1. `88 00 00 00 00 00 00 77`  (feature SET_REPORT)
2. feature **GET_REPORT** (read-back, 8 bytes)
3. `12 00 00 08 00 00 00 e5`  (start of per-key upload)
4. **8 frames x 64 bytes** on interrupt OUT endpoint 4. Each frame holds 16 keys, 4 bytes per key: `00 R G B` (here `00 00 ae c7`, i.e. R=0x00, G=0xAE, B=0xC7), 128 key slots in total; the unused tail of the last frame is zero.
5. `08 02 33 05 0c 08 01 a8`  (final effect packet: effect `0x33`, byte3 = 5, brightness `0x0c` = 12, then `08 01`, chk `a8`). Brightness 12 matches `[AcerUSBKeyboard Device_Direct] brightness=12` in `LightingProfile.ini`.
The key-to-slot map (which of the 128 slots is which key) is not decoded here; use your existing Linux knowledge of the matrix.

### 19c. What the Windows service does on top
- Applies the 30 s timeout by sending brightness 0 to the rear light too (section 14).
- Re-applies the saved keyboard state on service start and on some events (I saw the whole 19b sequence at capture start).

## 20. Battery charge mode / USB charger / charge limit: still UNRESOLVED
- A USB capture while I toggled **Optimized/Full battery charging** and the **power-off USB charger** in PredatorSense showed **no USB traffic at all** for those actions. So they do not go through the keyboard/EC USB HID.
- They are handled by Acer Quick Access (`AcerQAAgent.exe`) through WMI `BatteryControl` (GUID `79772EC5-04B1-4bfd-843C-61E7F77B6CC9`, methods 19..23) and/or other ACPI methods. On this laptop `GetBatteryHealthControlStatus` and `GetBatteryFunctionData` returned empty replies from PowerShell.
- On Linux: try the `acer-wmi-battery` driver for the same GUID first (health mode / 80% limit), then test `SetBatteryHealthControl` (id 21) yourself. If the replies are empty there too, the model needs a different battery method and a firmware (DSDT) read: `sudo acpidump` and look for the battery/health methods in the `PNP0C14` device `APGe`.

## 21. Final list of what is and is not covered
- Covered and tested: rear RGB, power modes, 30 s backlight, hardware monitoring reads.
- Covered from the service log / captured traffic (re-test with a read-back on Linux): LCD overdrive, fan Auto/Max, keyboard effects (19a, decoded from code and consistent with your own known packets), per-key upload (19b, real capture).
- Not decoded: custom fan curves, GPU mode (MUX), system boot animation/sound and boot-logo, battery charge mode, USB charger and charge limit write formats, the key-to-slot matrix.
- Windows-only software: Bluelight Shield, TrueHarmony/DTS audio profiles, sticky keys, app UI.

---
# PART 4: final status of the remaining PredatorSense items, plus Realtek headset retasking

## 22. Remaining PredatorSense items (investigated this round)
- **Battery charge mode ("Optimized 80%") and Care Center:** the Care Center agent log says "**The machine does not support Battery Boundary!**" and "Does not support Acer HID!" (the 2025 EC-HID path is absent). `C:\ProgramData\Acer\CC\settings.json` only stores a UI flag (`BatteryHealthy`). `BatteryControl` WMI methods return empty replies here. So on this PH16-71 there is no firmware battery limit that PredatorSense can use; the UI option is stored but has no hardware effect that I could find.
- **Power-off USB charger / charge limit (30%):** state in `C:\ProgramData\Acer\QA\settings.json` (`USBChargeState`, `USBChargeLimit`); a differential sweep of 849 generic WMI getters (`AcerGenericMethod.GetFunction` ids `0x000..0x30F`, `GetGamingMiscSetting` 0..0x40) around the toggle showed no change, so the read-back is not in those getters. Function ids seen in Quick Access for other features (generic `GetFunction`/`SetFunction`): fan control `0x107/0x207/0x307`, fan table `0x207`, SIM `0x1102/0x1103/0x1202`, D2D status `0x102`, device state `0x20001`.
- **Not decoded / not touched on purpose:** GPU mode (MUX), system boot animation/sound and boot-logo customization, custom fan curves.
- The USB charger toggle was restored to ON after my test (a mis-click had left it OFF for a short time).

## 23. Realtek headset/aux retasking
See `LINUX_HANDOFF_realtek_headset.md` and `fix_headset_mic.sh`.

## 25. SUPERSEDES sections 15, 20, 22 and 24 where they disagree: findings from Linuwu-Sense + live verification
Source of the leads: the open-source **Linuwu-Sense** kernel module (https://github.com/0x7375646F/Linuwu-Sense, `src/linuwu_sense.c`, which is itself a reverse-engineering of PredatorSense) plus the **Venator** and **TensorRaya/acer-predator-helios-rgb** projects. Every call below was then **run on this laptop with a read-back and restored to the original value** (scripts `re/tools/wmi_linuwu_*.ps1`, `wmi_gpu_test*.ps1`; outputs in `re/analysis/wmi_linuwu_*.txt`, `wmi_gpu_test*.txt`).
Why I missed the USB charger before: it is **not** on `AcerGenericMethod` (the class I swept) but on class **`APGeAction`**, GUID `61EF69EA-865C-4BC3-A502-A0DEBA0CB531` (ACPI `PNP0C14\APGe_0`), methods `SetFunction(uiInput:u64)` / `GetFunction(uiInput:u32)->uiOutput:u64` (Linux acer-wmi: `WMID_GUID3`, `ACER_WMID_SET_FUNCTION=1`, `ACER_WMID_GET_FUNCTION=2`). Why the battery looked empty: `BatteryControl` needs the exact buffer sizes (below).

| Feature | Linux / WMI call (all VERIFIED on this laptop: set, read back, restored) |
|---|---|
| **USB charging when powered off + stop level** | `APGeAction.GetFunction(0x4)`: `663296`=off, `659200`=10 %, `1314560`=20 %, `1969920`=30 %. Set with `APGeAction.SetFunction(v)`: off `663300`, 10 % `659204`, 20 % `1314564`, 30 % `1969924`. (This laptop reads 1969920 = 30 %, matching PredatorSense's `USBChargeLimit 30`.) Linuwu sysfs: `usb_charging` 0/10/20/30. |
| **Battery charge limit 80 % ("health mode") and calibration** | Class `BatteryControl` (GUID `79772EC5-04B1-4bfd-843C-61E7F77B6CC9`, Linux method ids 20 get / 21 set). **Get**: `GetBatteryHealthControlStatus(uBatteryNo=1, uFunctionQuery=1 (health) or 2 (calibration), uReserved=byte[2] {0,0})` -> `uFunctionStatus[0]` = 1 when on (the buffer must be 2 reserved bytes, other sizes returned nothing; that is why my first attempts were empty). `uFunctionList=0x3` = both functions supported. **Set**: `SetBatteryHealthControl(uBatteryNo=1, uFunctionMask=1 (health) or 2 (calibration), uFunctionStatus=1/0, uReservedIn=byte[5] zeros)` -> `uReturn 0`. Tested ON -> status 01, OFF -> status 00. This **replaces** the "no Battery Boundary / not supported" conclusion in 15/20/22: the firmware method works, only Acer's Care Center says otherwise. Linuwu sysfs: `battery_limiter`, `battery_calibration`. Upstream alternative: `acer-wmi-battery` driver. |
| **Boot animation and sound (the on/off switch, NOT a custom logo)** | `AcerGamingFunction.GetGamingMiscSetting(6)`: `0x100` = on, `0` = off. `SetGamingMiscSetting(0x106)` = on, `SetGamingMiscSetting(0x6)` = off. Tested and restored (off). This is an ordinary firmware setting, not a flash. A **custom boot logo image** (separate PredatorSense feature `SUPPORT_CUSTOM_BOOT_LOGO`) is still NOT done on purpose (it writes an image to firmware). |
| **Custom fan speed (NOT curves)** | `SetGamingFanBehavior(0xC30009)` selects "both fans manual", then `SetGamingFanSpeed(((pct*25600/100) & 0xFF00) + fanIndex)` with **CPU fan index 1, GPU fan index 4** (u64 inputs). Tested 40 % on both: readback `GetGamingFanSpeed(1)=GetGamingFanSpeed(4)=0x2800` (=40 %), `rc 0`. CPU only: behaviour `0x400008` then `0x30001`, then speed with index 1. GPU only: behaviour `0x10001` then `0xC00008`, then speed with index 4. **Max** `0x820009`, **Auto** `0x410009` (restored to Auto). RPM: `GetGamingSysInfo(0x0201)` = CPU fan, `0x0601` = GPU fan, value = `(raw >> 8) & 0xFFF` (read 0xb1c00 -> 2844 rpm). There is still no per-temperature curve table; "custom" in PredatorSense = fixed percentages per fan. Linuwu sysfs: `fan_speed` ("cpu,gpu", 0 = auto, 100 = max). |
| **GPU mode (MUX)** | Quick Access code + PredatorSense UI code: `GetGamingMiscSetting(2)` byte 1 = mode, `SetGamingMiscSetting((mode<<8)|2)`. The UI's capability value on this laptop is **7 = three modes** (Automatic Select, Optimus, NVIDIA GPU only). **CORRECTED by PreySense's docs + checked on this laptop (see `LINUX_HANDOFF_extras.md` section 2): firmware value 1 = Optimus, 2 = NVIDIA GPU only (dGPU/MUX, the CURRENT mode: Device Manager shows only the RTX 4060, no Intel iGPU), 3 = Automatic; 0 is rejected (rc 2).** All three accepted values read back correctly (0x100, 0x200, 0x300) and the original 0x200 was restored each time. (My first guess in this row, "2 = Automatic, 1 = NVIDIA only", was wrong.) The change normally takes effect after a reboot; I restored 0x200 immediately each time so nothing was applied. Linuwu-Sense has no GPU mode; this one is mine. |
| **Keyboard backlight timeout (30 s), alternative path** | Linuwu uses `APGeAction.GetFunction(0x88401)`: `0x80000` = off, `0x1E0000080000` = on; set `APGeAction.SetFunction(0x1E0000088402)` on / `0x88402` off. On this laptop `GetFunction(0x88401)` returned `0x80000`. My HID method in section 9 is the one I verified; this one is a second route (EC) and I did not write-test it. |
| **LCD overdrive** | Same as section 12; Linuwu confirms: `SetGamingProfile(0x1000000000010)` on / `0x10` off, `GetGamingProfile(0)` = `0x1000001000000` on / `0x1000000` off. Read: `0x1000000` (off) on this laptop. |
| **Per-key keyboard map (cell -> key)** | Community-captured map for exactly this keyboard (PH16-71, Chicony `04F2:0117`) from Venator: `re/reference/keymap_ph16-71_venator.json`. Layout rule: `cell = col*6 + row`, row 0 = bottom (modifier row), row 5 = top (function row), columns 0..21; e.g. Esc=5, F1=11, A=20, Space=24, Enter=86. Test run on this laptop with `re/tools/keymap_check_win.ps1` (Windows, sends the same per-key frames over the keyboard's vendor HID interface MI_03: feature reports via HidD_SetFeature, 65-byte output reports via WriteFile; no write errors, original Direct #00aec7 state restored afterwards; log `re/analysis/keymap_check_run.txt`). It lit Esc (cell 5), Space (24), Enter (86), W/A/S/D (21,20,26,32), F12/Right/Backslash (77,102,13), Num5/Predator (110,106), one step at a time; the owner then **verified the whole map on this laptop** with `re/tools/keymap_learn_win.ps1` (lights each of the 128 cells alone, the owner presses the lit key, the script records its scancode; 3 runs, logs `re/analysis/keymap_learn*.jsonl`, result `re/reference/keymap_ph16-71_verified.json`). Outcome: 96 cells confirmed by an actual key press; 81+ match the community map exactly. Corrections found: **cell 2 is Esc and cell 5 is CapsLock (swapped vs the community map; seen identically in two runs)**; **cell 70 is the main-row minus `-`** (missing from the community map; cell 123 is the numpad minus); the long **Space bar lights from more than one cell (24 and 36/37)**; numpad keys were recorded with NumLock off so they read as Delete/PageDown/Right/PageUp (cells 114-117 are Num. Num3 Num6 Num9 as in the map). Fn (cell 6) was confirmed by the owner by eye (no key event). Windows key, media keys, Predator, Power and PrtSc send no usable key event and keep the community label. Use `keymap_ph16-71_verified.json` as the source of truth, and the earlier community file only for those few unverified cells. Frame format confirmed identical to mine: 128 cells x `{00,R,G,B}` = 512 bytes in 8 x 64-byte interrupt-OUT frames, commit `08 02 33 05 <bright> 08 01 chk`. Note: lid logo and the rear glass bar of PH16-73 are Darfon `0D62:A00A` HID LampArray devices (TensorRaya docs); on this PH16-71 the rear bar worked through WMI as documented in section 3. |
| **Still NOT done** | Custom boot logo image upload (firmware write, left alone on purpose); per-temperature fan curves (no interface found anywhere, including Linuwu); the Optimus mode name for value 3. |

## 24. Final round (GPU mode, fan table, USB charger, key map)  [partly superseded by section 25]
- **GPU mode (MUX): decoded from `AcerQAAgent.exe` code, read verified, write NOT tested.** It is `AcerGamingFunction` misc-setting index 2.
  - Read: `GetGamingMiscSetting(2)`; the mode is byte 1 of the result. This laptop returned `0x200`, so the current mode value is **2**.
  - Write (from code): `SetGamingMiscSetting((mode << 8) | 2)`.
  - I did NOT run the write: it can need a reboot and I could not confirm which value is Hybrid or dGPU-only, so I left the mode alone. Find the values on Linux by reading index 2 after changing the mode in firmware setup.
  - Quick Access code paths: `CallGamingMiscSetting_Get(index, out byte)` and `SetGPUMode`. Power mode uses the same method with index 0x0B (section 8).
  - Misc indices 1..13 return real values on this laptop; 14 and above return the "unsupported" reply `0x2` (see `re/analysis/wmi_read2.txt`).
- **Fan table / custom curves:** `GetGamingFanTable` (method 19) returns only `1` (no table data). `SetGamingFanTable` (method 18) takes a u64 and Quick Access uses it only to switch between fan tables ("SwitchFanTable"), not to upload a curve. There is no custom-curve interface on this model. Fan behaviour readbacks are in `re/analysis/wmi_read2.txt`.
- **Power-off USB charger / limit:** the Quick Access service keeps the settings in its own JSON (`USBChargeState`, `USBChargeLimit`, config key `EcoPlusUSBChargingLimit`), and its code message reads "Disable USB Charge when battery level under ...". No firmware getter changed in my sweeps, so I could not find the firmware call. The Windows service may enforce this itself.
- **Key-to-slot map:** use `keymap_probe.py` on Linux (lights one of the 128 slots at a time, you type which key lit, it writes `keymap.json`). The packet sequence is the captured one from 19b. Not run on Linux yet.
- **Boot logo/sound, battery limit firmware writes:** deliberately not attempted (firmware writes).
- **Linux kernel already knows part of this:** the in-tree `acer-wmi` driver handles Predator "platform profile" (power modes) on newer kernels. Try that before the raw WMI calls in section 8.
