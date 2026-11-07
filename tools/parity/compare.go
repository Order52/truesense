package parity

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// volatile are the JSON keys whose values legitimately differ between two runs or two
// implementations (versions, times, the machine's own identifiers).
var volatile = map[string]bool{
	"truesense": true, "version": true, "current": true, "checked_at": true, "answered": true, "kernel": true,
}

// diff lists the structural differences between two decoded JSON values as "path: python X, go Y".
// Numbers compare by value (the two JSON encoders print 30 and 30.0 alike), object keys by name,
// arrays by position; keys in volatile are skipped.
func diff(path string, py, goV any) []string {
	switch p := py.(type) {
	case map[string]any:
		g, ok := goV.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: python object, go %T", path, goV)}
		}
		keys := map[string]bool{}
		for k := range p {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		var out []string
		for _, k := range names {
			if volatile[k] {
				continue
			}
			pv, inPy := p[k]
			gv, inGo := g[k]
			switch {
			case !inGo:
				out = append(out, fmt.Sprintf("%s.%s: only in python (%s)", path, k, short(pv)))
			case !inPy:
				out = append(out, fmt.Sprintf("%s.%s: only in go (%s)", path, k, short(gv)))
			default:
				out = append(out, diff(path+"."+k, pv, gv)...)
			}
		}
		return out
	case []any:
		g, ok := goV.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: python array, go %s", path, short(goV))}
		}
		var out []string
		if len(p) != len(g) {
			out = append(out, fmt.Sprintf("%s: python has %d items, go %d (%s | %s)", path, len(p), len(g), short(p), short(g)))
		}
		for i := 0; i < min(len(p), len(g)); i++ {
			out = append(out, diff(fmt.Sprintf("%s[%d]", path, i), p[i], g[i])...)
		}
		return out
	}
	if !reflect.DeepEqual(py, goV) {
		return []string{fmt.Sprintf("%s: python %s, go %s", path, short(py), short(goV))}
	}
	return nil
}

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 120 {
		return string(b[:117]) + "..."
	}
	return string(b)
}
