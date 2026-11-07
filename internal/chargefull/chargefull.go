// Package chargefull decides when "charge to 100 % once" is over. The 80 % battery limit is
// lifted for one charge and must come back by itself: when the charger is unplugged, when the
// battery has been full for five minutes, or after 24 hours, whichever comes first.
package chargefull

import "time"

// State is what the daemon remembers while a full charge is running (it is persisted).
type State struct {
	Since  float64  `json:"since"`   // unix seconds when it started
	FullAt *float64 `json:"full_at"` // unix seconds when the battery was first seen full
}

// Limits of the one-off charge.
const (
	MaxDuration = 24 * time.Hour
	FullFor     = 5 * time.Minute
)

// Check looks at the situation and says why the charge is over ("" while it continues). It may
// record that the battery has just been seen full, in which case changed is true and the
// caller must persist the state.
func Check(s *State, ac bool, pct float64, now time.Time) (done string, changed bool) {
	switch {
	case !ac:
		return "unplugged", false
	case now.Sub(time.Unix(int64(s.Since), 0)) > MaxDuration:
		return "24 hours passed", false
	case pct >= 100:
		if s.FullAt == nil {
			t := float64(now.Unix())
			s.FullAt = &t
			changed = true
		}
		if now.Sub(time.Unix(int64(*s.FullAt), 0)) > FullFor {
			return "the battery is full", changed
		}
	}
	return "", changed
}
