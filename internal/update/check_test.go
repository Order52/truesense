package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tagList(t testing.TB, names ...string) string {
	var tags []map[string]string
	for _, n := range names {
		tags = append(tags, map[string]string{"name": n})
	}
	return jsonString(t, tags)
}

func releaseJSON(t testing.TB, tag, body string) string {
	return jsonString(t, map[string]string{"tag_name": tag, "html_url": "https://github.com/Order52/truesense/releases/tag/" + tag, "body": body})
}

func TestLatestIsTheHighestOfTheReleaseAndTheTags(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{
		"/repos/Order52/truesense/releases/latest": releaseJSON(t, "v0.3.16", strings.Repeat("é", 2000)),
		"/repos/Order52/truesense/tags":            tagList(t, "v0.3.17", "v0.3.16", "v0.3.9", "junk", "v1.2"),
	})
	info, err := updaterFor(t, g).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "0.3.17" || info.Tag != "v0.3.17" || info.URL != g.URL+"/Order52/truesense/releases/tag/v0.3.17" || info.Notes != "" {
		t.Fatalf("info = %+v (a tag without a release page can be newer)", info)
	}
	g2 := newFakeGitHub(t, map[string]any{
		"/repos/Order52/truesense/releases/latest": releaseJSON(t, "v0.3.17", strings.Repeat("é", 2000)),
		"/repos/Order52/truesense/tags":            tagList(t, "v0.3.17", "v0.3.9"),
	})
	info, err = updaterFor(t, g2).Latest(context.Background())
	if err != nil || info.Tag != "v0.3.17" || len([]rune(info.Notes)) != 1500 || !strings.HasSuffix(info.URL, "/releases/tag/v0.3.17") {
		t.Fatalf("info = %+v, %v (the release page and its notes win, cut to 1500 characters)", info, err)
	}
}

func TestLatestWorksWithoutAnyReleasePage(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.9", "v0.3.15", "v0.3.2")})
	info, err := updaterFor(t, g).Latest(context.Background())
	if err != nil || info.Version != "0.3.15" {
		t.Fatalf("info = %+v, %v (a 404 for the release page is normal)", info, err)
	}
}

func TestLatestErrors(t *testing.T) {
	cases := map[string]map[string]any{
		"no tags at all":     {"/repos/Order52/truesense/tags": "[]"},
		"only junk tags":     {"/repos/Order52/truesense/tags": tagList(t, "nightly", "v1")},
		"rate limited":       {"/repos/Order52/truesense/releases/latest": 403},
		"tags are an error":  {"/repos/Order52/truesense/tags": 500},
		"tags are not JSON":  {"/repos/Order52/truesense/tags": "<html>"},
		"release not JSON":   {"/repos/Order52/truesense/releases/latest": "garbage", "/repos/Order52/truesense/tags": "[]"},
		"tags are an object": {"/repos/Order52/truesense/tags": `{"name":"v1.0.0"}`},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := updaterFor(t, newFakeGitHub(t, files)).Latest(context.Background()); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestCheckReportsAvailableAndCaches(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if strings.HasSuffix(r.URL.Path, "/tags") {
			_, _ = w.Write([]byte(tagList(t, "v0.3.17")))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	u := New("Order52/truesense")
	u.APIBase, u.WebBase, u.HTTP = srv.URL, srv.URL, srv.Client()
	u.CachePath = filepath.Join(t.TempDir(), "c", "update.json")
	u.Current = "0.3.15"
	now := time.Unix(1_700_000_000, 0)
	u.Now = func() time.Time { return now }

	st := u.Check(context.Background(), false, 0)
	if st.Error != nil || !st.Available || st.Latest == nil || *st.Latest != "0.3.17" || st.Current != "0.3.15" || st.CheckedAt != 1_700_000_000 {
		t.Fatalf("status = %+v", st)
	}
	first := hits
	now = now.Add(time.Hour)
	if st = u.Check(context.Background(), false, 0); !st.Available || hits != first {
		t.Fatalf("a fresh cache must answer without the network: hits %d -> %d", first, hits)
	}
	if st = u.Check(context.Background(), true, 0); hits == first {
		t.Fatal("force must ask again")
	}
	hits = 0
	now = now.Add(48 * time.Hour)
	if u.Check(context.Background(), false, 0); hits == 0 {
		t.Fatal("a cache older than check_hours must be refreshed")
	}
	hits = 0
	if u.Check(context.Background(), false, 1000); hits != 0 {
		t.Fatal("an explicit longer freshness window must keep the cache")
	}
	u.Current = "0.3.17"
	if st = u.Check(context.Background(), false, 0); st.Available {
		t.Fatalf("up to date reported as available: %+v", st)
	}
	u.Current = "0.4.0"
	if st = u.Check(context.Background(), false, 0); st.Available {
		t.Fatalf("a newer local build must not be offered a downgrade: %+v", st)
	}
}

func TestCheckReportsAnUnreachableGitHubWithTheCachedAnswer(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	u := updaterFor(t, g)
	if st := u.Check(context.Background(), false, 0); st.Error != nil || !st.Available {
		t.Fatalf("setup: %+v", st)
	}
	g.Close()
	u.Now = func() time.Time { return time.Unix(1_700_000_000+3*24*3600, 0) }
	st := u.Check(context.Background(), false, 0)
	if st.Error == nil || !strings.HasPrefix(*st.Error, "could not reach GitHub: ") {
		t.Fatalf("status = %+v", st)
	}
	if st.Latest == nil || *st.Latest != "0.3.17" || !st.Available || st.Info == nil {
		t.Fatalf("the cached answer must still be shown: %+v", st)
	}
	// and with no cache at all
	u.CachePath = filepath.Join(t.TempDir(), "none.json")
	st = u.Check(context.Background(), false, 0)
	if st.Error == nil || st.Latest != nil || st.Available || st.Info != nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestCheckStatusJSONNamesAreTheOldOnes(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	b, err := json.Marshal(updaterFor(t, g).Check(context.Background(), true, 0))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"current", "latest", "available", "checked_at", "error", "info"} {
		if _, ok := m[k]; !ok {
			t.Errorf("status lacks %q: %s", k, b)
		}
	}
	for _, k := range []string{"version", "tag", "url", "notes"} {
		if _, ok := m["info"].(map[string]any)[k]; !ok {
			t.Errorf("info lacks %q: %s", k, b)
		}
	}
}

func TestACacheWrittenByThePythonUpdaterIsRead(t *testing.T) {
	py := `{"checked_at": 1699999000.25, "info": {"version": "0.3.17", "tag": "v0.3.17", "url": "u", "notes": ""}, "error": null}`
	u := updaterFor(t, newFakeGitHub(t, nil)) // a server that knows nothing: only the cache can answer
	if err := os.MkdirAll(filepath.Dir(u.CachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(u.CachePath, []byte(py), 0o600); err != nil {
		t.Fatal(err)
	}
	st := u.Check(context.Background(), false, 0)
	if st.Error != nil || !st.Available || st.CheckedAt != 1699999000.25 {
		t.Fatalf("status = %+v", st)
	}
}

func TestACorruptCacheIsIgnored(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	u := updaterFor(t, g)
	if err := os.MkdirAll(filepath.Dir(u.CachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(u.CachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := u.Check(context.Background(), false, 0); st.Error != nil || !st.Available {
		t.Fatalf("status = %+v", st)
	}
}

func TestAReadOnlyCacheLocationIsNotAnError(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{"/repos/Order52/truesense/tags": tagList(t, "v0.3.17")})
	u := updaterFor(t, g)
	u.CachePath = "/proc/nonexistent/update.json"
	if st := u.Check(context.Background(), true, 0); st.Error != nil || !st.Available {
		t.Fatalf("status = %+v", st)
	}
}

func TestChecksumsMissingListIsNilOnlyForA404(t *testing.T) {
	g := newFakeGitHub(t, map[string]any{repoPath + "/releases/download/v1.0.0/SHA256SUMS": strings.Repeat("a", 64) + "  *dist/truesense-1.0.0.tar.gz\r\nnot a line\n"})
	u := updaterFor(t, g)
	sums, err := u.Checksums(context.Background(), "v1.0.0")
	if err != nil || sums["truesense-1.0.0.tar.gz"] != strings.Repeat("a", 64) || len(sums) != 1 {
		t.Fatalf("sums = %v, %v", sums, err)
	}
	if sums, err := u.Checksums(context.Background(), "v9.9.9"); sums != nil || err != nil {
		t.Fatalf("404 = %v, %v", sums, err)
	}
}

func TestSystemRepoComesOnlyFromTheSystemConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, tc := range []struct {
		name, text, want string
		fails            bool
	}{
		{"default", "", "Order52/truesense", false},
		{"override", "[update]\nrepo = \"someone/fork.v2\"\n", "someone/fork.v2", false},
		{"traversal", "[update]\nrepo = \"../../etc\"\n", "", true},
		{"query", "[update]\nrepo = \"a/b?x=1\"\n", "", true},
		{"single", "[update]\nrepo = \"onlyname\"\n", "", true},
		{"broken toml", "[update\n", "", true},
	} {
		got, err := SystemRepo(write(tc.name+".toml", tc.text))
		if (err != nil) != tc.fails || got != tc.want {
			t.Errorf("%s: got %q, %v", tc.name, got, err)
		}
	}
	if got, err := SystemRepo(filepath.Join(dir, "missing.toml")); err != nil || got != "Order52/truesense" {
		t.Errorf("missing system config: %q, %v", got, err)
	}
}

func TestValidRepo(t *testing.T) {
	for r, want := range map[string]bool{"Order52/truesense": true, "a/b": true, "a.b/c-d_e": true, "": false, "a": false, "a/b/c": false, "a/b c": false, "../..": false, "./.": false, "a/b\n": false, "a/b?x": false} {
		if got := ValidRepo(r); got != want {
			t.Errorf("ValidRepo(%q) = %v", r, got)
		}
	}
}
