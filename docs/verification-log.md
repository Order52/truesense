> Historical evidence log from the development builds. Commands appear under the project's earlier name (`rps` = today's `truesense`). The current verified / simulated / untested table is [STATUS.md](STATUS.md).

# Verification log (chronological, raw)

This is the unedited working log kept while the project was built and tested on one laptop. Some rows are **superseded by later rows** (for example an item marked "NOT DONE" that was fixed afterwards). For the current state read [STATUS.md](STATUS.md).

VERIFIED = set, read back / measured, restored on this laptop under Linux. UNTESTED = code written, not yet run against hardware.

| Feature | Status | Evidence |
|---|---|---|
| Headset mic (jack) | VERIFIED (owner + recording) | 4-pole headset: Headset Mic Jack=on after plug, source Mic2 available; recorded floor -65 dBFS, speech -29..-36 dBFS. Note: first detection needed a (re)plug. Old row: | kernel 7.3 carries the 1025:166c quirk (pin 0x19=0x03a1103c, "Headset Mic Jack" control exists) but with a 4-pole headset plugged in: Headphone Jack=on, Headset Mic Jack=off, no mic source. Run tools/headset_diag.sh |
| Power modes eco/quiet/balanced/performance/turbo | VERIFIED | all five set + read back, platform_profile agrees |
| Firmware temps + fan RPM | VERIFIED | RPM field is 13 bits (0x1FFF); earlier "anomalies" were a 12-bit mask bug |
| Selftest `rps selftest` | 10/10 PASS | pauses the daemon's fan + profile automation (fan-mode / auto-off files) and restores them; fan test: one write held both fans >2000 rpm in 100% of samples for 25 s |
| Fan manual % per fan, Max, Auto | VERIFIED | one write holds (4070 rpm @60%, 4580 @70%, 6056 @100%, Max ~6050); matches previous measured table |
| LCD overdrive, boot sound, battery 80%, USB charge level, kb timeout | VERIFIED | set / read back / restored |
| GPU mode (MUX) | VERIFIED read+write | set optimus / auto, read back, restored to nvidia (immediately, so no reboot ever sees a changed value). Effect after reboot NOT tested |
| Rear light bar (static/breathing/neon/wave/twinkling) | VERIFIED (owner saw all 5) | `rps rear ...`; readback matches; original state restored after test |
| Daemon under heavy load (24-thread AVX) | VERIFIED | fans 70/50 within 5 s, 100/90 within 10 s (6023/5615 rpm); governor lowered the clock cap 5000->2800 MHz in steps; CPU held 88-90 C (test aborted itself at 90 C); returned to idle afterwards. auto-cpufreq also writes scaling_max_freq, so the cap is not a hard guarantee |
| Daemon truesense.service (replaces predator.service) | VERIFIED | installed; idle -> firmware Auto; 100% CPU load -> 'heavy', 40% floor = 2855 rpm (matches table); auto-profile eco<->balanced; back to Auto/eco 20 s after load. Not yet tested: >75 C, governor, AC/plug events, service-stop restore |
| Keyboard per-key RGB upload | VERIFIED (owner) | all 126 cells + numpad +,-,*,Enter confirmed after fixing hidraw report-id framing (write 65 bytes: leading 0 + 64) ; brightness read-back works (GET byte 4) |
| Fn+F11/F12 keyboard brightness | VERIFIED (firmware) | owner confirmed the keyboard firmware changes brightness cleanly by itself with no software; the service no longer touches these keys (kb_handle=false) |
| Fn+F10 touchpad toggle | VERIFIED (owner + log + read-back) | 12:36:24 'touchpad off', 12:36:26 'touchpad on'; `inhibited` file read back 0 afterwards; udev rule makes it work without root |
| PREDATOR key: tap = power-mode step | VERIFIED | 'tap -> power mode: performance (held 30 min)'; mode still `performance` 1.5 min later (daemon honours the hold) |
| PREDATOR key: hold = fans Max toggle | VERIFIED | 'long -> fans: MAX' then 'long -> fans: auto (smart curve)'; the 2nd press only toggles back if firmware read back Max (0x82), so Max persisted; fans on Auto (0x41) afterwards |
| PREDATOR key: double tap = speakers/headphones | VERIFIED (log: 2 toggles, ended on headphones) | notification visuals not separately confirmed |
| Fn keys / PREDATOR key service (old row) | PARTIAL | service running on hidraw2; at 11:21:32 it logged a Fn+F11 event and wrote brightness 12->7 (owner unsure whether they pressed it). Deliberate test of F10/F11/F12/PREDATOR still pending |
| Mic mute toggle | VERIFIED | wpctl mute toggled and restored |
| F8 mic-mute LED | NOT WORKING | owner watched GPIO0, GPIO1, GPIO2 driven high for 8 s each: F8 never lit, so it is not a codec GPIO. Kernel LED hook never fires (SOF DMIC). Probably keyboard-controller/EC driven: needs a Windows USB capture while muting |
| Purified Voice equivalent (RNNoise) | VERIFIED (internal mic + headset mic) | headset mic: floor -64.8 -> -90.3 dBFS, speech unchanged (-26.6 vs -27.0 dBFS); internal mic: 10 dB less ambient. Old row: | filter-chain loads, source appears, 10 dB less ambient noise (-43.6 to -54.0 dBFS RMS); jack mic + speaker sink + personal mode untested |
| AcerUltraX / DTS speakers | NOT DONE | not installed |
| CPU PL1/PL2 write | VERIFIED (root test) | set 70/80 W -> read back 70/80 -> restored 80/80; 200 W refused by the range check; RAPL lock bit clear (writable). Daemon re-asserts 80/80 |
| CPU PL1/PL2 (old row) | READ OK | PL1=PL2=80 W, RAPL enabled; `cpu limits` writes are range-checked + read back (needs root/daemon), not yet exercised |
| Permanent install (tools/install.sh) | DONE by owner | daemon restart bug found (enable --now does not restart): fixed in the script; daemon restarted manually 12:36:15 |
| NVIDIA power limit (`nvidia-smi -pl`) | NOT POSSIBLE on this GPU | driver: 'Changing power management limit is not supported for GPU 01:00.0' (range 5-140 W, default 80 W, current [N/A]) |
| NVIDIA power limit (old row) | READ OK | default 80 W, range 5..140 W; `cpu nvidia-limit` not exercised |
| Undervolt (MSR 0x150) read | VERIFIED read only | mailbox answers: core plane 0 mV, cache plane 0 mV; msr lock bit clear; writing an offset NOT attempted (needs your explicit --confirm) |
| Undervolt (old row) | NOT TESTED by design | refuses without --confirm; reading needs root; BIOS lock unknown |
| Arch package | BUILT (not installed) | packaging/arch/PKGBUILD builds a clean package |
| Per-key RGB upload | NOT DONE | needs interrupt-OUT endpoint 4 (libusb) |
| AcerUltraX DTS, offline render of the same noise through all modes | VERIFIED (objective) | `acerultrax-cli render` on 8 modes x speakers/headphones: speakers off->game1 +27 dB loudness, L/R corr 1.00->0.84, voice cuts bass share -20->-28 dB; headphones use a different, gentler tuning (own EQ, corr 0.92-0.97 in game modes) |
| AcerUltraX / Acer DTS speaker sound (live) | VERIFIED (measured) / sound quality = owner's ear | v0.1.4 installed (user mode), Acer driver imported (26 tuning files, verified engine build); service running; pink-noise through the speaker sink differs per mode (off/music/movie/game1/voice: loudness up to +2.9 dB, presence band up to +7.7 dB) |
| Output switch speakers<->headphones with aux plugged | VERIFIED (command, F5 key via switch_audio.sh) | `truesense output ...` flips the card profile both ways with read-back; AcerUltraX follows; a 'sticky' mode was REMOVED: WirePlumber already reports api.acp.auto-profile=false, so the setting was a no-op |
| Keyboard built-in effects | VERIFIED (owner): static, breathing, wave, snake, ripple, raindrop, star, heartbeat, neon, lightning, spot, fireball, snow | neon was not visible at brightness 25 but shows (and responds to speed) at brightness 50. NOT tested: lightning, spot, fireball, snow, operatingmode |
| Keyboard OPERATINGMODE lighting | PARTIAL (owner) | by itself shows ONE static colour and does NOT track the power mode (verified); re-sending the colour on each mode change works: owner confirmed green(eco), blue(quiet), cyan(balanced) in order, says orange/red also worked but did not confirm them individually. Colours are my defaults (Acer's per-mode colours are not in the captured data). Opt-in via config [keyboard] operating_follow = true (daemon hook written, not yet exercised live) |
| Purified Voice, headset PERSONAL mode | VERIFIED (measured) | speech -31.2 -> -33.1 dBFS (-1.9 dB), quiet floor -63.4 -> -88.1 dBFS (-24.7 dB); very soft words are sometimes clipped (e.g. -25 -> -42 dB), so default is the gentler conference mode |
| Purified Voice auto-switch + notification | VERIFIED (owner saw both popups; log shows headset<->array on each plug) | found + fixed: filters with node.dont-fallback were destroyed on every plug event; the filters now survive and re-link |
| Purified Voice speaker filter | WIRED, processing NOT measured | app -> RNNoise -> AcerUltraX -> output confirmed by links; attenuation could not be measured (AcerUltraX sink monitor is empty); kept OFF by default (it also gates music) |
| Fn+Esc | CAPTURED (owner pressed) | sends vendor report `04 8a ff` on the Fn-key interface (hidraw2) plus keyboard usage 0xd7 (KEY_KPPLUSMINUS); nothing visible happens by default; configurable via [buttons] fn_esc |
| Fn+arrows (up/down/left/right) | CAPTURED (owner pressed) | each sends only keyboard usage 0xd7 (KEY_KPPLUSMINUS) on hidraw0, indistinguishable from each other, no visible effect, no vendor report |
| Battery calibration flag | VERIFIED (flag only) | set on, read back 1, set off, read back 0; a real calibration cycle (hours) not run |
| `rps selftest` (extended) | 15/15 PASS | firmware settings + daemon + key service + keyboard read-back + audio output + Purified Voice config |
| tools/test_cpu_power.sh | WRITTEN, needs sudo | RAPL write/readback/restore, NVIDIA -pl, undervolt READ ONLY; never writes an offset |
| Install 2 (13:01): daemon restarted with new code, audio watcher user service started | VERIFIED | all services active; watcher applied the headset policy at start (default source rps_pv_headset) |
| REBOOT TEST (13:03 boot) | 15/15 selftest PASS | modules loaded (DKMS), all system + user services active, 0 failed units, key service re-found the Fn interface after hidraw2 -> hidraw3 renumbering, touchpad switch still group-writable (udev rule survives boot) |
| Headset mic when plugged in AT BOOT | NOT WORKING (needs replug) | after the reboot Headphone Jack=on but Headset Mic Jack=off; policy falls back to the mic array. Workaround: unplug + replug. Workaround only. DO NOT use the codec `reconfig` interface: it DEADLOCKED the kernel here (`hda-codec: reconfiguring` never completed, process stuck in D state, sound card half torn down, needs a reboot) |
| Forced reboot after the codec-reconfig hang (13:13 boot) | RECOVERED CLEANLY | btrfs mounted without errors, previous boot ended with an orderly unmount, sound card back, 0 failed units, project files intact |
| BUG FOUND + FIXED: firmware read-back flake | FIXED | ~1 in 15 immediate reads after a write returned -1 (firmware not ready) so the strict one-shot read-back false-failed `usb charging`; all setters now retry the read-back for up to 1 s (`_settle`); 25 rounds x 8 setters back to back = 0 failures. Needs `sudo tools/install.sh` to reach the installed copy |
