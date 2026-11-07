package adapt

import (
	"context"

	"github.com/Order52/truesense/internal/doctor"
	"github.com/Order52/truesense/internal/tui"
)

// ---- ProfileBackend

func (b *backend) ProfileNames() []string             { return b.a.ProfileNames() }
func (b *backend) DescribeProfile(name string) string { return b.a.DescribeProfile(name) }
func (b *backend) LookNames() []string                { return b.a.Looks() }

func (b *backend) ApplyProfile(ctx context.Context, name string) error {
	_, err := b.a.ApplyProfile(ctx, name)
	return err
}

func (b *backend) ApplyLook(_ context.Context, name string) error {
	_, err := b.a.ApplyLook(name)
	return err
}

// ---- SystemBackend

func (b *backend) ModuleVersion(context.Context) (string, error) { return b.a.ModuleVersion() }

func (b *backend) DoctorChecks() []string { return b.a.DoctorChecks() }

// RunDoctor runs every health check. The UI has no INFO grade: it counts as OK.
func (b *backend) RunDoctor(ctx context.Context) ([]tui.DoctorResult, error) {
	rep := b.a.RunDoctor(ctx)
	out := make([]tui.DoctorResult, len(rep.Checks))
	for i, c := range rep.Checks {
		status := tui.DoctorOK
		switch c.Status {
		case doctor.WARN:
			status = tui.DoctorWarn
		case doctor.FAIL:
			status = tui.DoctorFail
		}
		out[i] = tui.DoctorResult{Check: c.Check, Status: status, Detail: c.Detail, Fix: c.Fix}
	}
	return out, nil
}

func (b *backend) OpenWindow(context.Context) error { return b.a.OpenWindow() }
