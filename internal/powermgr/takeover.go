package powermgr

import (
	"context"
	"encoding/json"
)

// TakeoverReport is what Takeover did. The JSON names are the Python dict keys; the daemon
// adds "on".
type TakeoverReport struct {
	Found        []string `json:"found"`
	Stopped      []string `json:"stopped"`
	Failed       []string `json:"failed"`
	CPUNotSet    []string `json:"cpu_not_set"`
	CPU          CPUState `json:"cpu"`
	CeilingFixed bool     `json:"ceiling_fixed"`
}

// maxFailureText bounds how much of systemctl's complaint goes into a report.
const maxFailureText = 80

// Takeover stops and disables every running or enabled manager from the fixed list, fixes the
// CPU defaults, and remembers it all. The record is written before anything is stopped, so a
// crash half way can still be undone; if it cannot be written nothing is changed. A second
// takeover keeps the ORIGINAL state to go back to.
func (m *Manager) Takeover(ctx context.Context) (TakeoverReport, error) {
	found := m.Status(ctx)
	rec := m.LoadRecord()
	if rec.CPU == nil {
		st := m.CPUState() // the state to go back to on restore, taken before the first change
		rec.CPU = &st
	}
	known := make(map[string]bool, len(rec.Units))
	for _, u := range rec.Units {
		known[u.Unit] = true
	}
	for _, f := range found {
		if !known[f.Unit] {
			rec.Units = append(rec.Units, RecordUnit{Unit: f.Unit, WasActive: f.Active, WasEnabled: f.Enabled})
		}
	}
	if err := m.saveRecord(rec); err != nil {
		return TakeoverReport{}, err
	}

	rep := TakeoverReport{Found: []string{}, Stopped: []string{}, Failed: []string{}}
	for _, f := range found {
		rep.Found = append(rep.Found, f.Unit)
		code, msg := m.Run(ctx, "systemctl", "disable", "--now", f.Unit+".service")
		if code != 0 && m.isActive(ctx, f.Unit) {
			rep.Failed = append(rep.Failed, f.Unit+": "+truncate(msg, maxFailureText))
		} else {
			rep.Stopped = append(rep.Stopped, f.Unit)
		}
	}
	// Defaults only when a manager was in charge or turbo was left off.
	rep.CPUNotSet = []string{}
	if nt := m.CPUState().NoTurbo; len(found) > 0 || equal(nt, "1") {
		rep.CPUNotSet = m.setCPU(defaultCPU())
	}
	// A ceiling left low by turbo being off is raised whatever the reason.
	for _, c := range m.FixCeiling() {
		rep.CPUNotSet = append(rep.CPUNotSet, "ceiling "+c)
	}
	rep.CPU = m.CPUState()
	rep.CeilingFixed = len(m.CeilingLow()) == 0
	return rep, nil
}

// truncate cuts s to at most n characters (not bytes).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// RestoreReport is what Restore did. The JSON names are the Python dict keys; the daemon adds
// "on". When there was nothing on record, Note says so and cpu_not_set is absent.
type RestoreReport struct {
	Restored  []string
	CPUNotSet []string
	CPU       CPUState
	Note      string
}

// MarshalJSON keeps the Python shapes: {"restored", "cpu_not_set", "cpu"} normally and
// {"restored", "cpu", "note"} when nothing was recorded.
func (r RestoreReport) MarshalJSON() ([]byte, error) {
	if r.Note != "" {
		return json.Marshal(struct {
			Restored []string `json:"restored"`
			CPU      CPUState `json:"cpu"`
			Note     string   `json:"note"`
		}{r.Restored, r.CPU, r.Note})
	}
	return json.Marshal(struct {
		Restored  []string `json:"restored"`
		CPUNotSet []string `json:"cpu_not_set"`
		CPU       CPUState `json:"cpu"`
	}{r.Restored, r.CPUNotSet, r.CPU})
}

// Restore starts the managers again exactly as they were and puts the CPU settings back. A
// unit that is not on the fixed list is never touched, whatever the record says. The record is
// removed afterwards.
func (m *Manager) Restore(ctx context.Context) RestoreReport {
	rec := m.LoadRecord()
	if rec.empty() {
		return RestoreReport{Restored: []string{}, CPU: m.CPUState(), Note: "nothing was changed by TrueSense"}
	}
	done := []string{}
	for _, u := range rec.Units {
		if !isKnownUnit(u.Unit) {
			continue // a hand-edited record can never make us touch another service
		}
		if u.WasEnabled {
			m.Run(ctx, "systemctl", "enable", u.Unit+".service")
		}
		if u.WasActive {
			m.Run(ctx, "systemctl", "start", u.Unit+".service")
		}
		done = append(done, u.Unit)
	}
	bad := []string{}
	if rec.CPU != nil {
		bad = m.setCPU(*rec.CPU)
	}
	m.removeRecord()
	return RestoreReport{Restored: done, CPUNotSet: bad, CPU: m.CPUState()}
}
