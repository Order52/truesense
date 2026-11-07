package audio

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const dtsCLI = "acerultrax-cli"

// FindDTS looks for AcerUltraX's command: on PATH first, then in the places its installer uses
// (the user's ~/.local/bin, the invoking user's when run with sudo, /usr/local, /usr).
func FindDTS(lookPath func(string) (string, error), homes ...string) string {
	if p, err := lookPath(dtsCLI); err == nil {
		return p
	}
	var candidates []string
	for _, h := range homes {
		if h != "" {
			candidates = append(candidates, filepath.Join(h, ".local", "bin", dtsCLI))
		}
	}
	candidates = append(candidates, "/usr/local/bin/"+dtsCLI, "/usr/bin/"+dtsCLI)
	return firstExisting(candidates...)
}

// HostDTSLocator is the production DTSBinary: it is evaluated on every use, because AcerUltraX
// can be installed while the audio service runs. When run with sudo (installer, doctor) it also
// looks in the invoking user's home, not only root's.
func HostDTSLocator(lookPath func(string) (string, error), getenv func(string) string) func() string {
	return func() string {
		home, _ := os.UserHomeDir()
		var sudoHome string
		if name := getenv("SUDO_USER"); name != "" {
			if u, err := user.Lookup(name); err == nil {
				sudoHome = u.HomeDir
			}
		}
		return FindDTS(lookPath, home, sudoHome)
	}
}

// FileLock takes an exclusive advisory lock on path (creating it) and returns the function that
// releases it. It blocks until the lock is free.
func FileLock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- a fixed per-user lock path
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
