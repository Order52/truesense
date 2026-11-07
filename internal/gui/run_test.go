package gui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type running struct {
	url    string
	dir    string
	done   chan error
	cancel context.CancelFunc
}

// start runs a window server in a temporary runtime directory and waits until it announced its URL.
func start(t *testing.T, o Options) *running {
	t.Helper()
	if o.Commands == nil {
		o.Commands = stub{}
	}
	if o.RuntimeDir == "" {
		o.RuntimeDir = t.TempDir()
	}
	o.Log = slog.New(slog.DiscardHandler)
	urls := make(chan string, 4)
	if o.Open == nil {
		o.Open = func(u string) error { urls <- u; return nil }
	} else {
		inner := o.Open
		o.Open = func(u string) error { urls <- u; return inner(u) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{dir: o.RuntimeDir, done: make(chan error, 1), cancel: cancel}
	go func() { r.done <- Run(ctx, o) }()
	select {
	case r.url = <-urls:
	case err := <-r.done:
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the window server did not start")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop")
		}
	})
	return r
}

// fragment returns the base address and the one-time code of the URL the window was opened with.
func (r *running) fragment(t *testing.T) (base, code string) {
	t.Helper()
	u, err := url.Parse(r.url)
	if err != nil {
		t.Fatal(err)
	}
	frag, _ := url.ParseQuery(u.Fragment)
	return u.Scheme + "://" + u.Host, frag.Get("code")
}

// exchange does what the page does: trades its one-time code for the session token.
func exchange(t *testing.T, base, code string) (int, string) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), "POST", base+"/api/gui/session", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	var out struct {
		Result struct {
			Token string `json:"token"`
		} `json:"result"`
	}
	b, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out.Result.Token
}

// parts returns the base address and the session token (the code of the URL, exchanged).
func (r *running) parts(t *testing.T) (base, token string) {
	t.Helper()
	base, code := r.fragment(t)
	status, token := exchange(t, base, code)
	if status != 200 || token == "" {
		t.Fatalf("exchanging the launch code: %d", status)
	}
	return base, token
}

func (r *running) post(t *testing.T, path, token string) (int, string) {
	t.Helper()
	base, _ := r.fragment(t)
	req, _ := http.NewRequestWithContext(t.Context(), "POST", base+path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestTheURLCarriesAOneTimeCodeNotTheToken(t *testing.T) {
	r := start(t, Options{Page: "keys"})
	base, code := r.fragment(t)
	if len(code) != 43 { // 256 bits, unpadded base64url
		t.Fatalf("code %q has %d characters", code, len(code))
	}
	u, _ := url.Parse(r.url)
	if !strings.HasPrefix(base, "http://127.0.0.1:") || u.RawQuery != "" || u.Path != "/" {
		t.Fatalf("URL %q: the code must only be in the fragment", r.url)
	}
	if frag, _ := url.ParseQuery(u.Fragment); frag.Get("page") != "keys" || frag.Has("token") {
		t.Fatalf("fragment %q", u.Fragment)
	}
	if status, _ := r.post(t, "/api/gui/ping", code); status != 401 {
		t.Fatalf("the launch code works as a token: %d", status)
	}
	status, token := exchange(t, base, code)
	if status != 200 || len(token) != 43 || token == code {
		t.Fatalf("exchange: %d %q", status, token)
	}
	if status, _ := exchange(t, base, code); status != 401 {
		t.Fatalf("a launch code worked twice: %d", status)
	}
	if status, _ := exchange(t, base, "not-a-code"); status != 401 {
		t.Fatalf("a made-up code: %d", status)
	}
	if status, _ := r.post(t, "/api/gui/ping", ""); status != 401 {
		t.Fatalf("without the token: %d", status)
	}
	if status, _ := r.post(t, "/api/gui/ping", token); status != 200 {
		t.Fatalf("with the token: %d", status)
	}
	if status, _ := r.post(t, "/api/gui/ping", strings.ToUpper(token)); status != 401 {
		t.Fatalf("a changed token: %d", status)
	}
}

func TestEachRunHasItsOwnToken(t *testing.T) {
	a := start(t, Options{})
	b := start(t, Options{})
	_, ta := a.parts(t)
	_, tb := b.parts(t)
	if ta == tb {
		t.Fatal("two runs share a token")
	}
	if status, _ := b.post(t, "/api/gui/ping", ta); status != 401 {
		t.Fatalf("the token of another run was accepted: %d", status)
	}
}

func TestSingleInstancePerUser(t *testing.T) {
	r := start(t, Options{Version: "9.9"})
	base, token := r.parts(t) // the code of the first URL is used up here

	got, ok := ResumeIn(r.dir)
	if !ok {
		t.Fatal("Resume does not see the running server")
	}
	u, _ := url.Parse(got)
	frag, _ := url.ParseQuery(u.Fragment)
	if u.Scheme+"://"+u.Host != base || frag.Get("code") == "" || frag.Has("token") {
		t.Fatalf("Resume gave %q for %s", got, base)
	}
	if strings.Contains(got, token) {
		t.Fatal("Resume put the session token in a URL")
	}
	if _, again := ResumeIn(r.dir); !again {
		t.Fatal("Resume worked once only")
	}
	// the code from Resume is a fresh, working one-time code
	status, token2 := exchange(t, base, frag.Get("code"))
	if status != 200 || token2 != token {
		t.Fatalf("exchanging the code of Resume: %d", status)
	}
	if status, _ := exchange(t, base, frag.Get("code")); status != 401 {
		t.Fatalf("Resume's code worked twice: %d", status)
	}

	if w := WithPage(got, "audio"); w == got || !strings.Contains(w, "page=audio") || !strings.Contains(w, "code="+frag.Get("code")) {
		t.Fatalf("WithPage: %q", w)
	}

	err := Run(context.Background(), Options{Commands: stub{}, RuntimeDir: r.dir, Open: func(string) error { return nil }})
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("a second Run returned %v, want ErrRunning", err)
	}

	info, err := os.Stat(filepath.Join(r.dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock file mode %v", info.Mode().Perm())
	}
	var li lockInfo
	b, _ := os.ReadFile(filepath.Join(r.dir, lockName))
	if err := json.Unmarshal(b, &li); err != nil || li.Token != token || li.Version != "9.9" || li.PID != os.Getpid() {
		t.Fatalf("lock file %s (%v)", b, err)
	}
}

func TestResumeIgnoresAStaleLockFile(t *testing.T) {
	dir := t.TempDir()
	stale := lockInfo{Port: 1, Token: "old", PID: 1}
	b, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(dir, lockName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ResumeIn(dir); ok {
		t.Fatal("a leftover file without a holder counts as a running server")
	}
	if _, ok := ResumeIn(t.TempDir()); ok {
		t.Fatal("an empty directory counts as a running server")
	}
	// and a new server can start over the leftover file
	r := start(t, Options{RuntimeDir: dir})
	if _, ok := ResumeIn(dir); !ok {
		t.Fatal("the new server is not found")
	}
	_ = r
}

func TestLockIsReleasedWhenTheServerStops(t *testing.T) {
	dir := t.TempDir()
	r := start(t, Options{RuntimeDir: dir})
	r.cancel()
	if err := <-r.done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	r.done <- nil // for the cleanup
	if _, ok := ResumeIn(dir); ok {
		t.Fatal("Resume still finds a stopped server")
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); !os.IsNotExist(err) {
		t.Fatalf("the lock file is still there: %v", err)
	}
	again := start(t, Options{RuntimeDir: dir})
	_ = again
}

func TestIdleShutdown(t *testing.T) {
	r := start(t, Options{IdleTimeout: 150 * time.Millisecond})
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatalf("idle stop returned %v", err)
		}
		r.done <- nil
	case <-time.After(3 * time.Second):
		t.Fatal("the server never stopped although no page connected")
	}
}

func TestAPageKeepsTheServerAlive(t *testing.T) {
	r := start(t, Options{IdleTimeout: 200 * time.Millisecond})
	base, token := r.parts(t)
	req, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/api/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.done:
		t.Fatal("the server stopped while a page was connected")
	case <-time.After(700 * time.Millisecond):
	}
	_ = res.Body.Close() // the page is closed
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
		r.done <- nil
	case <-time.After(3 * time.Second):
		t.Fatal("the server did not stop after the last page was closed")
	}
}

func TestQuitCommandStopsTheServer(t *testing.T) {
	r := start(t, Options{})
	_, token := r.parts(t)
	if status, _ := r.post(t, "/api/gui/quit", token); status != 200 {
		t.Fatalf("quit: %d", status)
	}
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
		r.done <- nil
	case <-time.After(3 * time.Second):
		t.Fatal("quit did not stop the server")
	}
}

func TestOnlyLoopbackAddresses(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.5:0", "[::]:0", "example.com:80", "nonsense"} {
		err := Run(context.Background(), Options{Commands: stub{}, RuntimeDir: t.TempDir(), Addr: addr, Open: func(string) error { return nil }})
		if err == nil || !strings.Contains(err.Error(), "gui:") {
			t.Errorf("Run(%q) = %v, want a refusal", addr, err)
		}
	}
	// the loopback forms are fine
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0"} {
		ln, err := listenLoopback(context.Background(), addr)
		if err != nil {
			t.Logf("%s: %v (no IPv6 here?)", addr, err)
			continue
		}
		_ = ln.Close()
	}
}

func TestRunNeedsCommands(t *testing.T) {
	if err := Run(context.Background(), Options{RuntimeDir: t.TempDir()}); err == nil {
		t.Fatal("Run without Commands succeeded")
	}
}

func TestOpenFailureDoesNotStopTheServer(t *testing.T) {
	var calls int
	var mu sync.Mutex
	r := start(t, Options{Open: func(string) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return errors.New("no browser")
	}})
	_, token := r.parts(t)
	if status, _ := r.post(t, "/api/gui/ping", token); status != 200 {
		t.Fatalf("status %d", status)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("Open called %d times", calls)
	}
}

func TestDefaultRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1234")
	if d, err := DefaultRuntimeDir(); err != nil || d != "/run/user/1234" {
		t.Fatalf("got %q, %v", d, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", t.TempDir())
	d, err := DefaultRuntimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(d); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("fallback dir %s: %v %v", d, st, err)
	}
	// a directory of the right name that others can read is not trusted
	if err := os.Chmod(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultRuntimeDir(); err == nil {
		t.Fatal("a world-readable runtime directory was accepted")
	}
}
