package audio

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTheCodecIsNeverWrittenWhateverIsAskedFor(t *testing.T) {
	for name, tc := range map[string]struct {
		prog string
		args []string
	}{
		"hda-verb":             {"hda-verb", []string{"/dev/snd/hwC0D0", "0x1", "SET_POWER_STATE", "0x0"}},
		"hda-jack-retask":      {"hda-jack-retask", nil},
		"reconfig by tee":      {"tee", []string{"/sys/class/sound/hwC0D0/reconfig"}},
		"a reconfig path":      {"sh", []string{"-c", "echo 1 > /sys/class/sound/hwC0D0/reconfig"}},
		"a hwC node":           {"cat", []string{"/sys/class/sound/hwC1D0/init_pin_configs"}},
		"reconfig in the name": {"/usr/local/bin/hda-reconfig", nil},
		"upper case":           {"echo", []string{"RECONFIG"}},
		"a pactl argument":     {"pactl", []string{"set-card-profile", "x", "hwC0D0"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, false)
			res := r.c.run(ctx(), tc.prog, tc.args...)
			if res.Code != 126 || !strings.Contains(res.Stderr, "refused") {
				t.Fatalf("result = %+v", res)
			}
			if len(r.sys.calls) != 0 {
				t.Fatalf("the command reached the runner: %v", r.sys.sent())
			}
		})
	}
	r := newRig(t, false)
	for _, ok := range [][]string{{"pactl", "list", "cards"}, {"amixer", "-c0", "cget", "iface=CARD,name=Headphone Jack"}, {"systemctl", "--user", "restart", "wireplumber.service"}} {
		if res := r.c.run(ctx(), ok[0], ok[1:]...); res.Code == 126 {
			t.Errorf("%v was refused", ok)
		}
	}
}

func TestNoPublicOperationCanReachTheCodec(t *testing.T) {
	// Drive every operation that takes free-form input with codec-looking text; none may get through.
	r := newRig(t, false)
	r.c.VoiceSet(ctx(), "hwC0D0/reconfig", "conference")
	r.c.SetOutput(ctx(), "/sys/class/sound/hwC0D0/reconfig")
	r.c.DTSMode(ctx(), "hwC0D0", DTSConfigured)
	r.c.SetMicMute(ctx(), true)
	for _, cmd := range r.sys.sent() {
		if strings.Contains(strings.ToLower(cmd), "reconfig") || strings.Contains(cmd, "hwC") && !strings.HasSuffix(strings.Fields(cmd)[0], "acerultrax-cli") {
			t.Errorf("reached the runner: %s", cmd)
		}
	}
}

func TestEveryCallHasATimeoutSoAHungToolCannotHangTheService(t *testing.T) {
	r := newRig(t, false)
	r.c.CallTimeout = 5 * time.Second
	var deadline time.Time
	r.c.Run = func(c context.Context, name string, args ...string) Result {
		deadline, _ = c.Deadline()
		return Result{}
	}
	r.c.Jack(ctx())
	if deadline.IsZero() || time.Until(deadline) > 5*time.Second {
		t.Errorf("deadline = %v", deadline)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	seen := false
	r.c.Run = func(c context.Context, name string, args ...string) Result { seen = c.Err() != nil; return Result{} }
	r.c.Jack(cancelled)
	if !seen {
		t.Error("the caller's cancellation must reach the tool")
	}
}

func TestExecRunnerTreatsAMissingProgramLikeTheOldCode(t *testing.T) {
	run := ExecRunner()
	if r := run(context.Background(), "/nonexistent/truesense-test-program"); r.Code != 127 || r.Stderr == "" {
		t.Errorf("missing absolute path = %+v", r)
	}
	if r := run(context.Background(), "truesense-no-such-program-xyz"); r.Code != 127 || r.Stderr != "truesense-no-such-program-xyz: not installed" {
		t.Errorf("not on PATH = %+v", r)
	}
}
