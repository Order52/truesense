package gui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Run starts the window server and shows the window. It returns nil when ctx ends, when the
// page asks to quit, or when no page has been connected for Options.IdleTimeout; it returns
// ErrRunning (wrapped) when this user already has a server (see Resume).
func Run(ctx context.Context, o Options) error {
	if err := o.withDefaults(); err != nil {
		return err
	}
	inst, err := acquire(o.RuntimeDir)
	if err != nil {
		return err
	}
	defer inst.release()

	ln, err := listenLoopback(ctx, o.Addr)
	if err != nil {
		return err
	}
	session, err := newToken()
	if err != nil {
		_ = ln.Close()
		return err
	}
	addr := ln.Addr().(*net.TCPAddr)
	quit := make(chan struct{})
	var quitOnce sync.Once
	srv := newServer(ln.Addr().String(), session, o, func() { quitOnce.Do(func() { close(quit) }) })
	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
		// No WriteTimeout: the live stream and long commands set their own write deadlines.
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	if err := inst.publish(lockInfo{Port: addr.Port, Token: session, PID: os.Getpid(), Version: o.Version}); err != nil {
		_ = httpSrv.Close()
		return err
	}
	o.Log.Info("window server listening", "addr", ln.Addr().String())
	code, err := srv.mintCode()
	if err != nil {
		_ = httpSrv.Close()
		return err
	}
	if err := o.Open(windowURL(addr.Port, code, o.Page)); err != nil {
		o.Log.Warn("could not open the window", "err", err)
	}

	check := max(o.IdleTimeout/4, 10*time.Millisecond)
	tick := time.NewTicker(check)
	defer tick.Stop()
	var reason error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-quit:
			break loop
		case err := <-serveErr:
			reason = fmt.Errorf("gui: server: %w", err)
			break loop
		case <-tick.C:
			if srv.idleSince() >= o.IdleTimeout {
				o.Log.Info("no page for a while: window server stops")
				break loop
			}
		}
	}
	srv.hub.stop()
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		_ = httpSrv.Close() // streams that do not end in time are cut
	}
	return reason
}

func listenLoopback(ctx context.Context, addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("gui: listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("gui: %q is not a loopback address: the window server never listens beyond this computer", addr)
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("gui: listen: %w", err)
	}
	return ln, nil
}

// newToken is 256 random bits, URL-safe: whoever has a session token may drive the laptop's firmware.
func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("gui: token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
