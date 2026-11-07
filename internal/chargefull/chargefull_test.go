package chargefull

import (
	"testing"
	"time"
)

func TestEndsWhenUnplugged(t *testing.T) {
	now := time.Unix(10_000, 0)
	s := &State{Since: float64(now.Unix() - 60)}
	if done, _ := Check(s, false, 50, now); done != "unplugged" {
		t.Fatalf("done = %q", done)
	}
}

func TestEndsAfterADay(t *testing.T) {
	now := time.Unix(200_000, 0)
	s := &State{Since: float64(now.Unix()) - 86_401}
	if done, _ := Check(s, true, 60, now); done != "24 hours passed" {
		t.Fatalf("done = %q", done)
	}
	s = &State{Since: float64(now.Unix()) - 3600}
	if done, _ := Check(s, true, 60, now); done != "" {
		t.Fatalf("an hour in is fine: %q", done)
	}
}

func TestEndsFiveMinutesAfterFull(t *testing.T) {
	now := time.Unix(50_000, 0)
	s := &State{Since: float64(now.Unix() - 600)}
	done, changed := Check(s, true, 100, now)
	if done != "" || !changed || s.FullAt == nil {
		t.Fatalf("first time full: must be recorded, not finished: %q %v", done, changed)
	}
	if done, changed = Check(s, true, 100, now.Add(4*time.Minute)); done != "" || changed {
		t.Fatalf("four minutes: still going: %q %v", done, changed)
	}
	if done, _ = Check(s, true, 100, now.Add(6*time.Minute)); done != "the battery is full" {
		t.Fatalf("six minutes: %q", done)
	}
}
