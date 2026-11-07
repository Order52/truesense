package userstate

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestSetGetRoundTrip(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "sub", "user.json")}
	if got := s.Get("x"); got != nil {
		t.Fatalf("empty store returned %v", got)
	}
	if err := s.Set("rear_last", map[string]any{"mode": "wave", "brightness": 60}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Mode       string `json:"mode"`
		Brightness int    `json:"brightness"`
	}
	if err := s.GetInto("rear_last", &out); err != nil || out.Mode != "wave" || out.Brightness != 60 {
		t.Fatalf("got %+v, %v", out, err)
	}
	if err := s.GetInto("missing", &out); err != ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentWritersLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := &Store{Path: path} // separate stores: the file lock has to do the work
			_ = s.Set(string(rune('a'+i)), i)
		}(i)
	}
	wg.Wait()
	if n := len((&Store{Path: path}).Load()); n != 20 {
		t.Fatalf("%d of 20 keys survived", n)
	}
}

func TestCorruptFileIsEmptyNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	s := &Store{Path: path}
	if err := s.Set("a", 1); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	if len(s.Load()) != 0 {
		t.Fatal("a corrupt file must read as empty")
	}
	if err := s.Set("b", 2); err != nil || s.Get("b") == nil {
		t.Fatal("and must be repaired by the next write")
	}
}

func TestSimUsesAThrowawayPath(t *testing.T) {
	t.Setenv("TRUESENSE_USERSTATE", "")
	t.Setenv("TRUESENSE_SIM", "1")
	if p := DefaultPath(); filepath.Base(filepath.Dir(p)) == "truesense" {
		t.Fatalf("the simulator must not use the real state directory: %s", p)
	}
}
