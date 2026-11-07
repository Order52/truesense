package api

import "encoding/json"

// NamedList is a list of profiles (fan curves, Smart auto profiles, undervolts, ...): the
// names in display order (built-in ones first), the one in use when the service says so, and
// a one-line description for the built-in ones.
type NamedList struct {
	Names        []string
	Current      string
	Descriptions map[string]string
}

// asMap converts a typed value to its JSON object, for code that edits one setting by name.
func asMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = json.Unmarshal(b, &m)
	return m, err
}
