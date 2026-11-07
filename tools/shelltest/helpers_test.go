package shelltest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// repoRoot is the real repository (the scripts under test live there); it is only read.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// result is the outcome of a script run.
type result struct {
	stdout, stderr string
	code           int
}

// scriptEnv is the process environment minus everything the scripts read (TRUESENSE_*, the sudo
// and pkexec variables), so a developer's own settings never leak into a test, plus extra "K=V".
func scriptEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "TRUESENSE_") || k == "SUDO_USER" || k == "PKEXEC_UID" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// bash runs `bash args...` with the given environment and returns its output and exit code. bash is
// found with the test process's PATH, whatever PATH the script itself is given.
func bash(t *testing.T, env []string, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", args...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := result{stdout: out.String(), stderr: errb.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		r.code = ee.ExitCode()
	default:
		t.Fatalf("bash %v: %v", args, err)
	}
	return r
}

// writeFile creates a file (and its directories) with the given mode.
func writeFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

// pathWith returns a PATH made only of the named programs (symlinks to the real ones), so a test can
// say "no go", "no gh", "no systemctl" without depending on what the machine has installed.
func pathWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p, err := exec.LookPath(n)
		if err != nil {
			t.Fatalf("the test needs %s on PATH: %v", n, err)
		}
		if err := os.Symlink(p, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fakeProgram writes an executable shell script called name into dir.
func fakeProgram(t *testing.T, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), "#!/bin/sh\n"+body+"\n", 0o755)
}
