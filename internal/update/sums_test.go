package update

import (
	"strings"
	"testing"
)

func TestParseSums(t *testing.T) {
	h1, h2 := strings.Repeat("a", 64), strings.Repeat("B", 64)
	got, err := ParseSums([]byte(h1 + "  truesense-0.3.17.tar.gz\n" + h2 + " *dist/other.tar.gz\r\n" + "\n# comment\n" + "short  file\n" + h1 + "\tkernel.tar.gz\n" + strings.Repeat("g", 64) + "  nothex\n" + h1 + "  has space.tar.gz\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"truesense-0.3.17.tar.gz": h1, "other.tar.gz": strings.ToLower(h2), "kernel.tar.gz": h1}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseSumsRefusesAmbiguousLists(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	if _, err := ParseSums([]byte(a + "  f.tar.gz\n" + b + "  f.tar.gz\n")); err == nil {
		t.Error("two different hashes for one file must be refused")
	}
	if _, err := ParseSums([]byte(a + "  f.tar.gz\n" + a + "  dist/f.tar.gz\n")); err != nil {
		t.Errorf("a repeated identical line is harmless: %v", err)
	}
	got, err := ParseSums(nil)
	if err != nil || len(got) != 0 {
		t.Errorf("empty list: %v, %v", got, err)
	}
}

func FuzzParseSums(f *testing.F) {
	h := strings.Repeat("0123456789abcdef", 4)
	for _, s := range []string{h + "  truesense-0.3.17.tar.gz\n", h + " *x\r\n" + h + "  y", "", "\x00\xff", h + "  ../../etc/passwd\n", h + "  \n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		sums, err := ParseSums(data)
		if err != nil {
			return
		}
		for name, sum := range sums {
			if len(sum) != 64 || sum != strings.ToLower(sum) || strings.Trim(sum, "0123456789abcdef") != "" {
				t.Fatalf("bad hash %q for %q", sum, name)
			}
			if name == "" || strings.Contains(name, "/") || strings.ContainsAny(name, " \t\r\n") {
				t.Fatalf("a file name must be a bare base name, got %q", name)
			}
		}
	})
}
