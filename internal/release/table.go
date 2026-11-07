// Package release is the release tooling of the repository: the one place that knows where the
// version number lives, which checks a release must pass, how the release notes are cut and how a
// release is prepared (bump, retitle, gate, commit, tag). It backs cmd/release; CI and the
// maintainer run the same code, so "it passed on my machine" and "it passed in the pipeline" mean the
// same thing.
package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// semver is the shape of a version number in every file of the table.
const semver = `\d+\.\d+\.\d+`

// Entry says where a version number is written: the pattern's first group is the version, and the
// file must contain exactly Count matches. Listing the places explicitly (instead of replacing
// every occurrence of the old number) keeps a stray mention of the same digits elsewhere in a file
// untouched when the version is bumped.
type Entry struct {
	File    string
	Pattern string
	Count   int
	re      *regexp.Regexp
}

// Table returns the places the version lives in the Go end state. The first entry is the source
// of truth (internal/buildinfo, compiled into the binary); every other file must agree with it.
func Table() []Entry {
	e := func(file, pattern string, count int) Entry {
		p := strings.ReplaceAll(pattern, "SEMVER", semver)
		return Entry{File: file, Pattern: p, Count: count, re: regexp.MustCompile("(?m)" + p)}
	}
	return []Entry{
		e("internal/buildinfo/buildinfo.go", `const Version = "(SEMVER)"`, 1),
		e("README.md", `badge/version-(SEMVER)-`, 1),
		e("docs/INSTALL.md", `TRUESENSE_VERSION=(SEMVER)`, 1),
		e("kernel/truesense-wmi/dkms.conf", `PACKAGE_VERSION="(SEMVER)"`, 1),
		e("kernel/truesense-wmi/truesense_wmi.c", `#define TS_VERSION\s+"(SEMVER)"`, 1),
		e("tools/aur-publish.sh", `tools/aur-publish\.sh (SEMVER)`, 1),
		e("tools/get.sh", `TRUESENSE_VERSION=(SEMVER) \(pin`, 1),
		e("packaging/arch/PKGBUILD", `^pkgver=(SEMVER)$`, 1),
		e("packaging/arch/PKGBUILD", `truesense-wmi-(SEMVER)`, 2),
		e("packaging/aur/PKGBUILD.template", `^pkgver=(SEMVER)$`, 1),
		e("packaging/aur/PKGBUILD.template", `truesense-wmi-(SEMVER)`, 2),
	}
}

// Version is a parsed X.Y.Z release number.
type Version [3]int

var versionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// ParseVersion accepts "X.Y.Z" with an optional leading "v" (a git tag) and surrounding blanks.
func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{}, fmt.Errorf("not a version: %q (expected X.Y.Z)", s)
	}
	var v Version
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, fmt.Errorf("not a version: %q (expected X.Y.Z)", s)
		}
		v[i] = n
	}
	return v, nil
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
