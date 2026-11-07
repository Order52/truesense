package gpuundervolt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func fakeRun(replies map[string]string, seen *[]string) RunFunc {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		key := strings.Join(args, " ")
		if seen != nil {
			*seen = append(*seen, key)
		}
		if r, ok := replies[key]; ok {
			return r, nil
		}
		return "err usage\n", nil
	}
}

func TestHelperReadsAndWrites(t *testing.T) {
	var seen []string
	h := newHelper("/x/truesense-nvml", fakeRun(map[string]string{"offset": "ok 150\n", "range": "ok -300 250\n", "set-offset -100": "ok\n", "lock 210 2000": "ok\n", "reset-clocks": "ok\n"}, &seen), time.Second)
	if n, err := h.Offset(); err != nil || n != 150 {
		t.Fatalf("offset: %d %v", n, err)
	}
	if lo, hi, err := h.OffsetRange(); err != nil || lo != -300 || hi != 250 {
		t.Fatalf("range: %d %d %v", lo, hi, err)
	}
	if err := h.SetOffset(-100); err != nil {
		t.Fatal(err)
	}
	if err := h.LockClocks(210, 2000); err != nil {
		t.Fatal(err)
	}
	if err := h.ResetClocks(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "offset|range|set-offset -100|lock 210 2000|reset-clocks" {
		t.Fatalf("commands: %v", seen)
	}
}

func TestHelperErrorsAreInWords(t *testing.T) {
	for _, tc := range []struct{ reply, want string }{
		{"err code 4\n", "needs root (the TrueSense service)"},
		{"err code 3\n", "not supported by this GPU or driver"},
		{"err code 77\n", "NVML error 77"},
		{"err library\n", "NVIDIA driver library (libnvidia-ml) not found"},
		{"err init\n", "could not start NVML"},
		{"err nodevice\n", "no NVIDIA GPU found"},
		{"err usage\n", "invalid argument"},
		{"", "gave no answer"},
		{"ok abc\n", "unreadable"},
		{"err\n", "unreadable"},
		{"err code x\n", "unreadable"},
		{"banana\n", "unreadable"},
	} {
		h := newHelper("p", fakeRun(map[string]string{"offset": tc.reply}, nil), time.Second)
		if _, err := h.Offset(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q -> %v, want %q", tc.reply, err, tc.want)
		}
	}
	h := newHelper("p", fakeRun(map[string]string{"offset": "ok 1 2\n"}, nil), time.Second)
	if _, err := h.Offset(); err == nil {
		t.Error("two numbers where one is expected must be an error")
	}
}

func TestADriverThatDoesNotAnswerCannotBlockTheCaller(t *testing.T) {
	block := func(ctx context.Context, _ string, _ ...string) (string, error) { <-ctx.Done(); return "", ctx.Err() }
	h := newHelper("p", block, 50*time.Millisecond)
	start := time.Now()
	_, err := h.Offset()
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the call must return when the timeout ends")
	}
}

func TestAHelperThatCrashesIsAnError(t *testing.T) {
	boom := func(context.Context, string, ...string) (string, error) {
		return "", errors.New("signal: segmentation fault")
	}
	if _, err := newHelper("p", boom, time.Second).Offset(); err == nil || !strings.Contains(err.Error(), "helper failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenNVMLNeedsTheHelper(t *testing.T) {
	t.Setenv("TRUESENSE_NVML", "/nonexistent/truesense-nvml")
	if p, err := HelperPath(); err == nil && !strings.Contains(p, "truesense-nvml") {
		t.Fatalf("unexpected path %q", p)
	}
	if _, err := OpenNVML(); err != nil && !errors.Is(err, ErrHelperMissing) {
		t.Fatalf("a missing helper must say so: %v", err)
	}
}

func FuzzParseReply(f *testing.F) {
	for _, s := range []string{"ok", "ok 1", "ok -5 5", "err code 3", "err library", "", "err", "ok x", "\x00", strings.Repeat("9 ", 200)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		nums, err := parseReply(s)
		if (nums == nil) == (err == nil) && (err != nil || len(nums) != 0) {
			t.Fatalf("a reply is either numbers or an error: %v %v", nums, err)
		}
	})
}
