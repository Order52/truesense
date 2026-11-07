package gui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stub is a Commands whose behaviour each test sets.
type stub struct {
	do     func(ctx context.Context, area, verb string, args json.RawMessage) (any, error)
	stream func(ctx context.Context, emit func(json.RawMessage))
}

func (s stub) Do(ctx context.Context, area, verb string, args json.RawMessage) (any, error) {
	if s.do == nil {
		return map[string]any{"area": area, "verb": verb, "args": args}, nil
	}
	return s.do(ctx, area, verb, args)
}

func (s stub) Stream(ctx context.Context, emit func(json.RawMessage)) {
	if s.stream == nil {
		<-ctx.Done()
		return
	}
	s.stream(ctx, emit)
}

const testToken = "test-token-0123456789"

type fixture struct {
	ts     *httptest.Server
	srv    *server
	addr   string
	quitCh chan struct{}
}

func newFixture(t *testing.T, c Commands) *fixture {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	addr := ts.Listener.Addr().String()
	quit := make(chan struct{})
	var once sync.Once
	opts := Options{Commands: c, CommandTimeout: 300 * time.Millisecond, Version: "test", Log: slog.New(slog.DiscardHandler)}
	srv := newServer(addr, testToken, opts, func() { once.Do(func() { close(quit) }) })
	ts.Config.Handler = srv
	ts.Start()
	t.Cleanup(func() { srv.hub.stop(); ts.Close() })
	return &fixture{ts: ts, srv: srv, addr: addr, quitCh: quit}
}

// result is a finished response: status, headers and the whole body.
type result struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func (f *fixture) request(t *testing.T, method, path, body string, tweak func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if tweak != nil {
		tweak(req)
	}
	res, err := f.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// do sends a request with the right token, Host and Content-Type unless the tweak changes them,
// and returns the finished response.
func (f *fixture) do(t *testing.T, method, path, body string, tweak func(*http.Request)) *result {
	t.Helper()
	res := f.request(t, method, path, body, tweak)
	defer res.Body.Close() //nolint:errcheck // test
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return &result{StatusCode: res.StatusCode, Header: res.Header, Body: string(b)}
}

// stream opens the live stream and leaves the body to the caller (it never ends by itself).
func (f *fixture) stream(t *testing.T) *http.Response {
	t.Helper()
	res := f.request(t, "GET", "/api/stream", "", nil)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *replyFail      `json:"error"`
}

func decode(t *testing.T, res *result) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(res.Body), &e); err != nil {
		t.Fatalf("not an envelope: %v", err)
	}
	return e
}

func TestAuthAndGuards(t *testing.T) {
	f := newFixture(t, stub{})
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		tweak  func(*http.Request)
		status int
		code   string
	}{
		{"valid command", "POST", "/api/power/set", `{"mode":"turbo"}`, nil, 200, ""},
		{"no token", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Del("Authorization") }, 401, "unauthorised"},
		{"wrong token", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, 401, "unauthorised"},
		{"token without Bearer", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Authorization", testToken) }, 401, "unauthorised"},
		{"token prefix only", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+testToken[:5]) }, 401, "unauthorised"},
		{"token in the query is no token", "POST", "/api/power/set?token=" + testToken, `{}`, func(r *http.Request) { r.Header.Del("Authorization") }, 401, "unauthorised"},
		{"wrong Host (DNS rebinding)", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Host = "evil.example:80" }, 403, "bad-host"},
		{"localhost is not the address", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Host = "localhost:" + strings.Split(f.addr, ":")[1] }, 403, "bad-host"},
		{"cross-site fetch", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403, "cross-site"},
		{"same-site (another port) fetch", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, 403, "cross-site"},
		{"same-origin fetch", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }, 200, ""},
		{"address typed by the user", "GET", "/api/gui/ping", ``, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "none") }, 405, ""},
		{"foreign Origin", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403, "cross-site"},
		{"Origin null", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Origin", "null") }, 403, "cross-site"},
		{"own Origin", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Origin", "http://"+f.addr) }, 200, ""},
		{"PUT refused", "PUT", "/api/power/set", `{}`, nil, 405, ""},
		{"DELETE refused", "DELETE", "/api/power/set", ``, nil, 405, ""},
		{"HEAD refused", "HEAD", "/", ``, nil, 405, ""},
		{"OPTIONS refused (no CORS)", "OPTIONS", "/api/power/set", ``, nil, 405, ""},
		{"GET a command", "GET", "/api/power/set", ``, nil, 405, ""},
		{"POST without JSON content type", "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415, ""},
		{"form content type", "POST", "/api/power/set", `a=b`, func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 415, ""},
		{"body not an object", "POST", "/api/power/set", `[1]`, nil, 400, ""},
		{"body not JSON", "POST", "/api/power/set", `{nope`, nil, 400, ""},
		{"empty body is {}", "POST", "/api/power/set", ``, nil, 200, ""},
		{"bad area name", "POST", "/api/Power/set", `{}`, nil, 404, "unknown-command"},
		{"path traversal in the verb", "POST", "/api/power/../x", `{}`, nil, 404, "unknown-command"},
		{"missing verb", "POST", "/api/power", `{}`, nil, 404, "unknown-command"},
		{"too many parts", "POST", "/api/a/b/c", `{}`, nil, 404, "unknown-command"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := f.do(t, tc.method, tc.path, tc.body, tc.tweak)
			if res.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (%s)", res.StatusCode, tc.status, res.Body)
			}
			if tc.status >= 400 && tc.method != "HEAD" && tc.method != "OPTIONS" {
				if code := decode(t, res).Error; tc.code != "" && (code == nil || code.Code != tc.code) {
					t.Fatalf("error = %+v, want code %q", code, tc.code)
				}
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	f := newFixture(t, stub{})
	big := `{"x":"` + strings.Repeat("a", maxBody) + `"}`
	if res := f.do(t, "POST", "/api/power/set", big, nil); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: status %d", res.StatusCode)
	}
	ok := `{"x":"` + strings.Repeat("a", maxBody/2) + `"}`
	if res := f.do(t, "POST", "/api/power/set", ok, nil); res.StatusCode != 200 {
		t.Fatalf("half-size body: status %d", res.StatusCode)
	}
}

type codedErr struct{ msg, code string }

func (e codedErr) Error() string { return e.msg }
func (e codedErr) Code() string  { return e.code }

func TestCommandBus(t *testing.T) {
	var got struct {
		area, verb string
		args       string
	}
	f := newFixture(t, stub{do: func(ctx context.Context, area, verb string, args json.RawMessage) (any, error) {
		got.area, got.verb, got.args = area, verb, string(args)
		switch area + "/" + verb {
		case "fan/set":
			return "fans: manual 40 %", nil
		case "fan/off":
			return nil, errors.New("too warm to stop a fan (CPU 64 C)")
		case "fan/unknown":
			return nil, fmt.Errorf("%w: fan unknown", ErrUnknownCommand)
		case "daemon/needs":
			return nil, codedErr{"the TrueSense daemon is not running", "needs-daemon"}
		case "x/slow":
			<-ctx.Done()
			return nil, ctx.Err()
		case "x/nil":
			return nil, nil
		case "x/bad":
			return make(chan int), nil // cannot be encoded
		case "x/panic":
			panic("boom")
		}
		return map[string]any{"ok": 1}, nil
	}})

	t.Run("success carries the result and the args reach Do untouched", func(t *testing.T) {
		res := f.do(t, "POST", "/api/fan/set", ` {"cpu":40, "gpu":50} `, nil)
		e := decode(t, res)
		if res.StatusCode != 200 || !e.OK || string(e.Result) != `"fans: manual 40 %"` || e.Error != nil {
			t.Fatalf("got %d %+v", res.StatusCode, e)
		}
		if got.area != "fan" || got.verb != "set" || got.args != `{"cpu":40, "gpu":50}` {
			t.Fatalf("Do saw %+v", got)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("content type %q", ct)
		}
	})
	t.Run("a null result is ok:true", func(t *testing.T) {
		e := decode(t, f.do(t, "POST", "/api/x/nil", `{}`, nil))
		if !e.OK || string(e.Result) != "null" {
			t.Fatalf("got %+v", e)
		}
	})
	t.Run("a failing command is 422 with its message", func(t *testing.T) {
		res := f.do(t, "POST", "/api/fan/off", `{"confirm":true}`, nil)
		e := decode(t, res)
		if res.StatusCode != 422 || e.OK || e.Error == nil || e.Error.Message != "too warm to stop a fan (CPU 64 C)" {
			t.Fatalf("got %d %+v", res.StatusCode, e)
		}
	})
	t.Run("an unknown command is 404", func(t *testing.T) {
		res := f.do(t, "POST", "/api/fan/unknown", `{}`, nil)
		e := decode(t, res)
		if res.StatusCode != 404 || e.Error == nil || e.Error.Code != "unknown-command" {
			t.Fatalf("got %d %+v", res.StatusCode, e)
		}
	})
	t.Run("an error code is passed on", func(t *testing.T) {
		e := decode(t, f.do(t, "POST", "/api/daemon/needs", `{}`, nil))
		if e.Error == nil || e.Error.Code != "needs-daemon" {
			t.Fatalf("got %+v", e.Error)
		}
	})
	t.Run("a command that runs too long is cut off with 504", func(t *testing.T) {
		res := f.do(t, "POST", "/api/x/slow", `{}`, nil)
		if res.StatusCode != http.StatusGatewayTimeout || decode(t, res).Error.Code != "timeout" {
			t.Fatalf("status %d", res.StatusCode)
		}
	})
	t.Run("a result that cannot be encoded is 500, not a hang", func(t *testing.T) {
		if res := f.do(t, "POST", "/api/x/bad", `{}`, nil); res.StatusCode != 500 {
			t.Fatalf("status %d", res.StatusCode)
		}
	})
	t.Run("a panic in a command is 500 and the server lives on", func(t *testing.T) {
		if res := f.do(t, "POST", "/api/x/panic", `{}`, nil); res.StatusCode != 500 {
			t.Fatalf("status %d", res.StatusCode)
		}
		if res := f.do(t, "POST", "/api/fan/set", `{}`, nil); res.StatusCode != 200 {
			t.Fatalf("server did not survive: %d", res.StatusCode)
		}
	})
	t.Run("gui ping is answered by the server", func(t *testing.T) {
		e := decode(t, f.do(t, "POST", "/api/gui/ping", `{}`, nil))
		if !e.OK || !strings.Contains(string(e.Result), `"version":"test"`) {
			t.Fatalf("got %+v", e)
		}
	})
	t.Run("gui quit asks the server to stop", func(t *testing.T) {
		if e := decode(t, f.do(t, "POST", "/api/gui/quit", `{}`, nil)); !e.OK {
			t.Fatalf("got %+v", e)
		}
		select {
		case <-f.quitCh:
		case <-time.After(time.Second):
			t.Fatal("quit was not requested")
		}
	})
}

func TestHeaders(t *testing.T) {
	f := newFixture(t, stub{})
	api := f.do(t, "POST", "/api/power/set", `{}`, nil)
	page := f.do(t, "GET", "/", ``, nil)
	denied := f.do(t, "POST", "/api/power/set", `{}`, func(r *http.Request) { r.Header.Del("Authorization") })
	for name, res := range map[string]*result{"api": api, "page": page, "401": denied} {
		h := res.Header
		if got := h.Get("Content-Security-Policy"); got != contentSecurityPolicy {
			t.Errorf("%s: CSP = %q", name, got)
		}
		for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY", "Cross-Origin-Resource-Policy": "same-origin"} {
			if h.Get(k) != want {
				t.Errorf("%s: %s = %q, want %q", name, k, h.Get(k), want)
			}
		}
		if h.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: a CORS header is set", name)
		}
	}
	for name, res := range map[string]*result{"api": api, "401": denied} {
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q", name, res.Header.Get("Cache-Control"))
		}
	}
	// the policy is exactly the strict one of the design: nothing external, nothing inline
	for _, must := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self'", "base-uri 'none'", "form-action 'none'", "frame-ancestors 'none'", "img-src 'self' data:"} {
		if !strings.Contains(contentSecurityPolicy, must) {
			t.Errorf("CSP lacks %q", must)
		}
	}
	if strings.Contains(contentSecurityPolicy, "unsafe") || strings.Contains(contentSecurityPolicy, "http") {
		t.Errorf("CSP is not strict: %s", contentSecurityPolicy)
	}
}

func TestStaticAssets(t *testing.T) {
	f := newFixture(t, stub{})
	t.Run("index and modules are served without the token", func(t *testing.T) {
		for path, typ := range map[string]string{"/": "text/html", "/index.html": "text/html", "/app.css": "text/css", "/js/app.js": "text/javascript", "/logo.svg": "image/svg+xml"} {
			res := f.do(t, "GET", path, ``, func(r *http.Request) { r.Header.Del("Authorization") })
			if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), typ) {
				t.Errorf("%s: %d %q", path, res.StatusCode, res.Header.Get("Content-Type"))
			}
			if res.Header.Get("ETag") == "" || res.Header.Get("Cache-Control") != "no-cache" {
				t.Errorf("%s: caching headers %v", path, res.Header)
			}
		}
	})
	t.Run("a repeat request is answered 304", func(t *testing.T) {
		first := f.do(t, "GET", "/app.css", ``, nil)
		etag := first.Header.Get("ETag")
		res := f.do(t, "GET", "/app.css", ``, func(r *http.Request) { r.Header.Set("If-None-Match", etag) })
		if res.StatusCode != http.StatusNotModified {
			t.Fatalf("status %d", res.StatusCode)
		}
	})
	t.Run("nothing outside the front-end is served", func(t *testing.T) {
		for _, path := range []string{"/nope", "/js/", "/js/../../gui.go", "/../go.mod", "/web/index.html", "/%2e%2e/gui.go", "/js/pages", "/package.json", "/index.html/x"} {
			res := f.do(t, "GET", path, ``, nil)
			if res.StatusCode != 404 {
				t.Errorf("%s: status %d", path, res.StatusCode)
			}
		}
	})
}

func readEvents(t *testing.T, res *http.Response, n int, within time.Duration) []string {
	t.Helper()
	got := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				got <- data
			}
		}
		close(got)
	}()
	var out []string
	deadline := time.After(within)
	for len(out) < n {
		select {
		case d, ok := <-got:
			if !ok {
				return out
			}
			out = append(out, d)
		case <-deadline:
			t.Fatalf("got %d of %d events: %v", len(out), n, out)
		}
	}
	return out
}

func TestStream(t *testing.T) {
	var running atomic.Int32
	var stopped atomic.Int32
	f := newFixture(t, stub{stream: func(ctx context.Context, emit func(json.RawMessage)) {
		running.Add(1)
		defer stopped.Add(1)
		for i := 1; ; i++ {
			// indented on purpose: the server must put one event on one line
			emit(json.RawMessage(fmt.Sprintf("{\n \"n\": %d\n}", i)))
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}})

	t.Run("needs the token and a GET", func(t *testing.T) {
		if res := f.do(t, "GET", "/api/stream", ``, func(r *http.Request) { r.Header.Del("Authorization") }); res.StatusCode != 401 {
			t.Fatalf("status %d", res.StatusCode)
		}
		if res := f.do(t, "POST", "/api/stream", `{}`, nil); res.StatusCode != 405 {
			t.Fatalf("status %d", res.StatusCode)
		}
		if res := f.do(t, "GET", "/api/stream", ``, func(r *http.Request) { r.Host = "evil.example" }); res.StatusCode != 403 {
			t.Fatalf("status %d", res.StatusCode)
		}
	})

	t.Run("delivers compact events and stops with the last page", func(t *testing.T) {
		res := f.stream(t) //nolint:bodyclose // closed by the cleanup in stream
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("status %d headers %v", res.StatusCode, res.Header)
		}
		ev := readEvents(t, res, 3, 3*time.Second)
		if ev[0] != `{"n":1}` && !strings.HasPrefix(ev[0], `{"n":`) {
			t.Fatalf("events %v", ev)
		}
		for _, e := range ev {
			if strings.Contains(e, "\n") {
				t.Fatalf("event spans lines: %q", e)
			}
		}
		if f.srv.streams.Load() != 1 {
			t.Fatalf("streams = %d", f.srv.streams.Load())
		}
		_ = res.Body.Close()
		deadline := time.Now().Add(2 * time.Second)
		for stopped.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if stopped.Load() != 1 || running.Load() != 1 {
			t.Fatalf("stream goroutine: started %d, stopped %d", running.Load(), stopped.Load())
		}
		for f.srv.streams.Load() != 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if f.srv.idleSince() <= 0 {
			t.Fatal("the server does not count the page as closed")
		}
	})

	t.Run("a new page gets the latest state at once", func(t *testing.T) {
		res := f.stream(t) //nolint:bodyclose // closed by the cleanup in stream
		start := time.Now()
		readEvents(t, res, 1, time.Second)
		if time.Since(start) > 500*time.Millisecond {
			t.Fatal("first event was slow")
		}
	})
}

func TestStreamSharedAndLimited(t *testing.T) {
	var started atomic.Int32
	f := newFixture(t, stub{stream: func(ctx context.Context, emit func(json.RawMessage)) {
		started.Add(1)
		for ctx.Err() == nil {
			emit(json.RawMessage(`{"ok":true}`))
			time.Sleep(10 * time.Millisecond)
		}
	}})
	var bodies []io.Closer
	for i := 0; i < maxStreams; i++ {
		res := f.stream(t) //nolint:bodyclose // closed by the cleanup in stream
		if res.StatusCode != 200 {
			t.Fatalf("stream %d: status %d", i, res.StatusCode)
		}
		bodies = append(bodies, res.Body)
		readEvents(t, res, 1, 2*time.Second)
	}
	if res := f.stream(t); res.StatusCode != http.StatusServiceUnavailable { //nolint:bodyclose // closed by the cleanup in stream
		t.Fatalf("stream beyond the limit: status %d", res.StatusCode)
	}
	if n := started.Load(); n != 1 {
		t.Fatalf("Commands.Stream started %d times for %d pages, want one shared run", n, maxStreams)
	}
	for _, b := range bodies {
		_ = b.Close()
	}
}

func TestStreamRestartsAfterEarlyReturn(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, stub{stream: func(ctx context.Context, emit func(json.RawMessage)) {
		n := calls.Add(1)
		emit(json.RawMessage(fmt.Sprintf(`{"call":%d}`, n)))
		// returns at once: the hub must call it again
	}})
	f.srv.hub.restartDelay = 5 * time.Millisecond
	res := f.stream(t) //nolint:bodyclose // closed by the cleanup in stream
	ev := readEvents(t, res, 2, 5*time.Second)
	if ev[0] != `{"call":1}` || ev[1] != `{"call":2}` {
		t.Fatalf("events %v", ev)
	}
}

func TestInvalidStreamStateIsDropped(t *testing.T) {
	f := newFixture(t, stub{stream: func(ctx context.Context, emit func(json.RawMessage)) {
		emit(json.RawMessage(`{broken`))
		emit(json.RawMessage(`{"good":1}`))
		<-ctx.Done()
	}})
	res := f.stream(t) //nolint:bodyclose // closed by the cleanup in stream
	if ev := readEvents(t, res, 1, 2*time.Second); ev[0] != `{"good":1}` {
		t.Fatalf("events %v", ev)
	}
}

func TestHubKeepsOnlyTheNewestForASlowListener(t *testing.T) {
	h := newHub(stub{}, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	ch, unsub := h.subscribe()
	defer unsub()
	for i := 1; i <= 5; i++ {
		h.publish(ctx, json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)))
	}
	if got := <-ch; !bytes.Equal(got, []byte(`{"n":5}`)) {
		t.Fatalf("got %s", got)
	}
	select {
	case extra := <-ch:
		t.Fatalf("an old state was kept: %s", extra)
	default:
	}
}

func TestLaunchCodes(t *testing.T) {
	f := newFixture(t, stub{})
	mint := func() string {
		code, err := f.srv.mintCode()
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	post := func(body string, tweak func(*http.Request)) *result {
		return f.do(t, "POST", "/api/gui/session", body, func(r *http.Request) {
			r.Header.Del("Authorization") // the exchange needs no token
			if tweak != nil {
				tweak(r)
			}
		})
	}

	t.Run("a code is exchanged once for the session token", func(t *testing.T) {
		code := mint()
		e := decode(t, post(`{"code":"`+code+`"}`, nil))
		if !e.OK || !strings.Contains(string(e.Result), testToken) {
			t.Fatalf("got %+v", e)
		}
		if res := post(`{"code":"`+code+`"}`, nil); res.StatusCode != 401 {
			t.Fatalf("second use: %d", res.StatusCode)
		}
	})
	t.Run("an expired code is refused", func(t *testing.T) {
		code := mint()
		f.srv.mu.Lock()
		f.srv.codes[hashCode(code)] = time.Now().Add(-time.Second)
		f.srv.mu.Unlock()
		if res := post(`{"code":"`+code+`"}`, nil); res.StatusCode != 401 {
			t.Fatalf("status %d", res.StatusCode)
		}
	})
	t.Run("the exchange keeps every other guard", func(t *testing.T) {
		code := mint()
		body := `{"code":"` + code + `"}`
		for name, tweak := range map[string]func(*http.Request){
			"wrong Host":     func(r *http.Request) { r.Host = "evil.example" },
			"cross-site":     func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
			"foreign Origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
			"not JSON":       func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		} {
			if res := post(body, tweak); res.StatusCode < 400 {
				t.Errorf("%s: status %d", name, res.StatusCode)
			}
		}
		if res := f.do(t, "GET", "/api/gui/session", "", nil); res.StatusCode != 405 {
			t.Errorf("GET: status %d", res.StatusCode)
		}
		// none of the refused attempts used the code up
		if res := post(body, nil); res.StatusCode != 200 {
			t.Errorf("the code was burnt by refused requests: %d", res.StatusCode)
		}
	})
	t.Run("garbage bodies are 400", func(t *testing.T) {
		for _, body := range []string{``, `{`, `[]`, `"x"`, strings.Repeat("a", 10000)} {
			if res := post(body, nil); res.StatusCode != 400 && res.StatusCode != 401 {
				t.Errorf("%.20q: status %d", body, res.StatusCode)
			}
		}
	})
	t.Run("the number of unused codes is bounded and the oldest go first", func(t *testing.T) {
		first := mint()
		for i := 0; i < maxCodes+2; i++ {
			mint()
		}
		f.srv.mu.Lock()
		n := len(f.srv.codes)
		f.srv.mu.Unlock()
		if n > maxCodes {
			t.Fatalf("%d codes kept", n)
		}
		if res := post(`{"code":"`+first+`"}`, nil); res.StatusCode != 401 {
			t.Fatalf("the oldest code survived: %d", res.StatusCode)
		}
	})
	t.Run("minting needs the token", func(t *testing.T) {
		if res := f.do(t, "POST", "/api/gui/launch", `{}`, func(r *http.Request) { r.Header.Del("Authorization") }); res.StatusCode != 401 {
			t.Fatalf("status %d", res.StatusCode)
		}
		e := decode(t, f.do(t, "POST", "/api/gui/launch", `{}`, nil))
		if !e.OK || !strings.Contains(string(e.Result), `"code"`) {
			t.Fatalf("got %+v", e)
		}
	})
}
