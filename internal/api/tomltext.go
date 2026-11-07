package api

import (
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// literalString matches a single-quoted TOML string without a quote or backslash inside.
var literalString = regexp.MustCompile(`'([^'"\\\n]*)'`)

// tomlText marshals v as TOML the way the 0.3.x tool wrote it: strings in double quotes
// (go-toml chooses single quotes, which are the same string but not what people expect).
func tomlText(v any) (string, error) {
	b, err := toml.Marshal(v)
	if err != nil {
		return "", err
	}
	return literalString.ReplaceAllString(string(b), `"$1"`), nil
}

// profileSections is the order the sections of a profile are shown in (the order its
// documentation lists them); sections it does not know follow, sorted.
var profileSections = []string{"power", "fan", "keyboard", "rear", "battery", "display", "boot", "audio"}

// orderedTOML marshals a profile: plain settings first, then the sections in their usual order.
func orderedTOML(p map[string]any) (string, error) {
	plain := map[string]any{}
	tables := map[string]any{}
	for k, v := range p {
		if _, ok := v.(map[string]any); ok {
			tables[k] = v
		} else {
			plain[k] = v
		}
	}
	var out strings.Builder
	if len(plain) > 0 {
		t, err := tomlText(plain)
		if err != nil {
			return "", err
		}
		out.WriteString(t)
	}
	names := append([]string(nil), profileSections...)
	var rest []string
	for k := range tables {
		known := false
		for _, n := range profileSections {
			known = known || n == k
		}
		if !known {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, name := range append(names, rest...) {
		v, ok := tables[name]
		if !ok {
			continue
		}
		t, err := tomlText(map[string]any{name: v})
		if err != nil {
			return "", err
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(t)
	}
	return out.String(), nil
}
