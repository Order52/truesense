// Package sdnotify speaks the systemd notification protocol (sd_notify(3)) for services
// declared Type=notify with a watchdog. Outside systemd (NOTIFY_SOCKET unset) every call is
// a silent no-op, so the same binary runs in a terminal and in tests.
package sdnotify

import (
	"net"
	"os"
	"strconv"
	"time"
)

// Notify sends one state string ("READY=1", "WATCHDOG=1", ...). It reports whether a
// notification socket exists; a missing socket is not an error.
func Notify(state string) (bool, error) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return false, nil
	}
	if addr[0] == '@' { // abstract socket
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(state)); err != nil {
		return false, err
	}
	return true, nil
}

// Ready tells systemd the service finished starting.
func Ready() error { _, err := Notify("READY=1"); return err }

// Watchdog pings the systemd watchdog. Call it only while the main loop is healthy.
func Watchdog() error { _, err := Notify("WATCHDOG=1"); return err }

// Stopping tells systemd the service has begun shutting down.
func Stopping() error { _, err := Notify("STOPPING=1"); return err }

// Status sets the one-line status shown by `systemctl status`.
func Status(text string) error { _, err := Notify("STATUS=" + text); return err }

// WatchdogInterval returns how often to ping (half the configured timeout, as systemd
// recommends), or 0 when the watchdog is not enabled for this process.
func WatchdogInterval() time.Duration {
	usec, err := strconv.ParseInt(os.Getenv("WATCHDOG_USEC"), 10, 64)
	if err != nil || usec <= 0 {
		return 0
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0
	}
	return time.Duration(usec) * time.Microsecond / 2
}
