package gamerules

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// DefaultProcRoot is where the kernel shows running processes.
const DefaultProcRoot = "/proc"

// ListProcesses reads the running programs from a /proc tree (root, empty meaning the real
// /proc; tests pass a temp tree). A process whose name cannot be read is skipped, one whose
// command line cannot be read is kept with its name only: processes come and go while the
// directory is read, so a miss is normal and never an error. An unreadable root gives an empty
// list.
func ListProcesses(root string) []Process {
	if root == "" {
		root = DefaultProcRoot
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var procs []Process
	for _, e := range entries {
		if _, err := strconv.ParseUint(e.Name(), 10, 64); err != nil || !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil {
			continue
		}
		p := Process{Comm: string(comm)}
		if f, err := os.Open(filepath.Join(dir, "cmdline")); err == nil {
			buf, _ := io.ReadAll(io.LimitReader(f, maxCmdline))
			_ = f.Close()
			p.Cmdline = buf
		}
		procs = append(procs, p)
	}
	return procs
}
