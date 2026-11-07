package api

import (
	"context"
	"io"

	"github.com/Order52/truesense/internal/assembly"
	"github.com/Order52/truesense/internal/doctor"
	"github.com/Order52/truesense/internal/models"
	"github.com/Order52/truesense/internal/selftest"
	"golang.org/x/sys/unix"
)

func kernelRelease() string {
	var u unix.Utsname
	_ = unix.Uname(&u)
	return unix.ByteSliceToString(u.Release[:])
}

// doctorEnv is the health check's view of this machine: the real filesystem and programs, the
// firmware, the daemon and the sound system, unless a test replaced it.
func (a *API) doctorEnv() *doctor.Env {
	if a.DoctorEnv != nil {
		return a.DoctorEnv()
	}
	e := doctor.NewEnv(a.s.HW, assembly.NewIPCDaemon(a.s.IPC), a.s.Audio)
	e.CeilingLow = a.s.PM.CeilingLow
	e.Config = a.LoadConfig
	if a.FnKeyNode != nil {
		e.FnKeyNode = a.FnKeyNode
	}
	return e
}

// RunDoctor runs the whole health check (read-only).
func (a *API) RunDoctor(ctx context.Context) doctor.Report {
	return doctor.Run(ctx, a.doctorEnv(), doctor.Checks())
}

// DoctorChecks lists the titles of the health checks in the order the report shows them.
func (a *API) DoctorChecks() []string {
	var titles []string
	for _, c := range doctor.Checks() {
		titles = append(titles, c.Title)
	}
	return titles
}

// SelfTest sets, reads back and restores every firmware feature, then runs the health check,
// writing each PASS / FAIL / SKIP line to out as it goes. Every setting it touches is back as
// it was when it returns. skipFans leaves the fans out.
func (a *API) SelfTest(ctx context.Context, out io.Writer, skipFans bool) selftest.Report {
	deps := selftest.Deps{
		HW: a.s.HW, Keyboard: a.s.KB, Audio: a.s.Audio, Out: out, Now: a.Now, Sleep: a.Sleep,
		Doctor: func(ctx context.Context, w io.Writer) int {
			r := a.RunDoctor(ctx)
			doctor.RenderText(w, r, false)
			return r.ExitCode()
		},
	}
	if a.DaemonAvailable() { // no service: nothing runs the fans or the mode, nothing to pause
		deps.Daemon = assembly.NewIPCDaemon(a.s.IPC)
	}
	return selftest.Run(ctx, deps, selftest.Options{SkipFans: skipFans})
}

// Probe is the read-only report of this laptop for model support: it never writes a setting
// (only the commands of models.ReadOnly are ever sent).
func (a *API) Probe() models.Report { return models.Probe(a.s.Dev, a.ProbeOptions) }

// ModuleVersion is the kernel module's version.
func (a *API) ModuleVersion() (string, error) { return a.s.HW.Version() }
