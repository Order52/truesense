# Release notes

## Unreleased

**Much faster launch** (measured from process start to a visible window: 266 ms before, 92 ms now)
- The window builds only Home before it appears; the other nine pages are built right after the first paint, one per event-loop turn, or at once if you navigate first. The page numbers never change, and data that arrives early is held back and applied once everything exists.
- The installer (and the Arch packages) now byte-compile the program as root. The installed folder is root-owned, so your user could not write the Python cache and recompiled `gui.py` and 14 other modules on every launch (and on many command-line calls). `doctor` has a new "Start-up speed" check that warns when the cache is missing.
- The update module (and with it the HTTP stack: `urllib`, `http.client`, `ssl`) is loaded only when you check for updates.

## 0.3.11 (2026-10-31): faster start-up, accessible controls, lint in CI

**Faster**
- Opening the window now does one full hardware refresh instead of two (20 calls / 235 ms in the first 3 s instead of 33 calls / 467 ms), and switching pages or coming back to the window no longer re-reads everything when the data is less than 5 s old. Anything you change still refreshes at once.

**Accessibility**
- Every control now has a name a screen reader can announce (before, 144 switches, sliders, drop-downs, colour swatches and mode tiles were unnamed). Clicking a row's name focuses its drop-down or text box. A test keeps it that way.

**Code quality**
- Added `ruff.toml`, `make lint` and a lint job in CI (real bugs and unused code: pyflakes rules, syntax errors, a few correctness rules). Cleaned up what it found: unused imports and variables, dead code in the shell completion generator, one-import-per-line, explicit returns.

## 0.3.10 (2026-10-31): slower effects, a faster and lighter window, a cleaner look

**Effect speed**
- The keyboard effect speed slider now goes slower. Measured on the real keyboard: it stores speed bytes 0 (fastest) to 11 (slowest) and refuses anything above; the old scale (1..11, byte = 10 - speed) never reached the two slowest steps, and its top setting sent 0xFF, which the keyboard turns into a medium speed (so the fastest setting was slower than the one below it). The scale is now 1 (slowest) to 12 (fastest), one keyboard step per slider step.
- Saved looks keep their number but run a little differently (the scale is now 12 - speed instead of 10 - speed).

**Home**
- Removed the Cycle mode Previous / Next buttons: the mode tiles do the same.

**Faster and lighter**
- The window asks the hardware for far less. While idle it now reads only what changes by itself (power mode and power source from the service, and the fans); the slow settings (lighting, battery, screen, boot, GPU) are re-read every 20 s and whenever you come back to the window, open a page or change something. Measured on the real laptop: 15 hardware calls (134 ms) per 10 s instead of 55 calls (594 ms), which also means less firmware time stolen from the audio path.
- When the window is not the focused one it polls three times less often.
- Temperatures and fan speeds are read off the UI thread, so a slow or stuck device can never freeze the window.
- Icons are rendered once instead of on every repaint (a big part of the cost of showing a page: Smart 56 to 25 ms, Battery 55 to 16 ms, Profiles 18 to 6 ms).

**Looks**
- Every section is a clearly outlined card; page margins and spacing were rebalanced to fit.
- Hint text is lighter (contrast 5.6:1 instead of 3.9:1, above the 4.5:1 readability guideline).
- The primary button's hover colour now matches the accent blue instead of an old cyan.

## 0.3.9 (2026-10-31): one place for profiles

- **Home:** removed the profile picker at the top. It duplicated the Profiles page, which stays as the single place to pick, apply, save and delete profiles (Home keeps the Profiles tile).
- The test for the Profiles page now also checks that Home has no second picker.

## 0.3.8 (2026-10-31): layout fixes on the Lighting and Undervolt pages

- **Fixed:** on tabs shorter than the tallest tab (Lighting: Rear bar and Profiles) the settings rows stretched, leaving a huge empty gap, for example around the Effect buttons on the Rear bar tab. Rows now take exactly the height they need.
- Lighting and Undervolt tabs are as tall as the tab you are on, so a short tab no longer scrolls or leaves long empty space (Rear bar and Keyboard no longer scroll at all).
- The window title follows the page ("TrueSense - Lighting"), which helps on a floating window.
- Tests cover the row height, the per-tab height and the window title.

## 0.3.7 (2026-10-31): updating from the window works without a polkit agent

- **Fixed:** the Update button in the window did nothing useful on desktops without a polkit authentication agent (Hyprland, sway and other minimal setups). It used `pkexec`, which cannot ask for a password without an agent, and only reported "the installer exited with code 127". The updater now checks for an agent first. With none, it opens a terminal window (`$TERMINAL`, then kitty, foot, alacritty, ghostty, wezterm, konsole, gnome-terminal, xfce4-terminal or xterm) that runs `sudo truesense update apply`, and tells you what to run if no terminal can be found.
- Whether an update really installed is now decided by the installed version, not by the terminal window closing.
- Tests cover agent detection (polkitd alone does not count), terminal selection, and the fallback path.

## 0.3.6 (2026-10-31): polish, a settings-file race fixed, clearer Profiles page

- **Fixed:** the per-user settings file could lose a setting when two things wrote at once (the window, the key service, the command line, or threads inside the window). It is now locked across threads and processes.
- **Profiles page:** says plainly that a profile only changes what it lists, so switching from one profile to another can leave things from the earlier one switched on (for example battery-saver's 80 % limit).
- **Home:** the profile picker fits on one line; all five mode tiles use the same label size; the fan hint matches the chosen fan mode instead of one long paragraph.
- **Settings:** grouped into Window, At login, Touchpad, Power management, Updates, Diagnostics, About and Reset; the Updates row no longer stretches its button.
- **Small things:** the undervolt offset reads "0 mV" instead of "-0 mV"; the "..." placeholders say what they are waiting for; the per-key "Save as a lighting profile" label says where the profile appears.
- **Tests:** two flaky window tests are now deterministic, and there is a test that hammers the settings file from several threads.

## 0.3.5 (2026-10-31): new key animations, game rules, profiles page, power-manager takeover, responsive window

Ideas gathered from four related projects (Venator, jma-studio, acer-predator-helios-rgb, PreySense); everything was written from scratch.

- 14 new per-key animations: rainbow, scanner, comet, ripple, rain, matrix, fire, plasma, aurora, lightning, confetti, starlight, mood, breathing. Unchanged frames are no longer re-sent.
- `keyboard design` (built-in gaming, coding, ice, ember, stealth, arrows; your own JSON files) and `keyboard image` (picture across the keys).
- Game rules: `power game add PROGRAM MODE` raises the power mode while a program runs.
- The "too hot" state now releases 5 degrees below its line (no flapping).
- Window: Designs and pictures on the Per-key tab, Game rules on the Smart page, hardware self-test and service log in Settings; the mouse back and forward buttons (and Alt+Left / Alt+Right) move through pages.
- Fans: manual sliders 20 to 100 %; the *Turn fans off…* button asks first, only works while cool, reads the result back, measures the real fan speed afterwards and says when the firmware keeps the fans running at a minimum speed (it does on the tested PH16-71); the watchdog gives the fans back to Auto at 70/65 °C. `fan set CPU GPU --confirm` accepts 0 per fan on the command line. Performance icon redrawn as a full dial; picture preview on the Per-key tab.
- Window: can be resized from 380 pixels wide up when you turn that on; settings rows move their controls under the name when narrow, the fan curve graph and palette stretch, content is centred on wide windows, and a page can no longer be cut off (a test checks every page). The compact fixed 480 pixel window (which floats on Hyprland and sway) stays the default; the "Fixed-size window" switch in Settings turns on free resizing. Esc now closes the app (the per-key board still uses the first Esc to clear its selection).
- *TrueSense manages power* (Settings, or `truesense power managers stop|restore|list`, off by default): stops and disables auto-cpufreq, TLP, tuned, power-profiles-daemon or system76-power when they run, fixes what they leave behind (a manager can leave CPU turbo OFF, and the kernel then lowers every core's frequency ceiling to the base clock and does not raise it again when turbo is back on: both are repaired), remembers it all and can restore it. `doctor` also warns when turbo is off.
- Profiles have their own page (a profile is a one-click bundle of power, fans, rear light, battery, screen, boot sound and audio): it lists what each profile changes in plain words and how profiles differ from Smart auto profiles, fan curves and lighting profiles; Home has a compact picker at the top.
- Window controls for every command-line feature that had none: profile bundles (apply, next, save, show, delete), cycle power mode, keyboard colour follows the power mode, per-key sets, At login (keyboard, rear bar, profile, apply now), speaker noise filter, microphone volume, touchpad, show/check configuration, service status.
- `doctor` lists other power managers (tuned, tlp, auto-cpufreq, ...) that can fight the power modes.

## 0.3.4 (2026-10-31): one-line, multi-distribution installer; AUR package

* **`tools/install.sh` rewritten**: detects Arch, Debian/Ubuntu, Fedora and openSUSE families, installs missing prerequisites (dkms, compiler, matching kernel headers, Python, socat, the window's Qt), handles clang-built kernels, test-builds the module before changing anything, swaps program files atomically and rolls back if the service does not start, restores SELinux contexts. New options: `--check` (read-only report), `--no-deps`, `--no-gui`, `--user=`, `--uninstall`.
* **`tools/get.sh`** now installs the newest release automatically; extra options pass through (`... | sudo bash -s -- --no-gui`).
* **AUR packaging**: `packaging/aur/PKGBUILD.template` and `tools/aur-publish.sh` (the build was verified locally from the v0.3.3 tarball). Publishing needs the maintainer's AUR account.
* **docs/INSTALL.md**. Only Arch/CachyOS is tested on real hardware; other families are covered by detection unit tests only.

## 0.3.3 (2026-10-31): MIT licence and legal clean-up

* **Relicensed to MIT** (the kernel module is dual MIT / GPL-2.0-only so the kernel accepts it). SPDX identifiers in every source file; the LICENSE is installed with the program.
* **docs/LEGAL.md**: what the licence covers, where the code and facts come from, what is *not* in the repository, trademark use ("Acer", "Predator" and "PredatorSense" belong to Acer Inc.; TrueSense is not affiliated with Acer), the licences of software you install alongside it, privacy, contact.
* **Removed the detailed notes about the vendor's Windows software** (`docs/research`) and a quote from firmware tables; the hardware facts that remain are rewritten as independent interface notes in **docs/PROTOCOL.md**. The README no longer presents the project as "PredatorSense for Linux".
* The kernel module and DKMS are version 0.3.3 (the licence string changed): re-run the installer.

## 0.3.2 (2026-10-31): icon, credits, update-check fix

* **Logo and app icon** (`src/truesense/data/truesense.svg`, PNGs in `packaging/icons`), used by the window, the launcher and the README; installed into the icon theme.
* **Credits**: [docs/CREDITS.md](docs/CREDITS.md) thanks every project that helped (Linuwu-Sense, AcerUltraX, RNNoise, noise-suppression-for-voice, G-Helper, PreySense, the maintainer's earlier tools, and the platform) with what each gave and its licence; a Credits button in Settings, About; a code of conduct and README badges.
* **Update check fix**: a version tag newer than the latest GitHub Release page is now found (the older release page used to win).

## 0.3.1 (2026-10-31): updates

* **Update check and one-click update**: Settings, Updates (and `truesense update check|apply|auto`). A banner on Home appears when a newer release exists; Update downloads the tagged release, verifies it and runs its installer with administrator rights (graphical password prompt, or sudo in a terminal), then offers to restart the window.
* **Automatic checks** (off until you switch them on): a daily user timer asks GitHub for the newest release number and shows a desktop notification once per version. Nothing is installed without you.
* Safe by construction: only version tags, HTTPS, unsafe archive paths refused, the version inside must match the tag, and the repository comes from the root-owned system config, never from your user config.
* The installer also works when started through `pkexec`, and starts the startup service in the current session.

## 0.3.0 (2026-10-31): production release

Tested on one machine, the **Acer Predator Helios 16 PH16-71** (BIOS V1.18, CachyOS, kernels 7.3.0-4 and 6.18 LTS). [docs/STATUS.md](docs/STATUS.md) says exactly what was verified on hardware and what is only covered by simulator tests. **The kernel module changed (fans-off), so the DKMS version is 0.3.0: re-run `sudo tools/install.sh`.**

### Highlights
* **A fresh install behaves like PredatorSense on Windows.** Fans on the firmware's own control, Native auto power mode, nothing of ours touching clocks or power limits until you choose a smart feature. Every profile kind has a read-only `native` entry that is that default.
* **One window** (`truesense gui`, Qt 6): a compact home page and clean pages for Lighting (keyboard, per-key on the real layout, rear bar, profiles), fan curves, Smart auto profiles, the PREDATOR key, undervolt (CPU and GPU), battery, audio and settings. Live updates, follows changes made elsewhere, the wheel never changes a setting by accident.
* **Smart auto** (heat, load and battery aware, with profiles) and **Native auto**; a mode you pick ends by itself when the situation changes.
* **Fans**: Smart curves (built-in, your own, drag-to-edit), manual with separate CPU/GPU sliders, and Off with a temperature watchdog.
* **Undervolt** for CPU and GPU with profiles, safety zones and typed confirmation beyond the danger line, power limits the service keeps, and a crash-guarded apply at boot.
* **PREDATOR key editor** with profiles, any action, and your own scripts through a file picker. Lighting profiles applied by the service at boot and after suspend. Per-key animations.

### Added since 0.2.0
Everything above, plus: `truesense doctor` checks for the service version, the window and status-bar support; `truesense battery full`; `truesense look`; `truesense keys profile`; `truesense power smart`; `truesense fan curve`; `truesense cpu undervolt` / `gpu undervolt`; the system check and report in Settings; `tools/screenshots.py`; CI; SAFETY, ARCHITECTURE and TROUBLESHOOTING documents.

### Changed
* The service no longer rewrites your power limits every few seconds; it keeps yours until you go back to automatic, and hands the firmware's values back when you switch smart features off.
* The GPU sampler (`nvidia-smi`) starts only when a smart feature needs it; the service paces itself at 2 s when idle.
* The keyboard's own 30-second timer (a separate controller flag, on by default) is now controlled; the old setting only changed the firmware flag.
* Defaults: smart fan off, Native auto on, hold time for a manual pick 10 minutes.

### Fixed
* Headset mic boost read the channel count instead of the value (every change looked like 2 and failed).
* Slider drags applied an older value instead of the newest; refreshes could move a slider while dragged; the window could crash when closed during a refresh; an uncaught error in a window slot aborted the program (now it is logged and shown).
* Tests used to talk to the real service and write the real user state; they are now fully isolated. Several tests depended on the real charger, CPU temperature or `/tmp`; they no longer do.
* Releases after a mode pick, per-key brightness dropping an effect to static, rear-effect changes turning the bar off, Fn+F10/F11/F12 counted as PREDATOR taps, doctor false warnings under `sudo`.

### Known limits
Not yet verified on the real laptop: fans-off, undervolt writes (CPU and GPU), Smart auto in real use, charge-to-100 %-once, key animations. F8 mic LED, headset-at-boot detection, NVIDIA power limit: see STATUS.

## 0.2.0 and 0.1.0

Earlier versions were development releases of this project (first the `rpredatorsense` tool, then the rename to TrueSense with one kernel module). Their contents are all part of 0.3.0 above; see the git history for the details.
