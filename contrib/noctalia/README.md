# Noctalia widgets for TrueSense

Bar widgets and panels for power mode (`profile`), fans (`fan`) and lighting (`rgb`). They read `/tmp/truesense-state` (written by the daemon every second) and call the `truesense` CLI.

Install: put the `.luau` files in a Noctalia plugin that declares widgets `profile`, `fan`, `rgb` and panels `profile_panel`, `fan_panel`, `rgb_panel` (see the plugin API of your Noctalia version), then add the widgets to a bar group. Noctalia hot-reloads them.

Clicking opens the panel; middle click switches the smart fan curve (fan), the automatic profile (profile) or the keyboard backlight (rgb); scrolling over RGB changes keyboard brightness.
