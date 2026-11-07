// Package powermgr deals with other power managers (auto-cpufreq, TLP, tuned,
// power-profiles-daemon, system76-power): they change CPU governors, turbo and power limits
// behind TrueSense's back.
//
// "TrueSense manages power" (off by default) stops and disables the ones that are running,
// remembers exactly what it did, puts the CPU back to the kernel defaults (governor powersave,
// energy preference balance_performance, turbo on: a manager that was stopped half way can
// leave turbo OFF, which caps the CPU at its base clock), and Restore undoes all of it. Only the
// fixed list below is ever touched, and a hand-edited record can never make this package start or
// enable anything else. Every effect is injected: the systemctl runner, the sysfs root and the
// record's path.
package powermgr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Units are the only services this package ever stops, starts, enables or disables.
func Units() []string {
	return []string{"auto-cpufreq", "tlp", "tuned", "tuned-ppd", "power-profiles-daemon", "system76-power"}
}

func isKnownUnit(name string) bool {
	for _, u := range Units() {
		if u == name {
			return true
		}
	}
	return false
}

const (
	// DefaultRecordPath is where Takeover remembers what it stopped, so Restore can undo it
	// across restarts of the daemon.
	DefaultRecordPath = "/var/lib/truesense/power_managers.json"
	// DefaultCPUDir is the kernel's CPU sysfs directory.
	DefaultCPUDir = "/sys/devices/system/cpu"

	systemctlTimeout = 30 * time.Second
)

// RecordPathFromEnv is the record path: TRUESENSE_PM_RECORD when set (tests, unusual
// installs), else DefaultRecordPath.
func RecordPathFromEnv() string {
	if p := os.Getenv("TRUESENSE_PM_RECORD"); p != "" {
		return p
	}
	return DefaultRecordPath
}

// Runner runs an external program and returns its exit code and its output (standard output,
// or standard error when there is none). A program that could not be started is code 1 with
// the reason as the output, the way the Python implementation reported it.
type Runner func(ctx context.Context, name string, args ...string) (code int, out string)

// ExecRunner is the real Runner: it runs the program with LC_ALL=C so the output is the same on
// every machine, and gives up after 30 seconds.
func ExecRunner(ctx context.Context, name string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), out
		}
		return 1, err.Error()
	}
	return 0, out
}

// Manager is the detection and takeover of power managers over injected dependencies.
type Manager struct {
	Run Runner
	// CPUDir is /sys/devices/system/cpu (tests use a temp tree).
	CPUDir string
	// RecordPath is the file Takeover remembers its work in.
	RecordPath string
}

// New returns a Manager over the real system: systemctl, the real sysfs and the record at
// RecordPathFromEnv.
func New() *Manager {
	return &Manager{Run: ExecRunner, CPUDir: DefaultCPUDir, RecordPath: RecordPathFromEnv()}
}

// Unit is a known power manager that is running or enabled.
type Unit struct {
	Unit    string `json:"unit"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
}

func (m *Manager) isActive(ctx context.Context, unit string) bool {
	_, out := m.Run(ctx, "systemctl", "is-active", unit+".service")
	return out == "active"
}

// Status returns every known manager that is running or enabled.
func (m *Manager) Status(ctx context.Context) []Unit {
	out := []Unit{}
	for _, u := range Units() {
		active := m.isActive(ctx, u)
		_, e := m.Run(ctx, "systemctl", "is-enabled", u+".service")
		enabled := e == "enabled"
		if active || enabled {
			out = append(out, Unit{Unit: u, Active: active, Enabled: enabled})
		}
	}
	return out
}

// CPUState is the turbo switch, governor and energy preference as they are now (cpu0 stands
// for all cores). A nil field could not be read. The JSON names are the Python dict keys.
type CPUState struct {
	NoTurbo  *string `json:"no_turbo"`
	Governor *string `json:"governor"`
	EPP      *string `json:"epp"`
}

// UnmarshalJSON reads a remembered state, ignoring any value that is not text: a hand-edited
// record must not make the whole record unreadable.
func (s *CPUState) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	text := func(key string) *string {
		var v *string
		if json.Unmarshal(raw[key], &v) != nil {
			return nil
		}
		return v
	}
	*s = CPUState{NoTurbo: text("no_turbo"), Governor: text("governor"), EPP: text("epp")}
	return nil
}

func ptr(s string) *string { return &s }

// defaultCPU is what the CPU is put back to when a manager was in charge.
func defaultCPU() CPUState {
	return CPUState{NoTurbo: ptr("0"), Governor: ptr("powersave"), EPP: ptr("balance_performance")}
}

func readTrim(path string) *string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return ptr(strings.TrimSpace(string(b)))
}

// overwrite writes to an existing sysfs attribute; it never creates a file.
func overwrite(path, value string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return false
	}
	_, werr := f.WriteString(value)
	return f.Close() == nil && werr == nil
}

func equal(a *string, b string) bool { return a != nil && *a == b }

// CPUState reads the CPU's current turbo switch, governor and energy preference.
func (m *Manager) CPUState() CPUState {
	return CPUState{
		NoTurbo:  readTrim(filepath.Join(m.CPUDir, "intel_pstate", "no_turbo")),
		Governor: readTrim(filepath.Join(m.CPUDir, "cpu0", "cpufreq", "scaling_governor")),
		EPP:      readTrim(filepath.Join(m.CPUDir, "cpu0", "cpufreq", "energy_performance_preference")),
	}
}

// setCPU writes governor and energy preference to every core and the turbo switch, then reads
// each back. It returns what could not be set.
func (m *Manager) setCPU(st CPUState) []string {
	bad := []string{}
	turbo := filepath.Join(m.CPUDir, "intel_pstate", "no_turbo")
	if st.NoTurbo != nil && readTrim(turbo) != nil {
		overwrite(turbo, *st.NoTurbo)
		if !equal(readTrim(turbo), *st.NoTurbo) {
			bad = append(bad, "turbo")
		}
	}
	for _, f := range []struct {
		key, file string
		want      *string
	}{{"governor", "scaling_governor", st.Governor}, {"epp", "energy_performance_preference", st.EPP}} {
		if f.want == nil {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(m.CPUDir, "cpu[0-9]*", "cpufreq", f.file))
		wrong := false
		for _, p := range files {
			overwrite(p, *f.want)
		}
		for _, p := range files {
			if !equal(readTrim(p), *f.want) {
				wrong = true
			}
		}
		if wrong {
			bad = append(bad, f.key)
		}
	}
	return bad
}

// CeilingLow lists the cores (their cpufreq directories) whose frequency ceiling
// (scaling_max_freq) is below what the core can do. Turning turbo off makes the kernel lower
// the ceiling, and turning it on again does not raise it.
func (m *Manager) CeilingLow() []string {
	low := []string{}
	files, _ := filepath.Glob(filepath.Join(m.CPUDir, "cpu[0-9]*", "cpufreq", "cpuinfo_max_freq"))
	for _, f := range files {
		dir := filepath.Dir(f)
		hw, cur := readTrim(f), readTrim(filepath.Join(dir, "scaling_max_freq"))
		if hw == nil || cur == nil {
			continue
		}
		h, errH := strconv.ParseInt(*hw, 10, 64)
		c, errC := strconv.ParseInt(*cur, 10, 64)
		if errH == nil && errC == nil && isDigits(*hw) && isDigits(*cur) && c < h {
			low = append(low, dir)
		}
	}
	return low
}

// isDigits is Python's str.isdigit for the ASCII sysfs numbers (strconv would also accept a sign).
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FixCeiling raises every low ceiling to the core's own maximum (what the service also does
// when it stops) and reads it back. It returns the cores that did not take it.
func (m *Manager) FixCeiling() []string {
	bad := []string{}
	for _, dir := range m.CeilingLow() {
		want := readTrim(filepath.Join(dir, "cpuinfo_max_freq"))
		cur := filepath.Join(dir, "scaling_max_freq")
		if want != nil {
			overwrite(cur, *want)
		}
		if want == nil || !equal(readTrim(cur), *want) {
			bad = append(bad, filepath.Base(filepath.Dir(dir)))
		}
	}
	return bad
}
