// Package crashguard protects against a boot-time setting that crashes the machine (an
// undervolt applied at boot). A flag file exists from the moment the setting is applied
// until the machine has stayed up long enough to call it stable. If the flag is still
// there at the next start, the previous boot did not survive, so the setting is dropped
// instead of being applied again: a bad value can cost one crash, never a boot loop.
package crashguard

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// StableAfter is how long the machine must stay up after applying before the flag is removed.
const StableAfter = 10 * time.Minute

// Guard is one flag file.
type Guard struct {
	Path string
	Now  func() time.Time

	armedAt time.Time
}

// New returns a Guard for the flag at path.
func New(path string) *Guard { return &Guard{Path: path, Now: time.Now} }

// Tripped reports whether the flag survived from the previous boot, and removes it: the
// caller must then forget the setting rather than apply it again.
func (g *Guard) Tripped() bool {
	if _, err := os.Stat(g.Path); err != nil {
		return false
	}
	_ = os.Remove(g.Path)
	return true
}

// Arm writes the flag; call it before applying. It fails when the flag cannot be written, in
// which case the setting must not be applied (there would be no protection).
func (g *Guard) Arm() error {
	if err := os.MkdirAll(filepath.Dir(g.Path), 0o755); err != nil {
		return err
	}
	g.armedAt = g.Now()
	return os.WriteFile(g.Path, []byte(strconv.FormatInt(g.armedAt.Unix(), 10)), 0o644)
}

// Disarm removes the flag (the setting failed to apply, or it proved stable).
func (g *Guard) Disarm() {
	if err := os.Remove(g.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	g.armedAt = time.Time{}
}

// Tick removes the flag once the machine has been up for StableAfter since Arm. Call it from the main loop.
func (g *Guard) Tick() {
	if !g.armedAt.IsZero() && g.Now().Sub(g.armedAt) > StableAfter {
		g.Disarm()
	}
}

// Armed reports whether the flag is waiting to be proven stable.
func (g *Guard) Armed() bool { return !g.armedAt.IsZero() }
