# Realtek "Connector Retasking: Headset" on Linux (Acer Predator Helios 16 PH16-71)

Problem (yours): on Linux the 3.5 mm jack always behaves as plain headphones, so no headset/aux microphone works. On Windows the Realtek Audio Console setting *Device advanced settings > Connector Retasking > ANALOG: Headset* fixes it. This note says what that setting really does and how to do the same on Linux.

## 0. THE ACTUAL CAUSE AND FIX (found after the sections below were written)
Mainline Linux only got a quirk for this laptop in August 2026 (commit `04a8e286bb2e`, "ALSA: hda/realtek: Add quirk for Acer Predator PH16-71", `ALC2XX_FIXUP_HEADSET_MIC` for subsystem 1025:166c). On an older kernel the quirk is simply missing, which is why the jack never had a mic. Either use a newer kernel, or force the same fixup: `options snd_sof_intel_hda_generic hda_model=alc2xx-fixup-headset-mic` in `/etc/modprobe.d/` + rebuild initramfs + reboot (what `fix_headset_mic.sh --known-fix` does; documented in AcerUltraX's README, which lists this exact PH16-71 problem). Everything below is the background investigation and the fallback search (`--auto`).

## 1. What I established on the laptop (Windows)
- Codec: **Realtek ALC245**, PCI-style id `10ec:0245`, subsystem **`1025:166c`** (Acer PH16-71). Driver `RTKVHD64.sys` 6.0.9652.1, audio bus = Intel Smart Sound (`8086:7A50`), the digital mics are the Intel DMIC array (separate from the jack).
- The console's dropdown for the single `ANALOG` jack offers **Headphone / Speaker Out / Headset**. It was on Headset.
- A registry before/after diff (15 000 values) of the audio driver, Realtek and endpoint keys, while switching Headset -> Headphone, shows exactly one functional change:

  `HKLM\SYSTEM\CurrentControlSet\Control\Class\{4d36e96c-e325-11ce-bfc1-08002be10318}\0003\Settings\Drv9652_DevType_0245_SS1025166c\Pin21` : **`22 00 00 00` (Headset) <-> `02 00 00 00` (Headphone)**
  Sibling values in the same key: `Pin19 = 30 00 00 00`, `Pin14 = 04 00 00 00` (unchanged by the toggle).
- Side effect seen in the audio endpoints: with Headphone selected the capture endpoint **"Microphone (Realtek(R) Audio)" disappears** (`MMDevices\Audio\Capture\{6d874afb-...}\DeviceState` 1 -> 8, and the console's "Microphone" entry vanishes). With Headset selected it exists. So the single combo jack is wired to the headphone pin **NID 0x21** and the headset-mic pin **NID 0x19**, and "Headset" is what activates the mic pin.
- The value names `Pin14/Pin19/Pin21` are the codec node IDs in hex (0x14 speaker, 0x19 mic, 0x21 headphone). The numbers (`0x22`, `0x02`, `0x30`, `0x04`) are Realtek's own jack codes, **not** raw HDA pin-default words, so they cannot be copied to Linux directly.
- I could **not** read the codec's raw pin-default words on Windows: they live in codec/BIOS and the Windows driver exposes no readout (checked the driver registry, INF, driver binary and its debug log). The Windows setting is restored to Headset.
- Evidence files: `re/analysis/realtek_diff.txt` (the full diff), UI screenshots of the console (not in the zip).

## 2. What this means for Linux
Windows is not changing hardware, it is changing how the driver programs the codec: it treats NID 0x19 as a live **headset mic** pin and NID 0x21 as the headphone pin of the same jack. On Linux the codec driver (`snd_hda_codec_realtek`) believes what the BIOS pin table says. Your symptom (always "headphone", no mic) means the BIOS table leaves NID 0x19 unusable for the jack, and this model has no kernel quirk for subsystem `1025:166c` that adds it. The Linux equivalent of Realtek's "Retasking" is a **pin-config override** for NID 0x19.

## 2b. UPDATE: mainline Linux already has a quirk for this laptop (found in upstream source)
`sound/hda/codecs/realtek/alc269.c` (formerly `sound/pci/hda/patch_realtek.c`) contains
`SND_PCI_QUIRK(0x1025, 0x166c, "Acer Predator PH16-71", ALC2XX_FIXUP_HEADSET_MIC)`.
That fixup sets **NID 0x19 = `0x03a1103c`**, changes codec COEF index 0x45 (`alc_update_coef_idx(codec, 0x45, 0xf<<12 | 1<<10, 5<<12)`) and enables headset-mic parsing. So:
1. **First just update the kernel** (or boot a current live ISO). If your kernel is recent enough, the jack becomes a headset automatically, with a "Headset Mic" input appearing when a headset is plugged in. Check: `uname -r`, then `arecord -l` / `pactl list short sources` with a headset inserted.
2. If your kernel is older or the quirk misfires, `fix_headset_mic.sh --try 0x03a1103c` is the first candidate (the same pin word). It cannot set the COEF bits; if the mic is still silent, try `hda-verb`/`hdajackretask`, or move to a kernel carrying the quirk.
3. The Realtek "Pin21 22/02" value I found on Windows is consistent with this: the BIOS table is wrong for the mic pin and the driver must retask it.
Status: pin word and quirk existence are verified from upstream source; I have not run it on this laptop under Linux.

## 2c. NEW Windows evidence (this round): what the console really switches
Collected with `re/tools/jack_dump.ps1` (Windows Core Audio `IKsJackDescription` + topology walk) while changing only the Realtek Audio Console dropdown. Files: `re/analysis/jack_headset.txt`, `jack_headphone.txt`, `jack_speakerout.txt`, `jack_headset_after.txt`.
- **Registry value `Pin21` per dropdown choice (verified):** Headphone = `2`, Speaker Out = `3`, **Headset = `0x22` (34)**. `Pin19` (=0x30) and `Pin14` (=4) never change. So the console only changes how the driver treats NID 0x21; the 0x20 bit means "headset (jack carries a mic)".
- **One physical jack:** in Headset mode the render endpoint "Headphones" and the capture endpoint "Microphone" both report `connType=1` (3.5 mm), `geoLoc=3` (left side), `genLoc=0` (primary chassis), `portConn=0` (jack), `isConnected=1`. The microphone's topology part is `rtmicintopo`; the headphone's is `rearlineouttoposst3`.
- **Headphone mode:** the capture endpoint "Microphone" goes to `state=8` (not present) and its jack `isConnected=0`. Speaker Out additionally splits the render side into two jack entries. So "Headphone" does not just mean "different EQ": the driver physically stops using the mic pin of that jack. Your Linux symptom is the same thing.
- **Decoded pin default word for the mic pin (NID 0x19)** from those fields (HDA pin-config layout): connectivity=jack, location=external-left (0x03), default device=Mic In (0xA), connection type=3.5 mm (1), colour unknown, misc 0 => **`0x03a110xx`**. That is exactly the family of the mainline word `0x03a1103c`, so the pin word itself is very likely right. The failing part on your kernel is therefore probably not the pin word but one of: the kernel does not contain the quirk, the quirk's COEF/headset-detect step does not suit this board revision, or the capture PCM is not exposed (SOF vs legacy driver).
- **The microphone array is a different thing:** `Microphone Array` is on the Intel Smart Sound controller (`ven_8086&dev_ae20`, `intcdmictopo`), not on the ALC245. On Linux that digital mic needs the SOF DSP driver; the jack mic is on the ALC245 itself and needs neither.

## 2d. Strategy implemented in the new `fix_headset_mic.sh`
1. `sudo ./fix_headset_mic.sh --auto` (plug a headset with mic in, then talk/tap during each 3 s recording). It first writes `hda_report.txt` (full codec dump, controls, dmesg, kernel config), then tries in order: no change; NID 0x19 = `0x03a1103c`, `0x03a11030`, `0x04a11040`, `0x01a1913c`, `0x03a11050`, `0x02a11030`, `0x03a19030`; then the kernel fixup models `alc2xx-fixup-headset-mic`, `headset-mic`, `headset-mode`, `headset-mode-no-hp-mic`, `alc-headset-jack`, `alc283-headset`. After every try it records every capture device on every sound card and prints peak/rms; it stops at the first strategy that shows a signal and tells you the `--persist` command.
2. If nothing works the script has still produced `hda_report.txt`: send that file back; it contains the real BIOS pin table (`init_pin_configs`), all codec node widgets and the SOF/legacy driver state, which is the data I could not read on Windows.
3. If `arecord -l` shows **no analog capture device for the ALC245** (only the DMIC), the codec driver is not the problem: boot with `snd_intel_dspcfg.dsp_driver=1` (legacy HDA driver; jack works, DMIC array does not) or `=3` (SOF, needs `sof-firmware` and a recent kernel + topology), and rerun `--auto`.
4. Kernel without `user_pin_configs`/`hints` (no `CONFIG_SND_HDA_RECONFIG`): use the firmware-patch method below.

## 2e. Everything else I tried on Windows to read the real pin table (all negative; you do NOT need to go back)
- **Driver binary search:** the Acer Realtek package (`hdxsstacernb.inf`, `RTKVHD64.sys` 6 MB, `RTAIODAT.DAT` 58 MB) contains **no** occurrence of the laptop's subsystem id `1025166C` in any form (little/big-endian dword, ASCII, UTF-16). The driver carries no per-laptop pin table; it reads pin defaults from the codec at runtime. The INF has no pin settings either (only the SST/HDA base `AddReg` and `ForceSST=1`, i.e. this machine runs the codec behind the Intel SST controller).
- **Registry/devnode dump:** `re/analysis/realtek_codec_props.txt` (1951 lines: every PnP property and every value under the driver class key and the codec's Enum key). Only three codec pin values exist: `Pin14=4`, `Pin19=0x30`, `Pin21=0x22/0x02/0x03`. Nothing else describes pins, verbs or COEFs.
- **Windows audio topology:** `re/analysis/jack_*.txt` (both endpoints are the same left 3.5 mm jack; the mic endpoint exists only in Headset mode). Topology names: "Realtek HDA Primary output mixer with SST" (headphone side), `rtmicintopo` (jack mic), `rtstereomixtopo`.
- **What cannot be read from Windows:** the codec's pin default registers (verb 0xF1C) and the COEF values the Windows driver programs. Both need a verb sent to the codec, which Windows only allows to a kernel driver. On Linux it is one `cat /proc/asound/card*/codec#*`, which `fix_headset_mic.sh --collect` saves. So the remaining data collection is a one-minute step on Linux, not a reason to return to Windows. All Windows-side evidence is already in the zip.

## 3. How to apply it (in this order)
`fix_headset_mic.sh` (in the zip) automates all of this.
1. **Look first, change nothing:** `sudo ./fix_headset_mic.sh --collect` (writes `hda_report.txt`)
   - Confirms the codec is the ALC245 / `1025:166c`, prints the BIOS pin configs for 0x19 and 0x21, and checks that the kernel has the override interface (`/sys/class/sound/hwC0D0/user_pin_configs`, needs `CONFIG_SND_HDA_RECONFIG=y`; most distro kernels have it).
2. **Try an override live (no reboot):** `sudo ./fix_headset_mic.sh --auto` (supersedes the old --try/--diagnose; see 2d)
   - It writes candidate "Mic In, 3.5 mm, external jack" words to NID 0x19 (`0x01a1913c`, then `0x04a11040`, `0x03a11050`, `0x01a19030`) and re-probes the codec. After each, plug your headset in and run `sudo ./fix_headset_mic.sh --test` (records 4 s and prints the peak level; speak or tap the mic).
   - Good result: a **`Headset Mic Jack`** control and a headset-mic source appear (`amixer -c0 controls | grep -i mic`, `pactl list short sources`) and `--test` reports a signal.
3. **Make it permanent** with the word that worked: `sudo ./fix_headset_mic.sh --persist pin 0x01a1913c` (or `--persist model NAME`)
   (installs a small systemd oneshot that re-applies it every boot; undo with `--revert`).
4. If `user_pin_configs` is missing, use a **patch firmware file** instead: put
   ```
   [codec]
   0x10ec0245 0x1025166c 0
   [pincfg]
   0x19 0x01a1913c
   ```
   in `/lib/firmware/hda-jack-retask.fw` and (when the codec is bound by `snd_hda_intel`) add `options snd-hda-intel patch=hda-jack-retask.fw` to `/etc/modprobe.d/`; this needs `CONFIG_SND_HDA_PATCH_LOADER=y`. With the Intel SOF DSP driver the sysfs method in step 2/3 is the one that works.
5. If one of the candidates works, that single pin word is also what a kernel quirk needs. Report it upstream as `SND_PCI_QUIRK(0x1025, 0x166c, "Acer Predator Helios 16 PH16-71", <fixup with pin 0x19 = that word + headset-mode>)`.

## 4. Honest status
- **Verified (Windows):** codec ID/subsystem, which driver value the "Headset/Headphone" toggle changes (`Pin21` `0x22` <-> `0x02`), that Headset mode is what exposes the microphone endpoint, and that the jack is one combo connector (NID 0x21 + 0x19).
- **Not verified:** the exact raw pin word for NID 0x19 on this laptop (unreadable from Windows) and whether this kernel already has the override interface. The four candidate words are the standard Realtek combo-jack mic configs, ordered by likelihood; the script finds out which one works by trying them. This part needs a test on the Linux side with a real headset.
- Mic quality/boost, OMTP vs CTIA headset auto-detection, and PipeWire profile selection may need a second step once the mic pin exists (the codec's headset-mode quirk).

