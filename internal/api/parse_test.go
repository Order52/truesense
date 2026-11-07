package api_test

import (
	"strings"
	"testing"

	"github.com/Order52/truesense/internal/api"
)

func TestKeyColors(t *testing.T) {
	got, err := api.KeyColors([]string{"w=ff0000", "arrows=00ff00", "esc=#ffffff"})
	must(t, err)
	if len(got) != 3 || got[0].Key != "w" || got[0].Color != "ff0000" || got[2].Color != "#ffffff" {
		t.Fatalf("%+v", got)
	}
	for _, bad := range []string{"w", "w=", "=", ""} {
		if _, err := api.KeyColors([]string{bad}); err == nil || !strings.Contains(err.Error(), "use key=RRGGBB") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// FuzzKeyColors: the items of `keyboard paint` are user text; they either parse into a key and a
// colour or are refused with the usage message, and the order the user gave is kept.
func FuzzKeyColors(f *testing.F) {
	for _, s := range []string{"w=ff0000", "esc", "=", "a=b=c", "wasd=#00ff00", "ключ=цвет", strings.Repeat("x", 300) + "=1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, item string) {
		got, err := api.KeyColors([]string{item, item})
		if err != nil {
			if !strings.Contains(err.Error(), "use key=RRGGBB") {
				t.Fatalf("%q: %v", item, err)
			}
			return
		}
		if len(got) != 2 || got[0] != got[1] || got[0].Color == "" {
			t.Fatalf("%q: %+v", item, got)
		}
	})
}
