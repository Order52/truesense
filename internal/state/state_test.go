package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDefaultsFileAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	s := Open(p, map[string]any{"smart_fan": false, "fan_curve": "native"})
	if s.Bool("smart_fan", true) || s.String("fan_curve", "") != "native" {
		t.Fatal("defaults must apply when there is no file")
	}
	s.Set("smart_fan", true)
	s.Set("pl_user", map[string]any{"pl1": 70.0})
	s2 := Open(p, map[string]any{"smart_fan": false, "fan_curve": "native"})
	if !s2.Bool("smart_fan", false) || s2.String("fan_curve", "") != "native" {
		t.Fatal("file must win over defaults, untouched defaults must stay")
	}
	if v, _ := s2.Get("pl_user"); v.(map[string]any)["pl1"] != 70.0 {
		t.Fatalf("pl_user = %v", v)
	}
}

func TestCorruptFileFallsBackToDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(p, map[string]any{"a": "x"})
	if s.String("a", "") != "x" {
		t.Fatal("defaults")
	}
	s.Set("b", 1)
	if Open(p, nil).Float("b", 0) != 1 {
		t.Fatal("the next change must repair the file")
	}
}

func TestConcurrentSets(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state.json"), nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Set("k", float64(i)); s.Snapshot() }()
	}
	wg.Wait()
	if _, ok := s.Get("k"); !ok {
		t.Fatal("lost")
	}
}

func TestReadOnlyDiskIsNotAnError(t *testing.T) {
	s := Open("/proc/nonexistent/state.json", nil)
	s.Set("a", 1) // must not panic or fail
	if s.Float("a", 0) != 1 {
		t.Fatal("the setting must still apply for this boot")
	}
}
