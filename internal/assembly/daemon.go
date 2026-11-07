package assembly

import (
	"context"
	"log/slog"
	"os"
	osuser "os/user"
	"strconv"

	"github.com/Order52/truesense/internal/crashguard"
	"github.com/Order52/truesense/internal/daemon"
	"github.com/Order52/truesense/internal/gamerules"
	"github.com/Order52/truesense/internal/ipc"
	"github.com/Order52/truesense/internal/state"
	"github.com/Order52/truesense/internal/thermal"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// RunDaemon is `truesense daemon`: the root service. It returns when ctx is cancelled (a
// signal); the fans and the clocks are handed back on every way out.
func RunDaemon(ctx context.Context, s *Services, log *slog.Logger) error {
	cfg := s.Cfg
	for _, p := range s.Issues {
		log.Warn("config problem", "problem", p)
	}
	if len(s.Issues) > 0 {
		log.Warn("falling back to the built-in defaults for the whole configuration")
	}
	run := envOr("TRUESENSE_RUN", "/run/truesense")
	if err := os.MkdirAll(run, 0o755); err != nil {
		return err
	}
	auto := "off"
	if cfg.Daemon.AutoProfile {
		auto = "native"
	}
	st := state.Open(state.DefaultPath(), map[string]any{
		"smart_fan": cfg.Daemon.SmartFan, "auto_profile": cfg.Daemon.AutoProfile, "auto_mode": auto, "fan_curve": "native", "fan_curve_data": nil, "pl_user": nil,
		"smart_profile": "native", "smart_profile_data": nil, "operating_follow": cfg.Keyboard.OperatingFollow, "profile_hold_until": 0.0,
	})
	st.Set("uv_applied", nil) // offsets never survive a reboot, so nothing is applied now
	st.Set("gpu_uv_applied", nil)

	var uids []uint32
	home := "/root"
	if u, err := osuser.Lookup(cfg.User); err == nil {
		if id, err := strconv.ParseUint(u.Uid, 10, 32); err == nil {
			uids = append(uids, uint32(id))
		}
		home = u.HomeDir
	}
	d := daemon.New(daemon.Deps{
		Config: cfg, State: st, HW: s.HW, Sensors: s.Sampler, Cap: thermal.Writer{}, Power: s.Power, UV: s.UV, GPUUV: s.GPUUV, PM: s.PM,
		Curves: s.Curves, Profiles: s.Smart, Looks: s.Looks, Lights: s.KB, OSD: s.OSD, Touchpad: s.Touchpad,
		Procs:   func() []gamerules.Process { return gamerules.ListProcesses("/proc") },
		UVGuard: crashguard.New(envOr("TRUESENSE_UV_PENDING", "/var/lib/truesense/uv-pending")), GPUGuard: crashguard.New(envOr("TRUESENSE_GPU_UV_PENDING", "/var/lib/truesense/gpu-uv-pending")),
		Clock: daemon.RealClock(), Log: log, RunDir: run, TmpState: "/tmp/truesense-state", LegacyFlag: legacyFlag(home),
	})
	return d.Run(ctx, ipc.SocketPath(), uids)
}
