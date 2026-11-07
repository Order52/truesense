package update

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var sumLineRE = regexp.MustCompile(`^([0-9a-fA-F]{64})\s+\*?(\S+)$`)

// ParseSums reads a SHA256SUMS file (the `sha256sum` output format: a 64-digit hash, blanks,
// an optional "*" for binary mode, then the file name) into {file name: lowercase hash}. Lines
// that are not in that format are ignored, like `sha256sum -c --ignore-missing` would. Only
// the base name counts, so a list made from a "dist/" directory still matches.
//
// A file named twice with different hashes is an error: the list is the trust anchor of the
// download, and an ambiguous anchor must not be resolved by picking one.
func ParseSums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		m := sumLineRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		name, sum := path.Base(m[2]), strings.ToLower(m[1])
		if name == "/" || name == "." || name == ".." { // not a file name
			continue
		}
		if prev, dup := out[name]; dup && prev != sum {
			return nil, fmt.Errorf("SHA256SUMS lists %s twice with different hashes", name)
		}
		out[name] = sum
	}
	return out, nil
}
