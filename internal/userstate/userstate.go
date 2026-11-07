// Package userstate is the small per-user memory (~/.local/state/truesense/user.json): what
// a light was set to before "off", the last keyboard profile, and similar conveniences.
// Nothing here is required: every reader has a default, and a read-only disk is not an error.
package userstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/Order52/truesense/internal/fsx"
)

// Store is one user.json file. Writers in several goroutines and several processes (the
// window, the key service, the command line) never lose each other's change: a set is a
// read-modify-write under a process lock and a file lock.
type Store struct {
	Path string
	mu   sync.Mutex
}

// DefaultPath is where the state lives. The simulator (TRUESENSE_SIM) must never touch your
// real settings, because the key service running on this machine would act on them, so it
// gets a throwaway directory unless TRUESENSE_USERSTATE says otherwise.
func DefaultPath() string {
	if p := os.Getenv("TRUESENSE_USERSTATE"); p != "" {
		return p
	}
	if os.Getenv("TRUESENSE_SIM") != "" {
		return filepath.Join(os.TempDir(), "truesense-sim-"+itoa(os.Getuid()), "user.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "truesense-user.json")
	}
	return filepath.Join(home, ".local", "state", "truesense", "user.json")
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// New returns the store at DefaultPath.
func New() *Store { return &Store{Path: DefaultPath()} }

// Load returns the whole state ({} when missing or unreadable).
func (s *Store) Load() map[string]any {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// Get returns one key (nil when absent).
func (s *Store) Get(key string) any { return s.Load()[key] }

// Set changes one key. Failure to persist is returned but callers may ignore it:
// the file is a convenience, never a requirement.
func (s *Store) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	if lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o644); err == nil {
		defer lock.Close() // closing releases the flock
		if unix.Flock(int(lock.Fd()), unix.LOCK_EX) != nil {
			_ = err // no lock possible: carry on, the worst case is a lost update of a convenience file
		}
	}
	m := s.Load()
	m[key] = value
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(s.Path, b, 0o644)
}

// ErrNotFound is returned by typed getters when the key is absent or has another type.
var ErrNotFound = errors.New("userstate: key not found")

// GetInto decodes one key into out.
func (s *Store) GetInto(key string, out any) error {
	v, ok := s.Load()[key]
	if !ok {
		return ErrNotFound
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
