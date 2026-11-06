## What and why

## How it was checked
- [ ] `python3 -m unittest discover -s tests -t .` passes (and new behaviour has a test)
- [ ] `ruff check .` and `make check` are clean
- [ ] Anything that writes to the hardware was only exercised against the simulator (`TRUESENSE_SIM=1`), or I say below what I ran on real hardware
- [ ] `RELEASE_NOTES.md` has a line under "## Unreleased" if users will notice the change
