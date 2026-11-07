package designs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Order52/truesense/internal/keymap"
)

// Colors is an ordered JSON object of name -> RRGGBB. Order matters: when two names cover
// the same key (a group and one of its keys) the later one wins, exactly as the order of a
// Python dict decided it, so the order of the file is kept.
type Colors []keymap.KeyColor

// MarshalJSON writes the object in order.
func (c Colors) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kc := range c {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kc.Key)
		v, _ := json.Marshal(kc.Color)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads an object in order, or null as empty. A repeated key keeps its first
// position with the last value (the way a Python dict loaded the same file).
func (c *Colors) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		*c = nil
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("an object of colours is expected")
	}
	var out Colors
	index := map[string]int{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := kt.(string)
		var val string
		if err := dec.Decode(&val); err != nil {
			return fmt.Errorf("colour of %q is not a string: %w", key, err)
		}
		if i, seen := index[key]; seen {
			out[i].Color = val
			continue
		}
		index[key] = len(out)
		out = append(out, keymap.KeyColor{Key: key, Color: val})
	}
	*c = out
	return nil
}
