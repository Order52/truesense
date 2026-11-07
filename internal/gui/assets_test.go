package gui

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestFrontEndIsEmbedded(t *testing.T) {
	a := loadAssets()
	for _, want := range []string{"/index.html", "/app.css", "/logo.svg", "/js/app.js", "/js/api.js", "/js/ui.js", "/js/state.js", "/js/pages/index.js"} {
		if _, ok := a.files[want]; !ok {
			t.Errorf("%s is not embedded", want)
		}
	}
	pageModule := regexp.MustCompile(`import\("\./([a-z-]+\.js)"\)`)
	idx := string(a.files["/js/pages/index.js"].body)
	mods := pageModule.FindAllStringSubmatch(idx, -1)
	if len(mods) < 11 {
		t.Fatalf("only %d page modules listed", len(mods))
	}
	for _, m := range mods {
		if _, ok := a.files["/js/pages/"+m[1]]; !ok {
			t.Errorf("page module %s is listed but not embedded", m[1])
		}
	}
	if len(a.names()) < 30 {
		t.Errorf("only %d files embedded", len(a.names()))
	}
}

func TestEveryImportResolves(t *testing.T) {
	a := loadAssets()
	imp := regexp.MustCompile(`(?:from\s+|import\()\s*"(\.{1,2}/[^"]+)"`)
	for p, f := range a.files {
		if !strings.HasSuffix(p, ".js") {
			continue
		}
		for _, m := range imp.FindAllStringSubmatch(string(f.body), -1) {
			target := path.Clean(path.Join(path.Dir(p), m[1]))
			if _, ok := a.files[target]; !ok {
				t.Errorf("%s imports %s, which is not embedded", p, m[1])
			}
		}
	}
}

// The page must be able to run under the strict Content-Security-Policy and must never
// reach beyond its own server: these are the constructs that would break either promise.
func TestFrontEndStaysInsideThePolicy(t *testing.T) {
	a := loadAssets()
	index := string(a.files["/index.html"].body)
	scriptTag := regexp.MustCompile(`(?s)<script\b[^>]*>(.*?)</script>`)
	for _, m := range scriptTag.FindAllStringSubmatch(index, -1) {
		if strings.TrimSpace(m[1]) != "" {
			t.Errorf("inline script in index.html: %q", m[1])
		}
	}
	for _, bad := range []string{" style=", "<style", " onclick=", " onload=", "javascript:", "http://", "https://", "//cdn", "<iframe", "<form"} {
		if strings.Contains(index, bad) {
			t.Errorf("index.html contains %q", bad)
		}
	}

	forbiddenJS := []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", `setAttribute("style"`, `setAttribute('style'`, "XMLHttpRequest", "WebSocket", "importScripts", "sendBeacon", "EventSource", "localStorage.setItem(\"token", "navigator.serviceWorker"}
	url := regexp.MustCompile(`https?://[^\s"'` + "`" + `)]+`)
	allowedURL := map[string]bool{"http://www.w3.org/2000/svg": true, "https://github.com/Order52/truesense": true, "https://github.com/": true}
	for p, f := range a.files {
		body := string(f.body)
		switch path.Ext(p) {
		case ".js":
			for _, bad := range forbiddenJS {
				if strings.Contains(body, bad) {
					t.Errorf("%s uses %s", p, bad)
				}
			}
			for _, u := range url.FindAllString(body, -1) {
				ok := false
				for allowed := range allowedURL {
					if strings.HasPrefix(u, allowed) {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s mentions %s", p, u)
				}
			}
			// every fetch goes to a relative path of this server
			for _, m := range regexp.MustCompile(`fetch\(\s*([^,)]+)`).FindAllStringSubmatch(body, -1) {
				if !strings.HasPrefix(m[1], "`/api/") && !strings.HasPrefix(m[1], `"/api/`) {
					t.Errorf("%s: fetch(%s) is not a /api/ path", p, m[1])
				}
			}
		case ".css":
			for _, bad := range []string{"@import", "@font-face", "http://", "https://", "url(//"} {
				if strings.Contains(body, bad) {
					t.Errorf("%s contains %s", p, bad)
				}
			}
		}
	}
}

func TestEmbeddedTreeHasNoSurprises(t *testing.T) {
	err := fs.WalkDir(webFS, "web", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, ok := contentTypes[path.Ext(p)]; !ok {
			t.Errorf("%s has an extension the server would not serve", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
