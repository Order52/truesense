package api

import (
	"os"

	"github.com/Order52/truesense/internal/actions"
	"github.com/Order52/truesense/internal/keybinds"
	"github.com/Order52/truesense/internal/profiles"
	"github.com/Order52/truesense/internal/startup"
)

// wire builds the pieces that depend on each other and on the configuration paths: the
// profile store and the action runner (profiles apply their settings by running actions, and
// the profile actions apply profiles), the key binding store and the login settings. One
// runner is shared by the command line, the key service and the profiles, so a key press, a
// command and a profile cannot drift apart.
func (a *API) wire() {
	s := a.s
	a.profiles = &profiles.Store{
		Files: profiles.Files{UserDir: profiles.DefaultUserDir(), SysDirs: profiles.DefaultSysDirs(os.Getenv)},
		Env:   profiles.Env{Fans: s.Client, Daemon: s.IPC, HW: s.HW, Keyboard: s.KB, Audio: s.Audio, Curves: s.Curves, State: s.User},
	}
	a.runner = actions.New(actions.Env{
		Config: a.LoadConfig, Power: s.Client, HW: s.HW, Keyboard: s.KB, KeyProfiles: s.KeyProf, Looks: s.Looks, Audio: s.Audio,
		Touchpad: s.Touchpad, Daemon: s.IPC, Profiles: a.profiles, State: s.User,
		Exec: actions.ShellExec, IsRoot: func() bool { return a.EUID == 0 },
	})
	a.profiles.Actions = a.runner
	a.Actions = a.runner
	a.binds = keybinds.New()
	a.binds.Check, a.binds.ConfigPath, a.binds.LoadConfig = a.runner.Validate, a.UserConfig, a.LoadConfig
	a.login = startup.New(startup.Env{
		Config: a.LoadConfig, ConfigPath: a.UserConfig, Profiles: a.profiles, KeyProfiles: s.KeyProf, Looks: s.Looks,
		Keyboard: s.KB, Rear: s.HW, State: s.User,
	})
}
