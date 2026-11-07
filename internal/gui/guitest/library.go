package guitest

import (
	"fmt"
	"sort"
	"strings"
)

// library is a set of named profiles: built-in ones that cannot change, and the user's own.
// Curves, Smart auto profiles, key bindings and both undervolts behave the same way.
type library struct {
	kind    string
	builtin []string
	data    map[string]map[string]any
	user    map[string]bool
}

func newLibrary(kind string) *library {
	return &library{kind: kind, data: map[string]map[string]any{}, user: map[string]bool{}}
}

func (l *library) addBuiltin(name string, p map[string]any) {
	l.builtin = append(l.builtin, name)
	l.data[name] = p
}

func (l *library) names() []string {
	out := append([]string{}, l.builtin...)
	var mine []string
	for n := range l.user {
		mine = append(mine, n)
	}
	sort.Strings(mine)
	return append(out, mine...)
}

func (l *library) isBuiltin(name string) bool { return l.data[name] != nil && !l.user[name] }

func (l *library) show(name string) (map[string]any, error) {
	p, ok := l.data[name]
	if !ok {
		return nil, fmt.Errorf("no %s '%s'", l.kind, name)
	}
	out := cloneMap(p)
	out["builtin"] = l.isBuiltin(name)
	return out, nil
}

func (l *library) save(name string, p map[string]any) error {
	if l.isBuiltin(name) {
		return fmt.Errorf("'%s' is built in and read-only; copy it and edit the copy", name)
	}
	if err := validName(name); err != nil {
		return err
	}
	delete(p, "builtin")
	l.data[name] = p
	l.user[name] = true
	return nil
}

func (l *library) delete(name string) error {
	if l.isBuiltin(name) {
		return fmt.Errorf("'%s' is built in and cannot be deleted", name)
	}
	if !l.user[name] {
		return fmt.Errorf("no %s '%s'", l.kind, name)
	}
	delete(l.data, name)
	delete(l.user, name)
	return nil
}

func (l *library) copy(name, next string) (string, error) {
	p, ok := l.data[name]
	if !ok {
		return "", fmt.Errorf("no %s '%s'", l.kind, name)
	}
	base := next
	if base == "" {
		base = name + "-copy"
	}
	target := base
	for i := 2; l.data[target] != nil; i++ {
		target = fmt.Sprintf("%s-%d", base, i)
	}
	return target, l.save(target, cloneMap(p))
}

func validName(n string) error {
	if n == "" || len(n) > 40 || strings.Trim(n, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-") != "" {
		return fmt.Errorf("names use letters, digits, . _ - (max 40)")
	}
	return nil
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case map[string]any:
			out[k] = cloneMap(t)
		case []any:
			out[k] = cloneSlice(t)
		default:
			out[k] = v
		}
	}
	return out
}

func cloneSlice(s []any) []any {
	out := make([]any, len(s))
	for i, v := range s {
		switch t := v.(type) {
		case map[string]any:
			out[i] = cloneMap(t)
		case []any:
			out[i] = cloneSlice(t)
		default:
			out[i] = v
		}
	}
	return out
}
