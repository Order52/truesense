package powermgr

import (
	"encoding/json"
	"os"

	"github.com/Order52/truesense/internal/firmware"
	"github.com/Order52/truesense/internal/fsx"
)

// RecordUnit is what Takeover remembers about one manager it stopped.
type RecordUnit struct {
	Unit       string `json:"unit"`
	WasActive  bool   `json:"was_active"`
	WasEnabled bool   `json:"was_enabled"`
}

// Record is everything Takeover changed, kept on disk so Restore can undo exactly that. It is
// written before anything is stopped.
type Record struct {
	// CPU is the CPU state to go back to on Restore, taken before the first change; nil when
	// nothing has been recorded.
	CPU   *CPUState    `json:"cpu,omitempty"`
	Units []RecordUnit `json:"units,omitempty"`
}

// empty reports whether nothing was recorded (a missing, unreadable or empty record).
func (r Record) empty() bool { return r.CPU == nil && len(r.Units) == 0 }

// UnmarshalJSON skips malformed entries instead of rejecting the whole record: what is
// readable is still worth restoring.
func (r *Record) UnmarshalJSON(data []byte) error {
	var raw struct {
		CPU   json.RawMessage   `json:"cpu"`
		Units []json.RawMessage `json:"units"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = Record{}
	var cpu *CPUState
	if json.Unmarshal(raw.CPU, &cpu) == nil {
		r.CPU = cpu
	}
	for _, u := range raw.Units {
		var ru RecordUnit
		if json.Unmarshal(u, &ru) == nil && ru.Unit != "" {
			r.Units = append(r.Units, ru)
		}
	}
	return nil
}

// LoadRecord reads the record; a missing or unreadable file is an empty record.
func (m *Manager) LoadRecord() Record {
	data, err := os.ReadFile(m.RecordPath)
	if err != nil {
		return Record{}
	}
	var r Record
	if json.Unmarshal(data, &r) != nil {
		return Record{}
	}
	return r
}

// Recorded reports whether a takeover is on record (there is something to restore).
func (m *Manager) Recorded() bool { return !m.LoadRecord().empty() }

func (m *Manager) saveRecord(r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(m.RecordPath, data, 0o644); err != nil {
		return firmware.Errorf("could not remember what was stopped (%v); nothing was changed", err)
	}
	return nil
}

// removeRecord forgets the record. A failure is ignored on purpose: a stale record only makes
// the next Restore a harmless repeat.
func (m *Manager) removeRecord() { _ = os.Remove(m.RecordPath) }
