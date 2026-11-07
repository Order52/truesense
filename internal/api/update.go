package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Order52/truesense/internal/config"
	"github.com/Order52/truesense/internal/update"
)

// updateNotifiedKey remembers, in the user state, the release the desktop was told about.
const updateNotifiedKey = "update_notified"

func (a *API) updater() (*update.Updater, error) {
	if a.Updater != nil {
		return a.Updater, nil
	}
	repo, err := update.SystemRepo(a.SystemConfig)
	if err != nil {
		return nil, err
	}
	return update.New(repo), nil
}

// UpdateRepo is the repository updates come from (the built-in default, changeable only by
// the system configuration, never by the user's).
func (a *API) UpdateRepo() (string, error) {
	u, err := a.updater()
	if err != nil {
		return "", err
	}
	return u.Repo, nil
}

// CheckUpdate asks GitHub for the newest release (once a day unless force). A failed lookup
// is in the status, not an error: the answer then carries what was cached.
func (a *API) CheckUpdate(ctx context.Context, force bool) (update.Status, error) {
	u, err := a.updater()
	if err != nil {
		return update.Status{}, err
	}
	return u.Check(ctx, force, a.cfg().Update.CheckHours), nil
}

// UpdatePlan is what `update apply` is about to install.
type UpdatePlan struct {
	Status update.Status
	// Tag is the release to install ("v0.3.17").
	Tag string
	// Needed is false when you already have the newest version and none was asked for.
	Needed bool
}

// PlanUpdate looks up the newest release and decides what to install: version (like "0.3.1")
// picks a specific release instead of the newest.
func (a *API) PlanUpdate(ctx context.Context, version string) (UpdatePlan, error) {
	st, err := a.CheckUpdate(ctx, true)
	if err != nil {
		return UpdatePlan{}, err
	}
	if st.Error != nil && st.Info == nil {
		return UpdatePlan{Status: st}, errors.New(*st.Error)
	}
	if version != "" {
		return UpdatePlan{Status: st, Tag: "v" + strings.TrimLeft(version, "v"), Needed: true}, nil
	}
	return UpdatePlan{Status: st, Tag: st.Info.Tag, Needed: st.Available}, nil
}

// ApplyUpdate downloads a release, verifies it against the release's SHA256SUMS and runs its
// installer as root. The result is returned even when the installer failed, together with an
// error that says so.
func (a *API) ApplyUpdate(ctx context.Context, tag string) (update.ApplyResult, error) {
	u, err := a.updater()
	if err != nil {
		return update.ApplyResult{}, err
	}
	res, err := u.Apply(ctx, update.ApplyOptions{Tag: tag})
	if err != nil {
		return res, err
	}
	if res.ReturnCode != 0 {
		return res, fmt.Errorf("installer exit code %d", res.ReturnCode)
	}
	return res, nil
}

// AutoUpdateCheck reports whether TrueSense looks for updates by itself once a day.
func (a *API) AutoUpdateCheck() (bool, error) {
	c, err := a.LoadConfig()
	if err != nil {
		return false, err
	}
	return c.Update.AutoCheck, nil
}

// SetAutoUpdateCheck switches the daily check (a desktop notification; nothing is installed).
func (a *API) SetAutoUpdateCheck(on bool) (bool, error) {
	err := config.WriteUser(map[string]any{"update": map[string]any{"auto_check": on}}, a.UserConfig)
	return on, err
}

// NotifyNewUpdate is what the daily user timer runs: when automatic checks are on and a newer
// release exists that the desktop was not told about yet, it shows one notification. It answers
// the version it announced ("" when it announced nothing).
//
// The 0.3.x tool kept the "told about" mark in the update cache, which the next refresh
// rewrote, so the notification came again every day; it is kept in the user state here, so a
// version is announced once, as intended.
func (a *API) NotifyNewUpdate(ctx context.Context) (string, error) {
	c, err := a.LoadConfig()
	if err != nil || !c.Update.AutoCheck {
		return "", err
	}
	st, err := a.CheckUpdate(ctx, false)
	if err != nil || !st.Available || st.Latest == nil {
		return "", err
	}
	var told string
	_ = a.s.User.GetInto(updateNotifiedKey, &told)
	if told == *st.Latest {
		return "", nil
	}
	a.s.OSD.Show(fmt.Sprintf("TrueSense %s is available (truesense update apply, or Settings in the window)", *st.Latest), "update")
	_ = a.s.User.Set(updateNotifiedKey, *st.Latest) // the mark is a convenience: a read-only disk only repeats the notice
	return *st.Latest, nil
}
