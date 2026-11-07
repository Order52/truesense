package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const lockName = "truesense-gui.lock"

// lockInfo is what the running server publishes so that a second `truesense gui` can reach it.
type lockInfo struct {
	Port    int    `json:"port"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Version string `json:"version,omitempty"`
}

// DefaultRuntimeDir is where the single-instance lock lives: $XDG_RUNTIME_DIR (private to
// the user, tmpfs, gone at logout), or a 0700 directory of ours in the temp directory.
func DefaultRuntimeDir() (string, error) {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir, nil
	}
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("truesense-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("gui: runtime directory: %w", err)
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("gui: runtime directory: %w", err)
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || st.Mode().Perm() != 0o700 || !ok || int(owner.Uid) != os.Getuid() {
		return "", fmt.Errorf("gui: %s is not a private directory of this user", dir)
	}
	return dir, nil
}

// instance is the held lock. flock ties the lock to the process: it vanishes with it, even
// after a crash, so a stale file is never mistaken for a running server.
type instance struct{ f *os.File }

func acquire(dir string) (*instance, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("gui: lock directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("gui: lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("gui: lock file: %w", err)
	}
	return &instance{f: f}, nil
}

// publish writes the port and token for a second launch to read.
func (i *instance) publish(info lockInfo) error {
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := i.f.Truncate(0); err != nil {
		return fmt.Errorf("gui: lock file: %w", err)
	}
	if _, err := i.f.WriteAt(b, 0); err != nil {
		return fmt.Errorf("gui: lock file: %w", err)
	}
	return i.f.Sync()
}

// release removes the published token first, then drops the lock.
func (i *instance) release() {
	_ = i.f.Truncate(0)
	_ = os.Remove(i.f.Name())
	_ = syscall.Flock(int(i.f.Fd()), syscall.LOCK_UN)
	_ = i.f.Close()
}

// readLive returns the published info only when a process really holds the lock.
func readLive(dir string) (lockInfo, bool) {
	f, err := os.Open(filepath.Join(dir, lockName))
	if err != nil {
		return lockInfo{}, false
	}
	defer f.Close() //nolint:errcheck // read-only
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return lockInfo{}, false // nobody holds it: a leftover file
	}
	var info lockInfo
	if err := json.NewDecoder(f).Decode(&info); err != nil || info.Port <= 0 || info.Token == "" {
		return lockInfo{}, false
	}
	return info, true
}
