package release

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const notesFile = "RELEASE_NOTES.md"

// footer closes every GitHub release text: how to install and what was verified. It is
// the same for every release, so it lives here and not in RELEASE_NOTES.md.
const footer = "\n**Install / update:** `curl -fsSL https://raw.githubusercontent.com/Order52/truesense/main/tools/get.sh | sudo bash`, or from the window: Settings, Update (on a desktop without a polkit agent a terminal window opens for the password), or `truesense update apply` in a terminal.\n\n" +
	"Tested on one machine (PH16-71, BIOS V1.18, CachyOS). What is verified and what is not is listed in docs/STATUS.md. Every release asset is listed in SHA256SUMS and carries a build provenance attestation (`gh attestation verify <file> --repo Order52/truesense`); the SBOM (`.sbom.cdx.json`) lists the Go modules compiled into the binary.\n"

func read(root, rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, rel))
	return string(b), err
}

func write(root, rel, text string) error {
	return os.WriteFile(filepath.Join(root, rel), []byte(text), 0o644) // #nosec G306 -- an existing file keeps its mode; new files are documents
}

// reason is the short human reason of a file error ("no such file or directory").
func reason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// Current returns the version the source of truth (internal/buildinfo) declares.
func Current(root string) (string, error) {
	first := Table()[0]
	text, err := read(root, first.File)
	if err != nil {
		return "", fmt.Errorf("cannot find Version in %s: %s", first.File, reason(err))
	}
	m := first.re.FindStringSubmatch(text)
	if m == nil {
		return "", fmt.Errorf("cannot find Version in %s", first.File)
	}
	return m[1], nil
}

// Check returns the list of problems that make this tree unreleasable (empty means consistent):
// every versioned file agrees with buildinfo, the release notes have a section for the version
// (or "Unreleased"), and the tag, when given, is this version. The error is for things that stop the
// check itself (no version found, a tag that is not a version).
func Check(root, tag string) ([]string, error) {
	want, err := Current(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, e := range Table() {
		text, err := read(root, e.File)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: cannot read (%s)", e.File, reason(err)))
			continue
		}
		found := e.re.FindAllStringSubmatch(text, -1)
		if len(found) != e.Count {
			problems = append(problems, fmt.Sprintf("%s: expected %d version mention(s) like %q, found %d", e.File, e.Count, e.Pattern, len(found)))
		}
		for _, m := range found {
			if m[1] != want {
				problems = append(problems, fmt.Sprintf("%s: says %s, buildinfo.Version says %s", e.File, m[1], want))
			}
		}
	}
	notes, err := read(root, notesFile)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", notesFile, reason(err))
	}
	if !regexp.MustCompile(`(?m)^## `+regexp.QuoteMeta(want)+` \(`).MatchString(notes) && !strings.Contains(notes, "## Unreleased") {
		problems = append(problems, fmt.Sprintf("%s: no section for %s", notesFile, want))
	}
	if tag != "" {
		tv, err := ParseVersion(tag)
		if err != nil {
			return nil, err
		}
		wv, err := ParseVersion(want)
		if err != nil {
			return nil, err
		}
		if tv != wv {
			problems = append(problems, fmt.Sprintf("tag %s does not match version %s", tag, want))
		}
	}
	return problems, nil
}

// sectionEnd is the index of the next "## " heading at or after from (len(lines) when there is none).
func sectionEnd(lines []string, from int) int {
	for j := from; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "## ") {
			return j
		}
	}
	return len(lines)
}

// Section returns the notes of one version without their heading; ok is false when there is no such
// section. RE2 has no look-ahead, so this walks the lines: a section runs to the next "## " heading.
func Section(notes, version string) (body string, ok bool) {
	lines := strings.Split(notes, "\n")
	prefix := "## " + version + " ("
	for i, l := range lines {
		if !strings.HasPrefix(l, prefix) || !strings.Contains(l[len(prefix):], ")") || i == len(lines)-1 {
			continue
		}
		return strings.Trim(strings.Join(lines[i+1:sectionEnd(lines, i+1)], "\n"), "\n"), true
	}
	return "", false
}

// Title is the short title after the date in a version's heading ("## 0.3.17 (2026-01-01): title"), or "".
// It reads lines instead of compiling the version into a regular expression: the version comes from the
// command line and may be anything.
func Title(notes, version string) string {
	prefix := "## " + version + " ("
	for _, l := range strings.Split(notes, "\n") {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		_, after, found := strings.Cut(l[len(prefix):], ")")
		if !found {
			continue
		}
		if t, ok := strings.CutPrefix(after, ":"); ok {
			return strings.TrimSpace(t)
		}
		return ""
	}
	return ""
}

// Notes returns the GitHub release text of a version: a headline, its notes from RELEASE_NOTES.md and the install footer.
func Notes(root, version string) (string, error) {
	text, err := read(root, notesFile)
	if err != nil {
		return "", fmt.Errorf("%s: %s", notesFile, reason(err))
	}
	body, _ := Section(text, version)
	if body == "" {
		return "", fmt.Errorf("%s has no notes for %s", notesFile, version)
	}
	head := fmt.Sprintf("TrueSense %s for the Acer Predator Helios 16 (PH16-71)", version)
	if t := Title(text, version); t != "" {
		head += ": " + t + "."
	} else {
		head += "."
	}
	return strings.TrimRight(head+"\n\n"+body+"\n"+footer, "\n"), nil
}

// Bump rewrites the version only where the table says it lives and returns the old one.
func Bump(root, version string) (old string, err error) {
	old, err = Current(root)
	if err != nil {
		return "", err
	}
	for _, e := range Table() {
		text, err := read(root, e.File)
		if err != nil {
			return "", fmt.Errorf("bump %s: %w", e.File, err)
		}
		var out strings.Builder
		last := 0
		for _, ix := range e.re.FindAllStringSubmatchIndex(text, -1) {
			out.WriteString(text[last:ix[2]])
			out.WriteString(version)
			last = ix[3]
		}
		out.WriteString(text[last:])
		if err := write(root, e.File, out.String()); err != nil {
			return "", fmt.Errorf("bump %s: %w", e.File, err)
		}
	}
	return old, nil
}

// Unreleased returns the notes collected under "## Unreleased" (what has changed since the last release).
func Unreleased(root string) (string, error) {
	text, err := read(root, notesFile)
	if err != nil {
		return "", fmt.Errorf("%s: %s", notesFile, reason(err))
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l != "## Unreleased" || i == len(lines)-1 {
			continue
		}
		return strings.TrimSpace(strings.Join(lines[i+1:sectionEnd(lines, i+1)], "\n")), nil
	}
	return "", nil
}

// Retitle turns the "## Unreleased" heading into this version's heading with its date and title.
func Retitle(root, version, title, today string) error {
	text, err := read(root, notesFile)
	if err != nil {
		return fmt.Errorf("%s: %s", notesFile, reason(err))
	}
	head := fmt.Sprintf("## %s (%s)", version, today)
	if title != "" {
		head += ": " + title
	}
	return write(root, notesFile, strings.Replace(text, "## Unreleased", head, 1))
}
