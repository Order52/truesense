package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Handler runs one command. It must return quickly for reads; writes may take a second.
// The context is cancelled when the server shuts down.
type Handler func(ctx context.Context, args Args) (any, error)

// Server serves Handlers on a Unix socket.
type Server struct {
	Path     string
	Allowed  map[uint32]bool // uids allowed besides root
	Handlers map[string]Handler
	Log      *slog.Logger
	// IOTimeout bounds reading a request and writing a reply (default 3 s). Handlers are not bounded.
	IOTimeout time.Duration

	ln net.Listener
	wg sync.WaitGroup
}

func (s *Server) timeout() time.Duration {
	if s.IOTimeout > 0 {
		return s.IOTimeout
	}
	return 3 * time.Second
}

func (s *Server) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Listen creates the socket (replacing a stale one) with mode 0666: any local user may connect,
// and every request is authorised by uid.
func (s *Server) Listen() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	old := syscall.Umask(0)
	ln, err := net.Listen("unix", s.Path)
	syscall.Umask(old)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.Path, 0o666); err != nil {
		_ = ln.Close()
		return err
	}
	s.ln = ln
	return nil
}

// Serve accepts connections until ctx is cancelled, then waits for the ones in flight.
func (s *Server) Serve(ctx context.Context) error {
	if s.ln == nil {
		return errors.New("ipc: Serve before Listen")
	}
	go func() { <-ctx.Done(); _ = s.ln.Close() }()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.serveConn(ctx, conn)
		}()
	}
}

// Close stops listening and removes the socket file.
func (s *Server) Close() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
	_ = os.Remove(s.Path)
}

func peerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) { cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return cred.Uid, nil
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(s.timeout()))
	uid, err := peerUID(conn)
	var out []byte
	if err != nil {
		out = encode(Response{Error: "cannot identify the caller: " + err.Error()})
	} else {
		line, rerr := readLine(conn)
		if rerr != nil {
			out = encode(Response{Error: rerr.Error()})
		} else {
			_ = conn.SetReadDeadline(time.Time{}) // the handler may take a while; the reply write below has its own deadline
			out = s.HandleLine(ctx, uid, line)
		}
	}
	_ = conn.SetWriteDeadline(time.Now().Add(s.timeout()))
	_, _ = conn.Write(out)
}

func readLine(r io.Reader) ([]byte, error) {
	br := bufio.NewReaderSize(io.LimitReader(r, MaxLine+1), 4096)
	line, err := br.ReadBytes('\n')
	if len(line) > MaxLine {
		return nil, errors.New("request too large")
	}
	if err != nil && !(errors.Is(err, io.EOF) && len(line) > 0) {
		if errors.Is(err, io.EOF) {
			return []byte("{}"), nil
		}
		return nil, fmt.Errorf("reading the request: %w", err)
	}
	return line, nil
}

// HandleLine authorises and runs one request line from the given uid and returns the reply
// line. It never panics and never returns an invalid reply: this is the function that faces
// untrusted input, and the one the fuzz test drives.
func (s *Server) HandleLine(ctx context.Context, uid uint32, line []byte) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			s.logger().Error("handler panicked", "panic", r, "stack", string(debug.Stack()))
			out = encode(Response{Error: fmt.Sprintf("internal error: %v", r)})
		}
	}()
	if uid != 0 && !s.Allowed[uid] {
		return encode(Response{Error: fmt.Sprintf("uid %d is not allowed to control TrueSense", uid)})
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return encode(Response{Error: "bad request: " + err.Error()})
	}
	h, ok := s.Handlers[req.Cmd]
	if !ok {
		return encode(Response{Error: fmt.Sprintf("unknown command '%s'", req.Cmd)})
	}
	res, err := h(ctx, req.Args)
	if err != nil {
		return encode(Response{Error: err.Error()})
	}
	return encode(Response{OK: true, Result: res})
}

func encode(r Response) []byte {
	b, err := json.Marshal(r)
	if err != nil { // a handler returned something that is not JSON: say so instead of dropping the reply
		b, _ = json.Marshal(Response{Error: "internal error: reply is not encodable: " + err.Error()})
	}
	return append(b, '\n')
}
