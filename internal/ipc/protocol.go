// Package ipc is the local control socket of the daemon: one JSON object per line, one reply
// per request, over a Unix socket.
//
// The daemon (root) owns the privileged and persistent settings; the CLI, the window and the
// key service (running as the user) talk to it here instead of sharing files. The socket is
// world-connectable but every request is authorised by the caller's uid (SO_PEERCRED): root,
// or the configured desktop user.
package ipc

import (
	"encoding/json"
	"fmt"
	"os"
)

// DefaultSocket is where the daemon listens (TRUESENSE_SOCKET overrides it).
const DefaultSocket = "/run/truesense/truesense.sock"

// MaxLine bounds one request or reply.
const MaxLine = 65536

// SocketPath returns the socket to use.
func SocketPath() string {
	if p := os.Getenv("TRUESENSE_SOCKET"); p != "" {
		return p
	}
	return DefaultSocket
}

// Request is one command: {"cmd": "...", "args": {...}}.
type Request struct {
	Cmd  string `json:"cmd"`
	Args Args   `json:"args,omitempty"`
}

// Response is the reply: {"ok": true, "result": ...} or {"ok": false, "error": "..."}.
type Response struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Args are a request's arguments, with typed accessors that report a wrong type instead of
// panicking (the daemon parses untrusted input).
type Args map[string]any

// Has reports whether the key is present.
func (a Args) Has(key string) bool { _, ok := a[key]; return ok }

// String returns the key as a string, or def when absent or null.
func (a Args) String(key, def string) (string, error) {
	v, ok := a[key]
	if !ok || v == nil {
		return def, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be text", key)
	}
	return s, nil
}

// Int returns the key as an integer, or def when absent or null. A JSON number with a
// fractional part is an error.
func (a Args) Int(key string, def int) (int, error) {
	v, ok := a[key]
	if !ok || v == nil {
		return def, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return 0, fmt.Errorf("argument %q must be a whole number", key)
	}
	return int(f), nil
}

// Float returns the key as a number, or def when absent or null.
func (a Args) Float(key string, def float64) (float64, error) {
	v, ok := a[key]
	if !ok || v == nil {
		return def, nil
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("argument %q must be a number", key)
	}
	return f, nil
}

// Bool returns the key as a boolean, or def when absent or null.
func (a Args) Bool(key string, def bool) (bool, error) {
	v, ok := a[key]
	if !ok || v == nil {
		return def, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("argument %q must be true or false", key)
	}
	return b, nil
}

// Decode converts the key's value into out (a struct or slice), via JSON.
func (a Args) Decode(key string, out any) error {
	v, ok := a[key]
	if !ok {
		return fmt.Errorf("argument %q is required", key)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("argument %q: %w", key, err)
	}
	return nil
}
