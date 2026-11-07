package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/Order52/truesense/internal/profiles"
)

// ProfileInfo is one whole-setup profile of the lists.
type ProfileInfo struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

// Profiles lists the whole-setup profiles (shipped and yours).
func (a *API) Profiles() []ProfileInfo {
	names := a.profiles.Names()
	out := make([]ProfileInfo, 0, len(names))
	for _, n := range names {
		info := ProfileInfo{Name: n, Source: a.profiles.Source(n)}
		if p, err := a.profiles.Load(n); err == nil {
			info.Description, _ = p["description"].(string)
		}
		out = append(out, info)
	}
	return out
}

// ProfileNames lists the profile names.
func (a *API) ProfileNames() []string { return nonNil(a.profiles.Names()) }

// Profile reads one profile.
func (a *API) Profile(name string) (profiles.Profile, error) { return a.profiles.Load(name) }

// ProfileTOML is a profile as the TOML text of its file.
func (a *API) ProfileTOML(p profiles.Profile) (string, error) {
	return orderedTOML(p)
}

// DescribeProfile says in one line what a profile changes ("" when it cannot be read).
func (a *API) DescribeProfile(name string) string {
	p, err := a.profiles.Load(name)
	if err != nil {
		return ""
	}
	return strings.Join(profiles.Describe(p), "  ·  ")
}

// ApplyProfile applies what the profile lists and nothing else, and answers the summary line.
func (a *API) ApplyProfile(ctx context.Context, name string) (string, error) {
	return a.profiles.Apply(ctx, name)
}

// NextProfile applies the profile after the last one applied.
func (a *API) NextProfile(ctx context.Context) (string, error) {
	n, err := a.profiles.NextName()
	if err != nil {
		return "", err
	}
	return a.profiles.Apply(ctx, n)
}

// SaveProfile saves the current state as a profile and answers the file written.
func (a *API) SaveProfile(ctx context.Context, name, description string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("profile names: letters, digits, - and _")
	}
	p, err := a.profiles.Capture(ctx, description)
	if err != nil {
		return "", err
	}
	return a.profiles.Save(name, p)
}

// DeleteProfile removes one of your profiles.
func (a *API) DeleteProfile(name string) error { return a.profiles.Delete(name) }
