package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/Order52/truesense/internal/buildinfo"
	"github.com/Order52/truesense/internal/firmware"
)

// ReadOnly is everything the probe may ask the firmware: reads only. A test proves that
// nothing outside this list is ever sent, because the probe runs on laptops nobody has verified.
var ReadOnly = []string{"version", "caps", "sensors", "get_fan", "get_mode", "get_modes", "get_overdrive", "get_boot", "get_kbtimeout", "get_usb",
	"get_battery 1", "get_battery 2", "get_gpu", "get_rear"}

var guidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Module says whether the kernel module is loaded and bound to a device node.
type Module struct {
	Loaded bool `json:"loaded"`
	Device bool `json:"device"`
}

// Answer is the firmware's reply to one probe command, or why there was none.
type Answer struct {
	OK    bool   `json:"ok"`
	Value any    `json:"value,omitempty"` // a firmware.Reply when OK
	Error string `json:"error,omitempty"`
}

// Report is the probe's result, and its JSON is what a maintainer reads to verify a model.
type Report struct {
	Truesense              string            `json:"truesense"`
	Model                  Model             `json:"model"`
	Kernel                 string            `json:"kernel"`
	Writes                 int               `json:"writes"`
	Module                 Module            `json:"module"`
	WMIGUIDs               []string          `json:"wmi_guids"`
	GamingInterfacePresent bool              `json:"gaming_interface_present"`
	Firmware               map[string]Answer `json:"firmware"`
	Note                   string            `json:"note,omitempty"`
	Answered               string            `json:"answered"`
	Verdict                string            `json:"verdict"`
}

// ProbeOptions say where the probe looks. The zero value is a real system; tests point the
// paths at a temporary tree.
type ProbeOptions struct {
	DMIRoot    string // default DMIRoot
	SysRoot    string // default /sys
	DevicePath string // default firmware.DevicePath
	Kernel     string // default: the running kernel's release
	Version    string // default: this build's version
}

func (o ProbeOptions) withDefaults() ProbeOptions {
	if o.DMIRoot == "" {
		o.DMIRoot = DMIRoot
	}
	if o.SysRoot == "" {
		o.SysRoot = "/sys"
	}
	if o.DevicePath == "" {
		o.DevicePath = firmware.DevicePath
	}
	if o.Kernel == "" {
		var u unix.Utsname
		if unix.Uname(&u) == nil {
			o.Kernel = unix.ByteSliceToString(u.Release[:])
		}
	}
	if o.Version == "" {
		o.Version = buildinfo.Version
	}
	return o
}

const unboundNote = "the kernel module is not bound (expected on an unverified model). To let the probe read the firmware: sudo modprobe truesense_wmi force=1   (reads only; undo with: sudo modprobe -r truesense_wmi)"

// Probe collects everything worth knowing about this machine, read-only. It only ever sends
// the commands in ReadOnly, only when the module's device exists, and never fails: a command
// the firmware does not answer is reported in the result.
func Probe(dev firmware.Device, opts ProbeOptions) Report {
	o := opts.withDefaults()
	r := Report{
		Truesense: o.Version, Model: Identify(o.DMIRoot), Kernel: o.Kernel, Writes: 0,
		Module:   Module{Loaded: isDir(filepath.Join(o.SysRoot, "module", "truesense_wmi")), Device: exists(o.DevicePath)},
		WMIGUIDs: wmiGUIDs(o.SysRoot), Firmware: map[string]Answer{},
	}
	r.GamingInterfacePresent = slices.Contains(r.WMIGUIDs, GamingWMIGUID)
	if !r.Module.Device || dev == nil {
		r.Note = unboundNote
	} else {
		for _, name := range ReadOnly {
			reply, err := firmware.Do(dev, name)
			if err != nil {
				r.Firmware[name] = Answer{Error: truncate(err.Error(), 120)}
				continue
			}
			r.Firmware[name] = Answer{OK: true, Value: reply}
		}
	}
	answered := 0
	for _, a := range r.Firmware {
		if a.OK {
			answered++
		}
	}
	r.Answered = "0/0"
	if len(r.Firmware) > 0 {
		r.Answered = fmt.Sprintf("%d/%d", answered, len(ReadOnly))
	}
	r.Verdict = map[Tier]string{TierSupported: "supported: verified on this model", TierUnverified: "unverified: share this report", TierUnsupported: "not an Acer Predator"}[r.Model.Tier]
	return r
}

func isDir(p string) bool  { st, err := os.Stat(p); return err == nil && st.IsDir() }
func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// wmiGUIDs lists the WMI devices the kernel found. Their directory names are the GUID, with a
// "-N" suffix when the same GUID appears twice.
func wmiGUIDs(sysRoot string) []string {
	entries, _ := os.ReadDir(filepath.Join(sysRoot, "bus", "wmi", "devices"))
	seen := map[string]bool{}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if len(name) >= 36 && guidRE.MatchString(name[:36]) {
			seen[name[:36]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// Render is the probe as text for a person.
func Render(r Report) string {
	m := r.Model
	vendor := m.Vendor
	if vendor == "" {
		vendor = "unknown vendor"
	}
	bound, gaming := "no", "not seen"
	if r.Module.Device {
		bound = "yes"
	}
	if r.GamingInterfacePresent {
		gaming = "PRESENT"
	}
	lines := []string{
		strings.TrimRight(fmt.Sprintf("Model     %s (%s), BIOS %s %s", m.Product, vendor, m.BIOS, m.BIOSDate), " "),
		"Verdict   " + r.Verdict,
		fmt.Sprintf("Kernel    %s   module bound: %s", r.Kernel, bound),
		fmt.Sprintf("WMI       Acer gaming interface %s (%d WMI devices)", gaming, len(r.WMIGUIDs)),
	}
	for _, name := range ReadOnly {
		a, ok := r.Firmware[name]
		if !ok {
			continue
		}
		if a.OK {
			lines = append(lines, fmt.Sprintf("  %-14s %s", name, formatReply(a.Value)))
		} else {
			lines = append(lines, fmt.Sprintf("  %-14s (no answer: %s)", name, a.Error))
		}
	}
	if r.Note != "" {
		lines = append(lines, "", r.Note)
	}
	share := ""
	if m.Tier == TierUnverified {
		share = "Share it: truesense probe --json, then " + ReportURL
	}
	lines = append(lines, "", fmt.Sprintf("Nothing was written (%d writes). %s", r.Writes, share))
	return strings.Join(lines, "\n")
}

func formatReply(v any) string {
	reply, ok := v.(firmware.Reply)
	if !ok {
		return fmt.Sprint(v)
	}
	keys := make([]string, 0, len(reply))
	for k := range reply {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + reply[k]
	}
	return strings.Join(parts, " ")
}

// JSON is the probe as indented JSON, for `truesense probe --json` and issue reports.
func JSON(r Report) (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	return string(b), err
}
