// Package state is the daemon's persistent settings (they survive reboots): one JSON file,
// /var/lib/truesense/state.json, written atomically. Defaults come from the configuration.
package state

import (
	"encoding/json"
	"maps"
	"os"
	"sync"

	"github.com/Order52/truesense/internal/fsx"
)

// DefaultPath is where the daemon keeps its state (TRUESENSE_STATE overrides it).
func DefaultPath() string {
	if p := os.Getenv("TRUESENSE_STATE"); p != "" {
		return p
	}
	return "/var/lib/truesense/state.json"
}

// Store is a concurrency-safe map persisted on every change. Values are JSON values
// (string, bool, float64, []any, map[string]any, nil) as decoded from the file.
type Store struct {
	path string
	mu   sync.Mutex
	data map[string]any
}

// Open loads the file over the defaults. A missing or corrupt file is not an error: the
// defaults apply (a corrupt file is replaced by the next change).
func Open(path string, defaults map[string]any) *Store {
	s := &Store{path: path, data: maps.Clone(defaults)}
	if s.data == nil {
		s.data = map[string]any{}
	}
	if b, err := os.ReadFile(path); err == nil {
		var onDisk map[string]any
		if json.Unmarshal(b, &onDisk) == nil {
			maps.Copy(s.data, onDisk)
		}
	}
	return s
}

// Get returns a value and whether it is set.
func (s *Store) Get(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok
}

// String returns a text value, or def.
func (s *Store) String(key, def string) string {
	if v, ok := s.Get(key); ok {
		if t, ok := v.(string); ok {
			return t
		}
	}
	return def
}

// Bool returns a boolean value, or def.
func (s *Store) Bool(key string, def bool) bool {
	if v, ok := s.Get(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// Float returns a numeric value, or def.
func (s *Store) Float(key string, def float64) float64 {
	if v, ok := s.Get(key); ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return def
}

// Set changes one value and persists the whole file. A read-only disk is not an error: the
// setting still applies for this boot.
func (s *Store) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	if b, err := json.Marshal(s.data); err == nil {
		_ = fsx.WriteAtomic(s.path, b, 0o644)
	}
}

// Snapshot returns a copy of every setting.
func (s *Store) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.data)
}
