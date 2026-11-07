package api

import (
	"context"
	"fmt"

	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/looks"
)

// StartupSettings is what the lights and the profile do at login.
type StartupSettings struct {
	Profile  string `json:"profile"`
	Keyboard string `json:"keyboard"`
	Rear     string `json:"rear"`
}

// Startup reads the login settings.
func (a *API) Startup() (StartupSettings, error) {
	c, err := a.LoadConfig()
	if err != nil {
		return StartupSettings{}, err
	}
	return StartupSettings{Profile: c.Startup.Profile, Keyboard: c.Startup.Keyboard, Rear: c.Startup.Rear}, nil
}

// ApplyStartup applies the login settings now (the user service does it at login): the profile
// first, then the lighting. A step that fails never stops the others. It answers one line per step.
func (a *API) ApplyStartup(ctx context.Context) ([]string, error) { return a.login.Apply(ctx) }

// SetStartup sets what happens at login: target keyboard | rear | profile ("none" clears the
// profile). The profile, lighting profile or look must exist. It answers the value stored.
func (a *API) SetStartup(target, value string) (string, error) {
	return a.login.SetOption(target, value)
}

// BootLook is the look the service applies at boot (before login) and after suspend; nil when none.
func (a *API) BootLook() (*looks.Look, error) {
	var l *looks.Look
	err := a.s.IPC.CallInto("boot_look", ipc.Args{"action": "get"}, &l)
	return l, err
}

// ClearBootLook stops applying a look at boot.
func (a *API) ClearBootLook() error {
	return a.s.IPC.CallInto("boot_look", ipc.Args{"action": "clear"}, nil)
}

// SetBootLook makes a look the boot look: "current" is what is on the lights now, anything
// else a saved look's name. The service keeps its own copy, so it works before login.
func (a *API) SetBootLook(what string) (*looks.Look, error) {
	var (
		l   *looks.Look
		err error
	)
	if what == "current" {
		l, err = a.s.Looks.Capture()
	} else {
		l, err = a.s.Looks.Load(what)
	}
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, fmt.Errorf("a look needs keyboard and/or rear settings")
	}
	var echoed looks.Look
	if err := a.s.IPC.CallInto("boot_look", ipc.Args{"action": "set", "look": l}, &echoed); err != nil {
		return nil, err
	}
	return l, nil
}
