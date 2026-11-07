// Command checklinks verifies that every relative link and image in the Markdown files points at
// a file that exists (and, for #anchors, at a heading that exists). It works offline: external URLs
// are not fetched, so it never flakes and CI can require it.
//
//	go run ./cmd/checklinks [DIR]      DIR defaults to the repository root (found from the current directory)
package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// skipDirs are never scanned: version control, dependencies and raw hardware dumps.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "__pycache__": true, "collected-hardware-data": true}

// link matches [text](target) and ![alt](target), with an optional "title". RE2 has no look-behind,
// so an image is matched too and treated like a link (an image with a title is checked as well).
var (
	link    = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	fence   = regexp.MustCompile("(?s)```.*?```")
	heading = regexp.MustCompile(`^#{1,6}\s+(.*)`)
	scheme  = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)
)

// slug is GitHub's anchor for a heading: lower case, markup dropped, punctuation dropped, blanks to dashes.
func slug(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	var b strings.Builder
	for _, r := range h {
		switch {
		case r == '`' || r == '*' || r == '_':
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// anchors are the heading anchors of a Markdown text (headings inside code fences do not count).
func anchors(text string) map[string]bool {
	out := map[string]bool{}
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if inFence {
			continue
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			out[slug(m[1])] = true
		}
	}
	return out
}

// targets are the link targets of a Markdown text, outside code fences, in order.
func targets(text string) []string {
	var out []string
	for _, m := range link.FindAllStringSubmatch(fence.ReplaceAllString(text, ""), -1) {
		out = append(out, m[1])
	}
	return out
}

// checkFile returns one message per broken link of the Markdown file at path.
func checkFile(root, path string, anchorCache map[string]map[string]bool) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot read (%v)", relTo(root, path), err)}
	}
	var bad []string
	for _, target := range targets(string(b)) {
		if scheme.MatchString(target) || strings.HasPrefix(target, "//") {
			continue // http:, https:, mailto: ...
		}
		rel, frag, _ := strings.Cut(target, "#")
		dest := path
		if rel != "" {
			dest = filepath.Join(filepath.Dir(path), rel)
		}
		if _, err := os.Stat(dest); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %s (no such file)", relTo(root, path), target))
			continue
		}
		if frag == "" || !strings.HasSuffix(dest, ".md") {
			continue
		}
		a, ok := anchorCache[dest]
		if !ok {
			if db, err := os.ReadFile(dest); err == nil {
				a = anchors(string(db))
			}
			anchorCache[dest] = a
		}
		if !a[slug(frag)] && !a[frag] {
			bad = append(bad, fmt.Sprintf("%s: %s (no such heading)", relTo(root, path), target))
		}
	}
	return bad
}

func relTo(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}

// run checks every Markdown file under root and returns the exit code.
func run(root string, stdout io.Writer) int {
	var bad []string
	cache := map[string]map[string]bool{}
	//nolint:gosec // root is the directory the user running the tool asked to check
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			bad = append(bad, checkFile(root, path, cache)...)
		}
		return nil
	})
	if err != nil {
		bad = append(bad, fmt.Sprintf("walk %s: %v", root, err))
	}
	sort.Strings(bad)
	for _, b := range bad {
		_, _ = fmt.Fprintln(stdout, "BROKEN", b)
	}
	_, _ = fmt.Fprintf(stdout, "%d broken link(s)\n", len(bad))
	if len(bad) > 0 {
		return 1
	}
	return 0
}

// findRoot walks up from dir to the directory that holds RELEASE_NOTES.md (the repository root).
func findRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "RELEASE_NOTES.md")); err == nil {
			return dir, nil
		}
		if parent := filepath.Dir(dir); parent != dir {
			dir = parent
			continue
		}
		return "", fmt.Errorf("no RELEASE_NOTES.md in the current directory or above: give the repository root as the argument")
	}
}

func main() {
	root := ""
	if len(os.Args) > 1 {
		root = os.Args[1]
	} else {
		var err error
		if root, err = findRoot("."); err != nil {
			fmt.Fprintln(os.Stderr, "checklinks:", err)
			os.Exit(2)
		}
	}
	os.Exit(run(root, os.Stdout))
}
