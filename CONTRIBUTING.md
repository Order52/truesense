# Contributing

Thanks for helping. TrueSense drives real hardware, so the rules are about safety and honesty.

## Ground rules
- **Licence**: contributions are under the project's MIT licence (the kernel module is dual MIT / GPL-2.0-only). Only submit code you wrote or have the right to publish; never copy code from GPL projects or from vendor software. See [docs/LEGAL.md](docs/LEGAL.md).
- **Never claim something works without a read-back or a real test.** Mark new features VERIFIED, SIMULATED or UNTESTED in [docs/STATUS.md](docs/STATUS.md).
- **Safety first**: every write is read, written and read back; anything that can hurt (voltages, fans, power limits) needs a confirmation, safe limits and a way back. No firmware flashing, no embedded-controller access outside documented WMI calls, never write the codec `reconfig` file.
- **No proprietary files**: nothing from Acer, DTS or Realtek, and no raw ACPI dumps (they contain the OEM Windows product key).
- Other models: the module is DMI-guarded on purpose. Porting is welcome; send the output of `tools/collect_hardware_data.sh` **after reading it** (it is read-only) and never the raw tables.

## Set up
```
git clone https://github.com/Order52/truesense && cd truesense
make test                       # the whole suite, against the built-in simulator (no hardware needed)
python3 tools/screenshots.py    # regenerate docs/images (needs python-pyqt6)
./truesense gui                 # run the window from the checkout (the service must be installed for hardware features)
```
`TRUESENSE_SIM=1` runs everything on the simulator. The tests never touch your real service, config or lighting memory.

## Changing code
- Python 3.11+, standard library only (PyQt6 is optional and only for `gui.py`). Keep the CLI and the window thin: put logic in the modules and test it.
- New setting: kernel/device, `backend.py`, daemon handler if it needs privilege or persistence, CLI verb, window control, a unit test, a smoke-test command, a line in USAGE and STATUS.
- New profile kind: follow `curves.py` / `smartprofiles.py` (built-ins read-only incl. `native`, copy to edit, validate everything).
- Kernel module: `make module-test` builds it for every installed kernel with warnings on; keep it warning-free.
- Run `make check` before you push.

## Reporting
Use the issue templates. Include `truesense doctor --json`.
