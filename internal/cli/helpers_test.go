package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/apitest"
	"github.com/Order52/truesense/internal/update"
)

func releaseServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag, "html_url": "https://example.test/r", "body": "notes"})
		case strings.HasSuffix(r.URL.Path, "/tags"):
			_ = json.NewEncoder(w).Encode([]map[string]string{{"name": tag}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func updaterFor(f *apitest.Fixture, srv *httptest.Server, current string) *update.Updater {
	return &update.Updater{Repo: "owner/name", APIBase: srv.URL, WebBase: srv.URL, HTTP: srv.Client(), CachePath: filepath.Join(f.Dir, "update.json"),
		Current: current, CheckHours: 24, Now: func() time.Time { return time.Unix(1_700_000_000, 0) }, Layout: update.DefaultLayout()}
}
