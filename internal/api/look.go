package api

import (
	"github.com/Order52/truesense/internal/looks"
)

// Looks lists the saved lighting looks (built-in ones first).
func (a *API) Looks() []string { return nonNil(a.s.Looks.Names()) }

// SaveLook saves what is on the lights now (keyboard look and rear light) under name.
func (a *API) SaveLook(name string) (*looks.Look, error) { return a.s.Looks.Save(name, nil) }

// Look reads a saved or built-in look.
func (a *API) Look(name string) (*looks.Look, error) { return a.s.Looks.Load(name) }

// DescribeLook summarises a look in one line.
func (a *API) DescribeLook(l *looks.Look) string { return looks.Describe(l) }

// ApplyLook puts a look on the lights and answers what was done.
func (a *API) ApplyLook(name string) ([]string, error) {
	l, err := a.s.Looks.Load(name)
	if err != nil {
		return nil, err
	}
	return a.s.Looks.Apply(l)
}

// DeleteLook removes one of your looks.
func (a *API) DeleteLook(name string) error { return a.s.Looks.Delete(name) }

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
