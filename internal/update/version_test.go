package update

import (
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Version
		ok   bool
	}{
		{"0.3.17", Version{0, 3, 17}, true}, {"v0.3.17", Version{0, 3, 17}, true}, {"  v1.20.300 \n", Version{1, 20, 300}, true}, {"v007.01.0", Version{7, 1, 0}, true},
		{"", Version{}, false}, {"v", Version{}, false}, {"1.2", Version{}, false}, {"1.2.3.4", Version{}, false}, {"1.2.3-rc1", Version{}, false}, {"V1.2.3", Version{}, false},
		{"vv1.2.3", Version{}, false}, {"1.2.x", Version{}, false}, {"-1.2.3", Version{}, false}, {"1. 2.3", Version{}, false}, {"1.2.3\n4", Version{}, false},
		{"99999999999999999999.0.0", Version{}, false}, {"١.٢.٣", Version{}, false},
	} {
		got, ok := ParseVersion(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestVersionOrderIsNumericNotAlphabetical(t *testing.T) {
	order := []string{"0.3.2", "0.3.9", "0.3.10", "0.3.15", "0.3.16", "0.3.17", "0.4.0", "1.0.0", "10.0.0"}
	shuffled := append([]string(nil), order...)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	sort.Slice(shuffled, func(i, j int) bool {
		a, _ := ParseVersion(shuffled[i])
		b, _ := ParseVersion(shuffled[j])
		return a.Compare(b) < 0
	})
	if strings.Join(shuffled, " ") != strings.Join(order, " ") {
		t.Fatalf("sorted = %v", shuffled)
	}
	a, _ := ParseVersion("v0.3.16")
	b, _ := ParseVersion("0.3.16")
	if a.Compare(b) != 0 || !a.AtLeast(b) || a.String() != "0.3.16" {
		t.Error("v-prefix must not matter")
	}
	if !SumsRequiredFrom.AtLeast(Version{0, 3, 16}) || (Version{0, 3, 15}).AtLeast(SumsRequiredFrom) {
		t.Error("the checksum rule starts at 0.3.16")
	}
}

func FuzzParseVersion(f *testing.F) {
	for _, s := range []string{"0.3.17", "v1.2.3", "", "1.2", "v1.2.3-rc", "99999999999999999999.1.1", "\x00", " v1.2.3 "} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		v, ok := ParseVersion(in)
		if !ok {
			if v != (Version{}) {
				t.Fatalf("not ok but %v", v)
			}
			return
		}
		// canonical form round-trips and compares equal to itself
		back, ok2 := ParseVersion(v.String())
		if !ok2 || back != v || v.Compare(v) != 0 {
			t.Fatalf("%q -> %v -> %q -> %v", in, v, v.String(), back)
		}
		for _, n := range v {
			if n < 0 {
				t.Fatalf("negative component in %v", v)
			}
		}
	})
}
