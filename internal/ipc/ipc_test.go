package ipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// start runs a server on a short temp path (Unix socket paths are limited to ~108 bytes).
func start(t *testing.T, allowed map[uint32]bool, handlers map[string]Handler) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "tsipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s := &Server{Path: filepath.Join(dir, "s.sock"), Allowed: allowed, Handlers: handlers}
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; s.Close() })
	return &Client{Path: s.Path, Timeout: 2 * time.Second}
}

func echo(_ context.Context, a Args) (any, error) { return map[string]any{"got": a}, nil }

func TestRoundTrip(t *testing.T) {
	me := uint32(os.Getuid())
	c := start(t, map[uint32]bool{me: true}, map[string]Handler{
		"echo": echo,
		"fail": func(context.Context, Args) (any, error) { return nil, errors.New("firmware said no") },
		"none": func(context.Context, Args) (any, error) { return nil, nil },
		"boom": func(context.Context, Args) (any, error) { panic("kaboom") },
	})
	var out struct{ Got map[string]any }
	if err := c.CallInto("echo", Args{"n": 3, "s": "x"}, &out); err != nil || out.Got["s"] != "x" {
		t.Fatalf("%+v, %v", out, err)
	}
	var re *RemoteError
	if err := c.CallInto("fail", nil, nil); !errors.As(err, &re) || re.Msg != "firmware said no" {
		t.Fatalf("err = %v", err)
	}
	if err := c.CallInto("none", nil, nil); err != nil {
		t.Fatalf("a handler with no result is fine: %v", err)
	}
	if err := c.CallInto("boom", nil, nil); !errors.As(err, &re) || !strings.Contains(re.Msg, "internal error") {
		t.Fatalf("a panicking handler must become an error reply, not a dead daemon: %v", err)
	}
	if err := c.CallInto("echo", nil, nil); err != nil {
		t.Fatalf("the server must survive the panic: %v", err)
	}
	err := c.CallInto("nosuch", nil, nil)
	if !errors.As(err, &re) || !strings.Contains(re.Msg, "older than this app") {
		t.Fatalf("err = %v", err)
	}
}

func TestOnlyAllowedUIDsMayControl(t *testing.T) {
	c := start(t, map[uint32]bool{999999: true}, map[string]Handler{"echo": echo})
	if os.Getuid() == 0 {
		t.Skip("root is always allowed")
	}
	var re *RemoteError
	if err := c.CallInto("echo", nil, nil); !errors.As(err, &re) || !strings.Contains(re.Msg, "is not allowed to control TrueSense") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientErrors(t *testing.T) {
	c := &Client{Path: "/nonexistent/dir/x.sock"}
	if err := c.CallInto("echo", nil, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v", err)
	}
	if c.Available() {
		t.Fatal("no socket, not available")
	}
}

func TestArgsAreCheckedNotTrusted(t *testing.T) {
	a := Args{"s": "x", "n": 5.0, "frac": 1.5, "b": true, "list": []any{1.0, 2.0}}
	if v, err := a.String("s", ""); err != nil || v != "x" {
		t.Fatal(v, err)
	}
	if _, err := a.String("n", ""); err == nil {
		t.Fatal("a number is not text")
	}
	if v, _ := a.String("missing", "dflt"); v != "dflt" {
		t.Fatal("default")
	}
	if v, err := a.Int("n", 0); err != nil || v != 5 {
		t.Fatal(v, err)
	}
	if _, err := a.Int("frac", 0); err == nil {
		t.Fatal("1.5 is not a whole number")
	}
	if _, err := a.Bool("s", false); err == nil {
		t.Fatal("text is not a boolean")
	}
	var l []int
	if err := a.Decode("list", &l); err != nil || len(l) != 2 {
		t.Fatal(l, err)
	}
	if err := a.Decode("missing", &l); err == nil {
		t.Fatal("a missing key cannot be decoded")
	}
}

func TestHandleLineRejectsGarbageAndOversize(t *testing.T) {
	s := &Server{Handlers: map[string]Handler{"echo": echo}}
	for _, in := range []string{"", "not json", `{"cmd": 5}`, `{"cmd": "echo", "args": [1]}`, `[]`, `{"cmd":"x"}`} {
		out := s.HandleLine(context.Background(), 0, []byte(in))
		if out[len(out)-1] != '\n' || !strings.Contains(string(out), `"ok":false`) {
			t.Errorf("%q -> %s", in, out)
		}
	}
	if _, err := readLine(strings.NewReader(strings.Repeat("a", MaxLine+10))); err == nil {
		t.Fatal("an oversized request must be refused")
	}
}

func FuzzHandleLine(f *testing.F) {
	for _, s := range []string{`{"cmd":"echo","args":{"a":1}}`, `{"cmd":"echo"}`, `{`, ``, `{"cmd":"boom"}`, "\x00\xff", `{"cmd":"echo","args":{"a":{"b":[1,2,{"c":null}]}}}`} {
		f.Add([]byte(s))
	}
	s := &Server{Handlers: map[string]Handler{"echo": echo, "boom": func(context.Context, Args) (any, error) { panic("x") }}}
	f.Fuzz(func(t *testing.T, in []byte) {
		out := s.HandleLine(context.Background(), 0, in)
		if len(out) == 0 || out[len(out)-1] != '\n' {
			t.Fatalf("a reply must be one line: %q", out)
		}
	})
}
