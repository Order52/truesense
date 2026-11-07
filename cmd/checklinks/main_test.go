package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, text string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, root string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	return run(root, &out), out.String()
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Install":                   "install",
		"What `get.sh` does":        "what-getsh-does",
		"Fans & power: the rules!":  "fans--power-the-rules",
		"  Spaces  ":                "spaces",
		"snake_case and *emphasis*": "snakecase-and-emphasis",
		"Überblick (Größe)":         "überblick-größe",
		"Other models (2 steps)":    "other-models-2-steps",
		"A-B":                       "a-b",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinksAreChecked(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/a.md", "# A\n\n## Second part\n\ntext\n")
	write(t, root, "docs/img.png", "x")
	write(t, root, "README.md", strings.Join([]string{
		"[ok file](docs/a.md)",
		"[ok anchor](docs/a.md#second-part)",
		"[ok title](docs/a.md \"a title\")",
		"[same page](#top-level)",
		"![ok image](docs/img.png)",
		"[web](https://example.com/x) [mail](mailto:a@b.c) [proto](//cdn.example/x)",
		"[bad file](docs/missing.md)",
		"[bad anchor](docs/a.md#nope)",
		"![bad image](docs/gone.png)",
		"![bad image with title](docs/gone2.png \"t\")",
		"[bad same page](#no-such)",
		"# Top level",
		"```",
		"[in a fence](docs/not-checked.md)",
		"# not a heading",
		"```",
	}, "\n")+"\n")
	write(t, root, "node_modules/x.md", "[bad](nope.md)\n")
	write(t, root, ".git/y.md", "[bad](nope.md)\n")

	code, out := check(t, root)
	if code != 1 {
		t.Fatalf("code = %d, want 1:\n%s", code, out)
	}
	for _, want := range []string{
		"BROKEN README.md: docs/missing.md (no such file)",
		"BROKEN README.md: docs/a.md#nope (no such heading)",
		"BROKEN README.md: docs/gone.png (no such file)",
		"BROKEN README.md: docs/gone2.png (no such file)",
		"BROKEN README.md: #no-such (no such heading)",
		"5 broken link(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"not-checked", "node_modules", ".git/", "docs/img.png", "example.com", "a@b.c", "a.md \"a title"} {
		if strings.Contains(out, not) {
			t.Errorf("output mentions %q:\n%s", not, out)
		}
	}
}

func TestACleanTreeExitsZero(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "[b](b.md#title)\n")
	write(t, root, "b.md", "# Title\n")
	if code, out := check(t, root); code != 0 || !strings.Contains(out, "0 broken link(s)") {
		t.Errorf("code %d, output %q", code, out)
	}
}

func TestTheRealDocumentationHasNoBrokenLinks(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if code, out := check(t, root); code != 0 {
		t.Errorf("broken links in the repository:\n%s", out)
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "RELEASE_NOTES.md", "x")
	write(t, root, "a/b/c.txt", "x")
	if got, err := findRoot(filepath.Join(root, "a", "b")); err != nil || got != root {
		t.Errorf("findRoot = %q, %v", got, err)
	}
	if _, err := findRoot(t.TempDir()); err == nil {
		t.Error("a tree without RELEASE_NOTES.md has no root")
	}
}

func FuzzLinks(f *testing.F) {
	for _, s := range []string{"[a](b.md#c)", "![x](y \"t\")", "```\n[a](b)\n```", "# H\n[z](#h)", "[](", "\xff[a](\xfe)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		_ = targets(text)
		_ = anchors(text)
		_ = slug(text)
	})
}
