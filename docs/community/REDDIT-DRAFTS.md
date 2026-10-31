# Announcement drafts (for the maintainer to post)

Read each subreddit's rules first (self-promotion limits, flair). Post from your own account, answer comments, be honest about what is untested.

## r/linux_gaming  /  r/archlinux (flair: "Software"/"Showcase")
**Title:** I built an open-source Linux control app for the Acer Predator Helios 16 (PH16-71): fans, power modes, battery limit, RGB, undervolt

Windows PredatorSense has no Linux equivalent for this laptop, so I wrote TrueSense (MIT): a small kernel module (DKMS) that talks to the firmware's WMI interface, a root daemon, a CLI (`truesense fan auto`, `truesense rear rgb ...`, everything on/off/status) and an optional Qt window.

- Fan control (firmware auto, custom curves, manual) that always falls back to firmware auto on exit/crash
- Power modes, battery charge limit, GPU MUX mode, CPU/GPU undervolt with safety limits
- Per-key keyboard RGB, rear zones, lighting profiles applied at boot
- Remappable PREDATOR key, Fn keys, mic/headset helpers
- One-line install, `truesense doctor` health check

Honest status: tested on my own PH16-71 (BIOS V1.18, CachyOS). Some features (fans-off, undervolt writes, charge-once) are simulated/unverified on hardware and marked as such in docs/STATUS.md. Only Arch-family install is tested; Debian/Fedora/openSUSE paths exist but are untested.

Install: `curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash`
Repo: https://github.com/Order52/truesense  (screenshots in the README)
Feedback and PH16-71 owners with other BIOS versions very welcome.

## r/AcerNitro-style communities, r/GamingLaptops, r/Predator (short version)
Same text, shortened; lead with the screenshot and "works on PH16-71 only".

## Hacker News (Show HN) / Lobsters
**Title:** Show HN: TrueSense – Linux control for the Acer Predator Helios 16 (kernel module, daemon, CLI)
