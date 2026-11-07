package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Order52/truesense/internal/apitest"
	"github.com/Order52/truesense/internal/update"
)

// release serves the two GitHub endpoints the updater asks for.
func release(t *testing.T, tag string) *httptest.Server {
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

func TestCheckAndPlanUpdate(t *testing.T) {
	f, a := machine(t)
	srv := release(t, "v0.4.0")
	a.Updater = updaterFor(f, srv, "0.3.17")
	st, err := a.CheckUpdate(t.Context(), true)
	must(t, err)
	if !st.Available || st.Latest == nil || *st.Latest != "0.4.0" {
		t.Fatalf("%+v", st)
	}
	plan, err := a.PlanUpdate(t.Context(), "")
	must(t, err)
	if !plan.Needed || plan.Tag != "v0.4.0" {
		t.Fatalf("%+v", plan)
	}
	plan, err = a.PlanUpdate(t.Context(), "v0.3.1")
	must(t, err)
	if !plan.Needed || plan.Tag != "v0.3.1" {
		t.Fatalf("a version asked for is installed even when older: %+v", plan)
	}
	a.Updater = updaterFor(f, srv, "0.4.0")
	if plan, _ := a.PlanUpdate(t.Context(), ""); plan.Needed {
		t.Fatalf("already the newest: %+v", plan)
	}
}

func TestPlanUpdateReportsAnUnreachableGitHub(t *testing.T) {
	f, a := machine(t)
	srv := release(t, "v0.4.0")
	u := updaterFor(f, srv, "0.3.17")
	srv.Close()
	a.Updater = u
	_, err := a.PlanUpdate(t.Context(), "")
	wantErr(t, err, "could not reach GitHub")
}

func TestRepoComesOnlyFromTheSystemConfig(t *testing.T) {
	f, a := machine(t)
	must(t, os.MkdirAll(filepath.Dir(a.UserConfig), 0o755))
	must(t, os.WriteFile(a.UserConfig, []byte("[update]\nrepo = \"evil/fork\"\n"), 0o644))
	repo, err := a.UpdateRepo()
	must(t, err)
	if repo == "evil/fork" {
		t.Fatal("the user's file must not choose where updates come from")
	}
	must(t, os.WriteFile(f.Dir+"/system.toml", []byte("[update]\nrepo = \"my/fork\"\n"), 0o644))
	if repo, _ := a.UpdateRepo(); repo != "my/fork" {
		t.Fatal(repo)
	}
	must(t, os.WriteFile(f.Dir+"/system.toml", []byte("[update]\nrepo = \"../x\"\n"), 0o644))
	if _, err := a.UpdateRepo(); err == nil {
		t.Fatal("a repository must look like owner/name")
	}
}

func TestAutoUpdateCheckSwitch(t *testing.T) {
	_, a := machine(t)
	if on, err := a.AutoUpdateCheck(); err != nil || on {
		t.Fatal("off until switched on")
	}
	if on, err := a.SetAutoUpdateCheck(true); err != nil || !on {
		t.Fatal(err)
	}
	if on, _ := a.AutoUpdateCheck(); !on {
		t.Fatal("the switch is remembered")
	}
}

func TestNotifyNewUpdateOncePerVersion(t *testing.T) {
	f, a := machine(t)
	srv := release(t, "v0.4.0")
	a.Updater = updaterFor(f, srv, "0.3.17")

	if v, err := a.NotifyNewUpdate(t.Context()); err != nil || v != "" || announced(f) != 0 {
		t.Fatalf("automatic checks are off: %q %v %v", v, err, f.Notified())
	}
	_, err := a.SetAutoUpdateCheck(true)
	must(t, err)
	v, err := a.NotifyNewUpdate(t.Context())
	must(t, err)
	if v != "0.4.0" || announced(f) != 1 || !strings.Contains(strings.Join(f.Notified(), " "), "TrueSense 0.4.0 is available") {
		t.Fatalf("%q %v", v, f.Notified())
	}
	// the cache is refreshed every day, the notice must not come again for the same version
	a.Updater.Now = func() time.Time { return time.Unix(1_700_000_000+3*86400, 0) }
	if v, _ := a.NotifyNewUpdate(t.Context()); v != "" || announced(f) != 1 {
		t.Fatalf("announced twice: %q %v", v, f.Notified())
	}
	// a newer version is announced again
	srv2 := release(t, "v0.5.0")
	a.Updater = updaterFor(f, srv2, "0.3.17")
	a.Updater.Now = func() time.Time { return time.Unix(1_700_000_000+9*86400, 0) }
	if v, _ := a.NotifyNewUpdate(t.Context()); v != "0.5.0" || announced(f) != 2 {
		t.Fatalf("%q %v", v, f.Notified())
	}
}

func TestNothingNewMeansNoNotice(t *testing.T) {
	f, a := machine(t)
	a.Updater = updaterFor(f, release(t, "v0.3.17"), "0.3.17")
	_, _ = a.SetAutoUpdateCheck(true)
	if v, err := a.NotifyNewUpdate(t.Context()); v != "" || err != nil || announced(f) != 0 {
		t.Fatalf("%q %v %v", v, err, f.Notified())
	}
}

// announced counts the update notices shown (one notice may take several helper programs).
func announced(f *apitest.Fixture) int {
	n := 0
	for _, c := range f.Notified() {
		if strings.HasPrefix(c, "notify-send") {
			n++
		}
	}
	return n
}
