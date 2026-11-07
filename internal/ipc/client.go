package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"time"
)

// Errors a caller may want to tell apart.
var (
	// ErrNotRunning: nothing listens on the socket.
	ErrNotRunning = errors.New("the TrueSense daemon is not running (sudo systemctl start truesense)")
	// ErrDenied: the socket exists but this user may not open it.
	ErrDenied = errors.New("no permission to reach the TrueSense daemon socket")
)

// RemoteError is an error the daemon reported for a request.
type RemoteError struct{ Msg string }

func (e *RemoteError) Error() string { return e.Msg }

// Client talks to the daemon.
type Client struct {
	Path    string        // "" = SocketPath()
	Timeout time.Duration // 0 = 3 s
}

func (c *Client) path() string {
	if c.Path != "" {
		return c.Path
	}
	return SocketPath()
}

// Available reports whether the daemon's socket exists.
func (c *Client) Available() bool { _, err := os.Stat(c.path()); return err == nil }

// Call sends a command and returns the raw result.
func (c *Client) Call(cmd string, args Args) (json.RawMessage, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(context.Background(), "unix", c.path())
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, ErrNotRunning
		case errors.Is(err, fs.ErrPermission):
			return nil, ErrDenied
		}
		return nil, fmt.Errorf("daemon not answering: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	req, _ := json.Marshal(Request{Cmd: cmd, Args: args})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, fmt.Errorf("daemon not answering: %w", err)
	}
	line, err := bufio.NewReaderSize(io.LimitReader(conn, MaxLine+1), 4096).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("daemon not answering: %w", err)
	}
	var rep struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(line, &rep); err != nil {
		return nil, fmt.Errorf("daemon not answering: unreadable reply: %w", err)
	}
	if !rep.OK {
		msg := rep.Error
		if msg == "" {
			msg = "daemon error"
		}
		if strings.HasPrefix(msg, "unknown command") {
			return nil, &RemoteError{Msg: fmt.Sprintf("unknown command '%s': the running TrueSense service is older than this app. Update it with: sudo tools/install.sh (it restarts the service)", cmd)}
		}
		return nil, &RemoteError{Msg: msg}
	}
	return rep.Result, nil
}

// CallInto sends a command and decodes the result into out (which may be nil to ignore it).
func (c *Client) CallInto(cmd string, args Args, out any) error {
	raw, err := c.Call(cmd, args)
	if err != nil || out == nil || len(raw) == 0 {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: unexpected reply: %w", cmd, err)
	}
	return nil
}
