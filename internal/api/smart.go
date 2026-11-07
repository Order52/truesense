package api

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/smartprofile"
)

// SmartSettingKeys are the settings of a Smart auto profile in the order they are shown.
var SmartSettingKeys = []string{"ac_idle", "ac_browsing", "ac_busy", "ac_turbo", "turbo_after_s", "turbo_max_cpu", "turbo_max_gpu", "backoff_hold_s", "idle_after_s",
	"battery_idle", "battery_browsing", "battery_busy", "battery_hot", "battery_low_pct", "up_dwell_s", "down_dwell_s", "busy_hold_s", "min_change_s", "turbo_margin_c", "turbo_retry_s"}

// SmartProfileInUse is the service's answer to which Smart auto profile runs.
type SmartProfileInUse struct {
	Name    string               `json:"name"`
	Profile smartprofile.Profile `json:"profile"`
	Names   []string             `json:"names"`
}

// SmartProfiles lists the Smart auto profiles and, when the service runs, the one in use.
func (a *API) SmartProfiles() (NamedList, error) {
	names, err := a.s.Smart.Names()
	if err != nil {
		return NamedList{}, err
	}
	l := NamedList{Names: names, Descriptions: map[string]string{}}
	for _, n := range names {
		if p, ok := smartprofile.Builtin(n); ok {
			l.Descriptions[n] = p.Description
		}
	}
	if a.DaemonAvailable() {
		cur, err := a.SmartProfileInUse()
		if err != nil {
			return NamedList{}, err
		}
		l.Current = cur.Name
	}
	return l, nil
}

// SmartProfileInUse asks the service which profile runs (it keeps it, so it works before login).
func (a *API) SmartProfileInUse() (SmartProfileInUse, error) {
	var r SmartProfileInUse
	err := a.s.IPC.CallInto("smart_profile", ipc.Args{"action": "get"}, &r)
	return r, err
}

// SmartProfile reads one profile, built-in or yours.
func (a *API) SmartProfile(name string) (smartprofile.Profile, error) { return a.s.Smart.Load(name) }

// UseSmartProfile makes a profile the one the service runs. A saved profile is sent as data,
// so the service does not depend on files in a home directory.
func (a *API) UseSmartProfile(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("which profile? (truesense power smart list)")
	}
	args := ipc.Args{"action": "use", "name": name, "data": nil}
	if !smartprofile.IsBuiltin(name) {
		p, err := a.s.Smart.Load(name)
		if err != nil {
			return "", err
		}
		args["data"] = p
	}
	var r SmartProfileInUse
	if err := a.s.IPC.CallInto("smart_profile", args, &r); err != nil {
		return "", err
	}
	return r.Name, nil
}

// CopySmartProfile saves a copy under newName (NAME-copy when empty) and answers the name.
func (a *API) CopySmartProfile(name, newName string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("copy which profile?")
	}
	return a.s.Smart.Copy(name, newName)
}

// SaveSmartProfile saves a profile: start from base (or from your own profile of that name,
// else the defaults) and apply the "key=value" settings.
func (a *API) SaveSmartProfile(name, base string, set []string) (smartprofile.Profile, error) {
	if name == "" {
		return smartprofile.Profile{}, fmt.Errorf("save under which name?")
	}
	start := smartprofile.Defaults()
	switch {
	case base != "":
		p, err := a.s.Smart.Load(base)
		if err != nil {
			return start, err
		}
		start = p
	case !smartprofile.IsBuiltin(name):
		if names, _ := a.s.Smart.Names(); slices.Contains(names, name) {
			p, err := a.s.Smart.Load(name)
			if err != nil {
				return start, err
			}
			start = p
		}
	}
	m, err := asMap(start)
	if err != nil {
		return start, err
	}
	defaults, _ := asMap(smartprofile.Defaults())
	for _, kv := range set {
		k, v, _ := strings.Cut(kv, "=")
		kind, known := defaults[k]
		if !known || v == "" {
			return start, fmt.Errorf("--set key=value, key one of: %s", strings.Join(SmartSettingKeys, ", "))
		}
		switch kind.(type) {
		case bool:
			m[k] = slices.Contains([]string{"1", "true", "yes", "on"}, strings.ToLower(v))
		case float64:
			n, err := strconv.Atoi(v)
			if err != nil {
				return start, fmt.Errorf("%s must be a whole number, not %q", k, v)
			}
			m[k] = n
		default:
			m[k] = v
		}
	}
	p, err := smartprofile.Validate(m)
	if err != nil {
		return p, err
	}
	return a.s.Smart.Save(name, p)
}

// DeleteSmartProfile removes one of your profiles (built-in ones are read-only).
func (a *API) DeleteSmartProfile(name string) error { return a.s.Smart.Delete(name) }

// UseSmartProfileData makes a profile that is not saved (the window's editor) the one the
// service runs; name is only a label. Settings the object lacks take their defaults and every
// value is checked.
func (a *API) UseSmartProfileData(name string, settings map[string]any) (string, error) {
	p, err := smartprofile.Validate(settings)
	if err != nil {
		return "", err
	}
	var r SmartProfileInUse
	if err := a.s.IPC.CallInto("smart_profile", ipc.Args{"action": "use", "name": orElse(name, "custom"), "data": p}, &r); err != nil {
		return "", err
	}
	return r.Name, nil
}

// SaveSmartProfileData saves a whole profile as you edited it.
func (a *API) SaveSmartProfileData(name string, settings map[string]any) (smartprofile.Profile, error) {
	if name == "" {
		return smartprofile.Profile{}, fmt.Errorf("save under which name?")
	}
	p, err := smartprofile.Validate(settings)
	if err != nil {
		return p, err
	}
	return a.s.Smart.Save(name, p)
}

// SmartProfileIsBuiltin reports whether a profile is read-only.
func SmartProfileIsBuiltin(name string) bool { return smartprofile.IsBuiltin(name) }
