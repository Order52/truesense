package apitest

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/Order52/truesense/internal/ipc"
)

// Call is one request the fake service received.
type Call struct {
	Cmd  string
	Args ipc.Args
}

// Daemon is a stand-in for the control socket: it answers each command with a canned reply (set
// with Reply) and records every request, so a test can say what the client asked for. The real
// handlers are tested in internal/daemon; this checks the client side of the same protocol.
type Daemon struct {
	mu      sync.Mutex
	replies map[string]func(args ipc.Args) (any, error)
	calls   []Call
}

// StartDaemon starts a fake service on a socket in the fixture's directory and points the
// fixture's clients at it. The default reply to "state" describes a service with the smart fan
// off and the automatic profile on.
func (f *Fixture) StartDaemon() *Daemon {
	f.T.Helper()
	d := &Daemon{replies: map[string]func(ipc.Args) (any, error){}}
	d.Reply("state", map[string]any{
		"live":        map[string]any{"power_mode": "balanced", "ac": true, "auto_mode": "native"},
		"settings":    map[string]any{"smart_fan": false, "auto_profile": true, "auto_mode": "native", "operating_follow": false, "smart_profile": "truesense", "fan_curve": "native"},
		"hold_left_s": 0,
	})
	d.Reply("ping", "pong")
	handlers := map[string]ipc.Handler{}
	for _, cmd := range []string{"state", "set", "fan", "power", "fan_curve", "smart_profile", "undervolt", "gpu_undervolt", "battery_full", "boot_look", "hold", "game_rules", "power_managers", "cpu_limits", "cpu_info", "touchpad", "ping"} {
		handlers[cmd] = d.handler(cmd)
	}
	sock := filepath.Join(f.Dir, "run", "truesense.sock")
	_ = os.MkdirAll(filepath.Dir(sock), 0o755)
	uid := uint32(os.Getuid()) //nolint:gosec // a user id is never negative or beyond 32 bits
	srv := &ipc.Server{Path: sock, Allowed: map[uint32]bool{uid: true}, Handlers: handlers}
	if err := srv.Listen(); err != nil {
		f.T.Fatalf("fake service: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(done) }()
	f.T.Cleanup(func() { cancel(); <-done; srv.Close() })
	f.S.IPC.Path = sock
	f.daemon = d
	return d
}

func (d *Daemon) handler(cmd string) ipc.Handler {
	return func(_ context.Context, args ipc.Args) (any, error) {
		d.mu.Lock()
		d.calls = append(d.calls, Call{Cmd: cmd, Args: args})
		fn := d.replies[cmd]
		d.mu.Unlock()
		if fn == nil {
			return nil, nil
		}
		return fn(args)
	}
}

// Reply sets the answer to a command: a value, or a func(ipc.Args) (any, error).
func (d *Daemon) Reply(cmd string, reply any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch r := reply.(type) {
	case func(ipc.Args) (any, error):
		d.replies[cmd] = r
	default:
		d.replies[cmd] = func(ipc.Args) (any, error) { return reply, nil }
	}
}

// Fail makes a command answer with an error.
func (d *Daemon) Fail(cmd, message string) {
	d.Reply(cmd, func(ipc.Args) (any, error) { return nil, &ipc.RemoteError{Msg: message} })
}

// Calls lists the requests received, oldest first.
func (d *Daemon) Calls() []Call {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Call(nil), d.calls...)
}

// Last returns the latest request of a command, and whether there was one.
func (d *Daemon) Last(cmd string) (Call, bool) {
	calls := d.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Cmd == cmd {
			return calls[i], true
		}
	}
	return Call{}, false
}
