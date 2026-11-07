// Package update finds a newer TrueSense release on GitHub and installs it.
//
// Checking only reads public release information (one HTTPS request, no identifiers, at most
// once a day when automatic checks are on). Installing downloads the tagged release from the
// project's repository, verifies it against the release's SHA256SUMS, checks that it really is
// that version, unpacks it with an extractor that cannot write outside its folder, and hands
// over to its installer with administrator rights: the same trust as `curl ... | sudo bash`,
// but only when you ask. The repository comes from the SYSTEM config (root-owned) or the
// built-in default, never from the user's config, so a file the user can write cannot point
// the updater somewhere else.
package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a release number: major.minor.patch, nothing else (no pre-releases, no build
// metadata), because the release pipeline only ever tags plain versions.
type Version [3]int

var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// ParseVersion reads "0.3.17" or "v0.3.17" (surrounding blanks are ignored). ok is false for
// anything else, including numbers too large to be a release.
func ParseVersion(tag string) (v Version, ok bool) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(tag))
	if m == nil {
		return Version{}, false
	}
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, false
		}
		v[i] = n
	}
	return v, true
}

// Compare returns -1, 0 or 1 as v is older than, equal to or newer than o.
func (v Version) Compare(o Version) int {
	for i := range v {
		switch {
		case v[i] < o[i]:
			return -1
		case v[i] > o[i]:
			return 1
		}
	}
	return 0
}

// AtLeast reports whether v is o or newer.
func (v Version) AtLeast(o Version) bool { return v.Compare(o) >= 0 }

// String is the canonical form without a "v" ("0.3.17"), the one used in file names.
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }
