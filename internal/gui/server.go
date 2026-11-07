package gui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxBody bounds a request body: the biggest legitimate one is 128 key colours (a few KB).
const maxBody = 1 << 20

// maxStreams bounds the open live streams: a page needs one, two windows two.
const maxStreams = 8

// contentSecurityPolicy lets the page load only its own scripts and styles and talk only
// to this server: no inline code, no external request of any kind.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// reply is the JSON envelope of every API answer.
type reply struct {
	OK     bool       `json:"ok"`
	Result any        `json:"result"`
	Error  *replyFail `json:"error,omitempty"`
}

type replyFail struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// server is the http.Handler: security checks, the command bus, the live stream and the
// embedded assets.
type server struct {
	addr    string            // the listening address ("127.0.0.1:port"): the only acceptable Host
	session [sha256.Size]byte // hash of the session token every API request must carry
	// sessionToken is handed out by exchange; it never appears in a URL.
	sessionToken string
	cmds         Commands
	assets       *assets
	hub          *hub
	log          *slog.Logger
	version      string
	timeout      time.Duration
	quit         func()

	streams atomic.Int32

	mu       sync.Mutex
	lastSeen time.Time            // when the number of open streams last became zero
	codes    map[string]time.Time // one-time launch codes (hashed) -> expiry
}

var _ http.Handler = (*server)(nil)

func newServer(addr, session string, o Options, quit func()) *server {
	s := &server{
		addr: addr, session: sha256.Sum256([]byte(session)), sessionToken: session, cmds: o.Commands, assets: loadAssets(),
		log: o.Log, version: o.Version, timeout: o.CommandTimeout, quit: quit, lastSeen: time.Now(),
		codes: map[string]time.Time{},
	}
	s.hub = newHub(o.Commands, o.Log)
	return s
}

// idleSince reports how long no page has had a live stream open (zero while one is open).
func (s *server) idleSince() time.Duration {
	if s.streams.Load() > 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastSeen)
}

func (s *server) streamClosed() {
	if s.streams.Add(-1) == 0 {
		s.mu.Lock()
		s.lastSeen = time.Now()
		s.mu.Unlock()
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("handler panic", "path", r.URL.Path, "panic", p)
			fail(w, http.StatusInternalServerError, "internal error", "")
		}
	}()
	h := w.Header()
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	h.Set("X-Frame-Options", "DENY")

	// DNS rebinding: a page on another name that resolves to 127.0.0.1 still sends its own
	// name as Host, so anything but the exact listening address is refused.
	if r.Host != s.addr {
		fail(w, http.StatusForbidden, "unexpected Host header", "bad-host")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		fail(w, http.StatusMethodNotAllowed, "only GET and POST are served", "")
		return
	}
	// Cross-site requests: browsers label every request; the page itself is same-origin
	// ("none" is the user typing the address). Clients that send neither header (curl)
	// are not a CSRF vector and still need the token for anything that acts.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		fail(w, http.StatusForbidden, "cross-site request refused", "cross-site")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.addr {
		fail(w, http.StatusForbidden, "cross-origin request refused", "cross-site")
		return
	}

	if !strings.HasPrefix(r.URL.Path, "/api/") {
		s.assets.serve(w, r)
		return
	}
	h.Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/gui/session" {
		s.exchange(w, r) // the only call that needs no token: it is how the page gets one
		return
	}
	if !s.authorised(r) {
		fail(w, http.StatusUnauthorized, "missing or wrong token", "unauthorised")
		return
	}
	s.api(w, r)
}

// authorised compares the bearer token in constant time (both sides are hashed first so
// the comparison does not depend on the length of what the caller sent).
func (s *server) authorised(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	sum := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(sum[:], s.session[:]) == 1
}

func (s *server) api(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/")
	if rest == "stream" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			fail(w, http.StatusMethodNotAllowed, "the stream is read with GET", "")
			return
		}
		s.stream(w, r)
		return
	}
	area, verb, ok := strings.Cut(rest, "/")
	if !ok || !namePattern.MatchString(area) || !namePattern.MatchString(verb) {
		fail(w, http.StatusNotFound, "no such command", "unknown-command")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, http.StatusMethodNotAllowed, "commands are sent with POST", "")
		return
	}
	// A cross-origin page cannot send application/json without a preflight we never answer.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "send application/json", "")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "request body too large", "")
			return
		}
		fail(w, http.StatusBadRequest, "could not read the request body", "")
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		body = []byte("{}")
	}
	if body[0] != '{' || !json.Valid(body) {
		fail(w, http.StatusBadRequest, "the body must be a JSON object", "")
		return
	}

	// Commands of the server itself: they never reach the application.
	switch area + "/" + verb {
	case "gui/ping":
		reply200(w, map[string]any{"version": s.version})
		return
	case "gui/quit":
		reply200(w, true)
		s.quit()
		return
	case "gui/launch":
		code, err := s.mintCode()
		if err != nil {
			fail(w, http.StatusInternalServerError, "could not make a launch code", "")
			return
		}
		reply200(w, map[string]string{"code": code})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	// A long command (self-test, update) must outlive the default per-request write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.timeout + 10*time.Second))
	res, err := s.cmds.Do(ctx, area, verb, body)
	if err != nil {
		status, code := http.StatusUnprocessableEntity, ""
		var coded CodedError
		if errors.As(err, &coded) {
			code = coded.Code()
		}
		switch {
		case errors.Is(err, ErrUnknownCommand):
			status, code = http.StatusNotFound, "unknown-command"
		case errors.Is(err, context.DeadlineExceeded):
			status, code = http.StatusGatewayTimeout, "timeout"
		}
		s.log.Debug("command failed", "area", area, "verb", verb, "err", err)
		fail(w, status, err.Error(), code)
		return
	}
	reply200(w, res)
}

func reply200(w http.ResponseWriter, result any) {
	out, err := json.Marshal(reply{OK: true, Result: result})
	if err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("the result cannot be encoded: %v", err), "")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

func fail(w http.ResponseWriter, status int, message, code string) {
	out, _ := json.Marshal(reply{Error: &replyFail{Message: message, Code: code}})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

// stream is the server-sent-event feed of the live state. The page uses fetch() and not
// EventSource because EventSource cannot send the Authorization header.
func (s *server) stream(w http.ResponseWriter, r *http.Request) {
	if s.streams.Add(1) > maxStreams {
		s.streamClosed()
		fail(w, http.StatusServiceUnavailable, "too many open windows", "")
		return
	}
	defer s.streamClosed()
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	write := func(format string, args ...any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !write("retry: 2000\n\n") {
		return
	}
	sub, unsubscribe := s.hub.subscribe()
	defer unsubscribe()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case msg := <-sub:
			if !write("event: state\ndata: %s\n\n", msg) {
				return
			}
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// launchCodeLife is how long a window URL stays usable. The URL is on the browser's command
// line, where other users of the computer can read it, so it carries a code that works once
// (and soon expires) and not the session token itself.
const launchCodeLife = 5 * time.Minute

// maxCodes bounds the unused launch codes kept.
const maxCodes = 16

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return string(sum[:])
}

// mintCode makes a one-time code that the page exchanges for the session token.
func (s *server) mintCode() (string, error) {
	code, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, exp := range s.codes {
		if now.After(exp) {
			delete(s.codes, k)
		}
	}
	for len(s.codes) >= maxCodes { // drop the oldest
		var oldest string
		var at time.Time
		for k, exp := range s.codes {
			if oldest == "" || exp.Before(at) {
				oldest, at = k, exp
			}
		}
		delete(s.codes, oldest)
	}
	s.codes[hashCode(code)] = now.Add(launchCodeLife)
	return code, nil
}

// exchange trades a valid one-time launch code for the session token. The code is used up
// whatever happens next, so a copy of the URL taken from the command line is worthless once
// the real window has opened.
func (s *server) exchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, http.StatusMethodNotAllowed, "send the launch code with POST", "")
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "send application/json", "")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the body must be {\"code\": ...}", "")
		return
	}
	key := hashCode(req.Code)
	s.mu.Lock()
	exp, ok := s.codes[key]
	delete(s.codes, key)
	s.mu.Unlock()
	if !ok || time.Now().After(exp) {
		fail(w, http.StatusUnauthorized, "this window link has been used already or has expired", "unauthorised")
		return
	}
	reply200(w, map[string]string{"token": s.sessionToken})
}
