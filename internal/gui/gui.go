// Package gui is the TrueSense window: a small web server that listens on the loopback
// interface only, serves the embedded front-end (internal/gui/web, plain ES modules) and
// forwards every action of the page to a Commands implementation as the calling user.
//
// Qt has no maintained Go binding, and a cgo-free web UI keeps the binary static and works
// in any browser, so `truesense gui` starts this server and opens it as an app window.
// Because the server can change firmware settings, it is built to be hard to abuse:
// a per-run 256-bit session token, handed to the page in exchange for a one-time code that
// travels in the URL fragment (so no URL on a command line or in a log ever holds the token),
// a Host check against DNS rebinding, a same-origin check against cross-site requests, a
// strict Content-Security-Policy, bounded request bodies and timeouts, one instance per user.
//
// The wire contract between the page and the application layer is internal/gui/COMMANDS.md.
package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Commands is everything the window needs from the application. The page never talks to
// the hardware or the daemon itself: each of its actions is one Do call (area and verb are
// the names of the command line, see COMMANDS.md), and the live values come from Stream.
// Implementations run as the user who started the window, so the daemon's own uid check
// still decides what is allowed.
type Commands interface {
	// Do executes one command. args is the JSON object the page sent (never empty:
	// "{}" when it sent nothing). The result must be JSON-encodable. An error that wraps
	// ErrUnknownCommand answers 404, any other error answers 422 with its message.
	Do(ctx context.Context, area, verb string, args json.RawMessage) (any, error)

	// Stream calls emit with the live state about once a second until ctx is done. It
	// is only running while a page is connected, so it may do real hardware reads.
	Stream(ctx context.Context, emit func(json.RawMessage))
}

// ErrUnknownCommand is wrapped by Commands implementations for an (area, verb) they do not
// have, so the server can tell a mistyped command (404) from a failing one (422).
var ErrUnknownCommand = errors.New("unknown command")

// ErrRunning is returned by Run when this user already has a window server (use Resume).
var ErrRunning = errors.New("the TrueSense window is already running")

// CodedError lets a command failure carry a short machine-readable code next to the message
// (for example "needs-daemon"), which the page can use to explain what to do.
type CodedError interface {
	error
	Code() string
}

// Options configures Run. Only Commands is required.
type Options struct {
	// Commands runs the page's actions and provides the live state.
	Commands Commands
	// RuntimeDir holds the single-instance lock file; empty means DefaultRuntimeDir.
	RuntimeDir string
	// Addr is the listen address; it must be on the loopback interface. Empty means
	// "127.0.0.1:0" (a random port).
	Addr string
	// Page is the page the window opens on (see WithPage); empty means the home page.
	Page string
	// Open shows the window. Nil means Open (a browser app window, or the URL on the terminal).
	Open func(url string) error
	// IdleTimeout is how long the server stays up after the last page was closed; zero
	// means one minute. A page that never connects counts as closed from the start.
	IdleTimeout time.Duration
	// CommandTimeout bounds one command (a self-test or an update takes minutes); zero
	// means ten minutes.
	CommandTimeout time.Duration
	// Version is shown in /api/gui/ping and stored in the lock file.
	Version string
	// Log receives start, stop and per-request problems (never the token); nil discards.
	Log *slog.Logger
}

func (o *Options) withDefaults() error {
	if o.Commands == nil {
		return errors.New("gui: Options.Commands is required")
	}
	if o.RuntimeDir == "" {
		dir, err := DefaultRuntimeDir()
		if err != nil {
			return err
		}
		o.RuntimeDir = dir
	}
	if o.Addr == "" {
		o.Addr = "127.0.0.1:0"
	}
	if o.Open == nil {
		o.Open = Open
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = time.Minute
	}
	if o.CommandTimeout <= 0 {
		o.CommandTimeout = 10 * time.Minute
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return nil
}

// WithPage adds the page to a window URL that Run or Resume produced (the page names are
// those of `truesense gui --page`: home, lighting, keys, ...). An unknown name opens home.
func WithPage(rawURL, page string) string {
	u, err := url.Parse(rawURL)
	if err != nil || page == "" {
		return rawURL
	}
	frag, _ := url.ParseQuery(u.Fragment)
	frag.Set("page", page)
	u.Fragment = frag.Encode()
	return u.String()
}

// Resume reports the URL of the window server this user already runs, so a second
// `truesense gui` can bring the existing window forward (print or open that URL) instead of
// starting another server, as the Qt window did.
func Resume() (string, bool) {
	dir, err := DefaultRuntimeDir()
	if err != nil {
		return "", false
	}
	return ResumeIn(dir)
}

// ResumeIn is Resume with an explicit runtime directory (tests, or a caller that already knows it).
func ResumeIn(runtimeDir string) (string, bool) {
	info, ok := readLive(runtimeDir)
	if !ok {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(info.Port)))
	if err != nil {
		return "", false
	}
	_ = conn.Close()
	// ask the running server for a fresh one-time code: the session token itself never leaves the lock file
	base := "http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(info.Port))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gui/launch", strings.NewReader("{}"))
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close() //nolint:errcheck // read-only
	var body struct {
		OK     bool `json:"ok"`
		Result struct {
			Code string `json:"code"`
		} `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body) != nil || !body.OK || body.Result.Code == "" {
		return "", false
	}
	return windowURL(info.Port, body.Result.Code, ""), true
}

// windowURL is the address the browser opens: the code is in the fragment, which browsers
// never send to the server.
func windowURL(port int, code, page string) string {
	frag := url.Values{"code": {code}}
	if page != "" {
		frag.Set("page", page)
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), Path: "/", Fragment: frag.Encode()}
	return u.String()
}
