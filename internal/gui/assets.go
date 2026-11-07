package gui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed web
var webFS embed.FS

// asset is one embedded file, prepared once: the front-end is part of the binary.
type asset struct {
	body []byte
	typ  string
	etag string
}

type assets struct{ files map[string]asset }

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".svg":  "image/svg+xml",
	".json": "application/json; charset=utf-8",
	".png":  "image/png",
	".txt":  "text/plain; charset=utf-8",
}

func loadAssets() *assets {
	a := &assets{files: map[string]asset{}}
	root, err := fs.Sub(webFS, "web")
	if err != nil {
		return a
	}
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		typ, ok := contentTypes[path.Ext(p)]
		if !ok {
			return nil // only known types are ever served
		}
		body, err := fs.ReadFile(root, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(body)
		a.files["/"+p] = asset{body: body, typ: typ, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		return nil
	})
	return a
}

// names lists the served paths (tests check the front-end is really embedded).
func (a *assets) names() []string {
	out := make([]string, 0, len(a.files))
	for p := range a.files {
		out = append(out, p)
	}
	return out
}

func (a *assets) serve(w http.ResponseWriter, r *http.Request) {
	p := path.Clean("/" + r.URL.Path)
	if p == "/" {
		p = "/index.html"
	}
	f, ok := a.files[p]
	if !ok || strings.Contains(r.URL.Path, "..") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.typ)
	h.Set("ETag", f.etag)
	h.Set("Cache-Control", "no-cache") // revalidated with the ETag: a new binary shows its new page
	if r.Header.Get("If-None-Match") == f.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(f.body)
}
