package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// entry is one member of a test archive.
type entry struct {
	name     string
	body     string
	typeflag byte
	link     string
	mode     int64
}

func archive(t testing.TB, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: tf, Mode: mode, Linkname: e.link}
		if tf == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if tf == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseTarball is what the release pipeline publishes for version, claiming to be `inside`.
func releaseTarball(t testing.TB, version, inside string) []byte {
	if inside == "" {
		inside = version
	}
	top := "truesense-" + version + "/"
	return archive(t,
		entry{name: top, typeflag: tar.TypeDir, mode: 0o755},
		entry{name: top + "tools/install.sh", body: "#!/bin/sh\nexit 0\n", mode: 0o644},
		entry{name: top + "src/truesense/__init__.py", body: fmt.Sprintf("__version__ = %q\n", inside)},
		entry{name: top + "kernel/truesense-wmi/truesense_wmi.c", body: "/* module */\n"},
	)
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fakeGitHub serves a map of path -> body ([]byte or string) or HTTP status (int) and records
// every request path, so tests can prove what was NOT asked for.
type fakeGitHub struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newFakeGitHub(t testing.TB, files map[string]any) *fakeGitHub {
	g := &fakeGitHub{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.paths = append(g.paths, r.URL.Path)
		g.mu.Unlock()
		switch v := files[r.URL.Path].(type) {
		case nil:
			http.NotFound(w, r)
		case int:
			w.WriteHeader(v)
		case string:
			_, _ = w.Write([]byte(v))
		case []byte:
			_, _ = w.Write(v)
		}
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGitHub) asked(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range g.paths {
		if p == path {
			return true
		}
	}
	return false
}

const repoPath = "/Order52/truesense"

// release returns the files a published release of version has on the fake GitHub.
type releaseOpts struct {
	sums    any // "auto" (default), nil (no list), string/[]byte (that list), int (status)
	noAsset bool
	inside  string
	archive bool // also serve GitHub's tag archive
	tamper  bool // serve other bytes than the ones the list vouches for
}

func release(t testing.TB, version string, o releaseOpts) map[string]any {
	blob := releaseTarball(t, version, o.inside)
	base := fmt.Sprintf("%s/releases/download/v%s", repoPath, version)
	f := map[string]any{}
	served := blob
	if o.tamper {
		served = append(append([]byte(nil), blob...), 0)
	}
	if !o.noAsset {
		f[fmt.Sprintf("%s/truesense-%s.tar.gz", base, version)] = served
	}
	switch s := o.sums.(type) {
	case nil:
	case string:
		if s == "auto" {
			f[base+"/SHA256SUMS"] = fmt.Sprintf("%s  truesense-%s.tar.gz\n", sha(blob), version)
		} else {
			f[base+"/SHA256SUMS"] = s
		}
	case []byte:
		f[base+"/SHA256SUMS"] = s
	case int:
		f[base+"/SHA256SUMS"] = s
	}
	if o.archive {
		f[fmt.Sprintf("%s/archive/refs/tags/v%s.tar.gz", repoPath, version)] = blob
	}
	return f
}

func merge(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// updaterFor points an Updater at the fake GitHub with a private cache and temp dir.
func updaterFor(t *testing.T, g *fakeGitHub) *Updater {
	t.Helper()
	u := New("Order52/truesense")
	u.APIBase, u.WebBase = g.URL, g.URL
	u.HTTP = g.Client()
	dir := t.TempDir()
	u.CachePath = filepath.Join(dir, "cache", "update.json")
	u.TempDir = filepath.Join(dir, "tmp")
	mustMkdir(t, u.TempDir)
	u.Current = "0.3.15"
	u.Now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	u.Host = Host{EUID: 1000, Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { return "", errNotFound }, ProcRoot: filepath.Join(dir, "proc"),
		InstalledPath: filepath.Join(dir, "installed", "__init__.py"),
		Exec: func(context.Context, []string) (int, error) {
			t.Error("a test must never run an installer")
			return 1, nil
		}}
	return u
}

func leftovers(t *testing.T, u *Updater) []string {
	t.Helper()
	return listDir(t, u.TempDir)
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := readDirNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return entries
}

func jsonString(t testing.TB, v any) string {
	b, err := json.MarshalIndent(v, "", "  ") // GitHub pretty-prints
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func contains(err error, parts ...string) bool {
	if err == nil {
		return false
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			return false
		}
	}
	return true
}
