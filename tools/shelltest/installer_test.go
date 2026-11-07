package shelltest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installLib runs `source install.sh; <script>` with TRUESENSE_LIB_ONLY=1, so only the functions load
// and nothing is installed. The script gets the install.sh path as $0 and the arguments as $1...
func installLib(t *testing.T, env []string, script string, args ...string) result {
	t.Helper()
	full := append([]string{"-c", `source "$0"; ` + script, filepath.Join(repoRoot(t), "tools", "install.sh")}, args...)
	return bash(t, append(env, "TRUESENSE_LIB_ONLY=1"), full...)
}

// osRelease writes a fake /etc/os-release and returns its path.
func osRelease(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "os-release")
	writeFile(t, p, text, 0o644)
	return p
}

var osReleases = map[string]struct{ content, family string }{
	"arch":    {"ID=arch\n", "arch"},
	"cachy":   {"ID=cachyos\nID_LIKE=arch\n", "arch"},
	"manjaro": {"ID=manjaro\nID_LIKE=arch\n", "arch"},
	"ubuntu":  {"ID=ubuntu\nID_LIKE=debian\n", "debian"},
	"mint":    {"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "debian"},
	"debian":  {"ID=debian\n", "debian"},
	"fedora":  {"ID=fedora\n", "fedora"},
	"nobara":  {"ID=nobara\nID_LIKE=\"rhel centos fedora\"\n", "fedora"},
	"suse":    {"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"\n", "suse"},
	"weird":   {"ID=gentoo\n", "unknown"},
}

func TestFamilyDetection(t *testing.T) {
	t.Parallel()
	for name, c := range osReleases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := installLib(t, scriptEnv("TRUESENSE_OS_RELEASE="+osRelease(t, c.content)), `detect_os; echo "$FAMILY"`)
			if got := strings.TrimSpace(r.stdout); got != c.family {
				t.Errorf("family = %q, want %q (stderr %q)", got, c.family, r.stderr)
			}
		})
	}
}

func TestAMissingOsReleaseIsAnUnknownFamilyNotAnError(t *testing.T) {
	t.Parallel()
	r := installLib(t, scriptEnv("TRUESENSE_OS_RELEASE=/nonexistent/os-release"), `detect_os; echo "$OS_ID $FAMILY"`)
	if r.code != 0 || strings.TrimSpace(r.stdout) != "unknown unknown" {
		t.Errorf("got %d %q %q", r.code, r.stdout, r.stderr)
	}
}

func TestHeaderPackages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, distro, kernel, want string }{
		{"ubuntu", "ubuntu", "6.1.0-test", "linux-headers-6.1.0-test"},
		{"fedora", "fedora", "6.1.0-test", "kernel-devel-6.1.0-test"},
		{"suse default", "suse", "6.1.0-1-default", "kernel-default-devel"},
		{"suse other flavour", "suse", "6.1.0-1-azure", "kernel-azure-devel"},
		{"arch without pkgbase", "arch", "6.1.0-test", "linux-headers"},
		{"unknown", "weird", "6.1.0-test", "the kernel headers for 6.1.0-test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := installLib(t, scriptEnv("TRUESENSE_OS_RELEASE="+osRelease(t, osReleases[tc.distro].content), "TRUESENSE_KERNEL="+tc.kernel), `detect_os; headers_pkg`)
			if got := strings.TrimSpace(r.stdout); got != tc.want {
				t.Errorf("headers_pkg = %q, want %q", got, tc.want)
			}
		})
	}
}

// Nothing in the Go installer needs Python, PyQt or socat: the program is one static binary.
func TestPackageNamesHaveNoPythonQtOrSocat(t *testing.T) {
	t.Parallel()
	want := map[string]map[string]string{
		"arch":   {"dkms": "dkms", "make": "make", "cc": "gcc", "clang": "clang llvm lld", "audio": "noise-suppression-for-voice"},
		"debian": {"dkms": "dkms", "make": "make", "cc": "gcc", "clang": "clang lld", "audio": ""},
		"fedora": {"dkms": "dkms", "make": "make", "cc": "gcc", "clang": "clang lld", "audio": ""},
		"suse":   {"dkms": "dkms", "make": "make", "cc": "gcc", "clang": "clang lld", "audio": ""},
	}
	for distro, names := range osReleases {
		if osReleases[distro].family == "unknown" {
			continue
		}
		for what, pkg := range want[osReleases[distro].family] {
			r := installLib(t, scriptEnv("TRUESENSE_OS_RELEASE="+osRelease(t, names.content)), `detect_os; pkg "$1"`, what)
			if got := strings.TrimSpace(r.stdout); got != pkg {
				t.Errorf("%s: pkg %s = %q, want %q", distro, what, got, pkg)
			}
		}
		for _, gone := range []string{"python", "gui", "socat", "pyqt"} {
			r := installLib(t, scriptEnv("TRUESENSE_OS_RELEASE="+osRelease(t, names.content)), `detect_os; pkg "$1"`, gone)
			if got := strings.TrimSpace(r.stdout); got != "" {
				t.Errorf("%s: pkg %s = %q: the prerequisite no longer exists", distro, gone, got)
			}
		}
	}
}

func TestPrerequisitesAreTheModuleToolchainOnly(t *testing.T) {
	t.Parallel()
	env := func(path string) []string {
		return scriptEnv("PATH="+path, "TRUESENSE_OS_RELEASE="+osRelease(t, osReleases["arch"].content), "TRUESENSE_KERNEL=6.1.0-test")
	}
	// nothing installed: the toolchain for the kernel module and the headers, and systemd is a problem a package cannot fix
	r := installLib(t, env(t.TempDir()), `detect_os; check_prereqs; echo "MISSING=${MISSING[*]}"; printf 'PROBLEM=%s\n' "${PROBLEMS[@]}"`)
	if !strings.Contains(r.stdout, "MISSING=dkms make gcc linux-headers\n") {
		t.Errorf("missing list: %q", r.stdout)
	}
	if !strings.Contains(r.stdout, "PROBLEM=systemd is required") {
		t.Errorf("a system without systemd must be reported as a problem: %q", r.stdout)
	}
	// the toolchain present: only the headers of the (fake) kernel are left, and no systemd problem
	bin := t.TempDir()
	for _, n := range []string{"systemctl", "dkms", "make", "gcc"} {
		fakeProgram(t, bin, n, "exit 0")
	}
	r = installLib(t, env(bin), `detect_os; check_prereqs; echo "MISSING=${MISSING[*]}"; echo "PROBLEMS=${#PROBLEMS[@]}"`)
	if !strings.Contains(r.stdout, "MISSING=linux-headers\n") || !strings.Contains(r.stdout, "PROBLEMS=0") {
		t.Errorf("with the toolchain present: %q", r.stdout)
	}
	for _, out := range []string{r.stdout, r.stderr} {
		for _, word := range []string{"python", "PyQt", "socat"} {
			if strings.Contains(strings.ToLower(out), strings.ToLower(word)) {
				t.Errorf("the prerequisite check mentions %s: %q", word, out)
			}
		}
	}
}

// fakeRoot is a copy of install.sh in a tools/ directory, as the script finds its tree from its own path.
func fakeRoot(t *testing.T) (root, script string) {
	t.Helper()
	root = t.TempDir()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(root, "tools", "install.sh")
	writeFile(t, script, string(b), 0o755)
	return root, script
}

func TestCheckRunsWithoutRootAndReportsOnlyThingsAPackageCanFix(t *testing.T) {
	t.Parallel()
	_, script := fakeRoot(t)
	r := bash(t, scriptEnv("TRUESENSE_OS_RELEASE="+osRelease(t, osReleases["ubuntu"].content)), script, "--check")
	if r.code != 0 && r.code != 1 {
		t.Errorf("exit code %d", r.code)
	}
	for _, want := range []string{"prerequisites", "distribution: ubuntu (family: debian)", "package manager: apt"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("--check output lacks %q:\n%s", want, r.stdout)
		}
	}
	for _, word := range []string{"python", "pyqt", "socat"} {
		if strings.Contains(strings.ToLower(r.stdout+r.stderr), word) {
			t.Errorf("--check mentions %s:\n%s", word, r.stdout)
		}
	}
}

func TestUnknownOptionAndHelp(t *testing.T) {
	t.Parallel()
	_, script := fakeRoot(t)
	if r := bash(t, scriptEnv(), script, "--nope"); r.code != 2 || !strings.Contains(r.stderr, "unknown option: --nope") {
		t.Errorf("--nope: %d %q", r.code, r.stderr)
	}
	r := bash(t, scriptEnv(), script, "--help")
	if r.code != 0 {
		t.Errorf("--help: exit %d", r.code)
	}
	for _, opt := range []string{"--check", "--no-deps", "--force", "--prefix=DIR", "--uninstall", "Safety:"} {
		if !strings.Contains(r.stdout, opt) {
			t.Errorf("--help does not document %s:\n%s", opt, r.stdout)
		}
	}
}

func TestInstallingRequiresRootAndSaysHowToCheckFirst(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("running as root: the refusal cannot be observed (and the script must never run for real)")
	}
	_, script := fakeRoot(t)
	r := bash(t, scriptEnv(), script)
	if r.code != 1 || !strings.Contains(r.stderr, "run with sudo") || !strings.Contains(r.stderr, "--check") {
		t.Errorf("%d %q", r.code, r.stderr)
	}
}

// ---- the three model tiers ---------------------------------------------------------------------------------------------------------------

// dmi writes a fake /sys/class/dmi/id for a product.
func dmi(t *testing.T, product, vendor string) string {
	t.Helper()
	d := t.TempDir()
	for n, v := range map[string]string{"product_name": product, "sys_vendor": vendor, "bios_version": "V1.0"} {
		writeFile(t, filepath.Join(d, n), v+"\n", 0o644)
	}
	return d
}

func TestTheThreeTiersInTheCheckReport(t *testing.T) {
	t.Parallel()
	_, script := fakeRoot(t)
	check := func(product, vendor string) string {
		return bash(t, scriptEnv("TRUESENSE_DMI="+dmi(t, product, vendor), "TRUESENSE_OS_RELEASE="+osRelease(t, osReleases["arch"].content)), script, "--check").stdout
	}
	if out := check("Predator PH16-71", "Acer"); !strings.Contains(out, "(supported)") {
		t.Errorf("the verified model is not reported as supported:\n%s", out)
	}
	for _, similar := range []string{"Predator PH16-72", "Predator PH18-71", "Predator PHN16-71", "Nitro AN16-41"} {
		out := check(similar, "Acer")
		for _, want := range []string{"NOT supported yet", "--force", "probe"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: report lacks %q:\n%s", similar, want, out)
			}
		}
	}
	out := check("Inspiron 15", "Dell Inc.")
	if !strings.Contains(out, "NOT supported") || !strings.Contains(out, "not an Acer Predator") {
		t.Errorf("a Dell is not reported as foreign:\n%s", out)
	}
}

func TestInstallingOnAnotherModelIsRefusedWithTheSafeNextStep(t *testing.T) {
	t.Parallel()
	gate := func(product string, force int) result {
		return installLib(t, scriptEnv("TRUESENSE_DMI="+dmi(t, product, "Acer")), `FORCE=$1; model_gate`, string(rune('0'+force)))
	}
	r := gate("Predator PH18-71", 0)
	if r.code != 1 {
		t.Errorf("similar model: exit %d", r.code)
	}
	for _, want := range []string{"not an Acer Predator PH16-71", "truesense probe", "similar Acer laptop"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("refusal lacks %q: %q", want, r.stderr)
		}
	}
	if r := gate("OptiPlex", 0); r.code != 1 || !strings.Contains(r.stderr, "This is not an Acer Predator") {
		t.Errorf("foreign machine: %d %q", r.code, r.stderr)
	}
	if r := gate("Predator PH16-71", 0); r.code != 0 || !strings.Contains(r.stdout, "model: Predator PH16-71") {
		t.Errorf("verified model: %d %q", r.code, r.stdout)
	}
	if r := gate("Predator PH18-71", 1); r.code != 0 || !strings.Contains(r.stdout, "WARNING") {
		t.Errorf("--force: %d %q", r.code, r.stdout)
	}
}

func TestTiersAsAFunction(t *testing.T) {
	t.Parallel()
	for product, word := range map[string]string{
		"Predator PH16-71":  "(supported)",
		"Predator PHN16-71": "NOT supported yet",
		"Nitro V 15":        "NOT supported yet",
		"OptiPlex":          "not an Acer Predator",
	} {
		r := installLib(t, scriptEnv(), `model_tier "$1"`, product)
		if !strings.Contains(r.stdout, word) {
			t.Errorf("model_tier %q = %q, want it to contain %q", product, r.stdout, word)
		}
	}
}

// ---- find_binary: the program the installer puts on the system --------------------------------------------------------------------------

const sourceVersion = "0.3.17"

// sourceTree is a fake source tree: a version in internal/buildinfo and, optionally, a go.mod.
func sourceTree(t *testing.T, version string, goMod bool) string {
	t.Helper()
	root := t.TempDir()
	if version != "" {
		writeFile(t, filepath.Join(root, "internal/buildinfo/buildinfo.go"), "package buildinfo\n\nconst Version = \""+version+"\"\n", 0o644)
	}
	if goMod {
		writeFile(t, filepath.Join(root, "go.mod"), "module example.com/truesense\n", 0o644)
	}
	return root
}

// program writes a fake "truesense" that answers --version like the real one.
func program(t *testing.T, path, version string) {
	t.Helper()
	writeFile(t, path, "#!/bin/sh\necho \"truesense "+version+"\"\n", 0o755)
}

func findBinary(t *testing.T, root string, env ...string) result {
	t.Helper()
	return installLib(t, scriptEnv(env...), `ROOT=$1; find_binary; echo "BIN=$BIN"`, root)
}

func TestFindBinaryPrefersTheBundleThenTheBuildAndTheOverrideWins(t *testing.T) {
	t.Parallel()
	root := sourceTree(t, sourceVersion, true)
	program(t, filepath.Join(root, "dist/truesense"), sourceVersion)
	if r := findBinary(t, root); r.code != 0 || !strings.Contains(r.stdout, "BIN="+root+"/dist/truesense\n") {
		t.Errorf("only dist/truesense: %d %q %q", r.code, r.stdout, r.stderr)
	}
	program(t, filepath.Join(root, "bin/truesense"), sourceVersion)
	if r := findBinary(t, root); r.code != 0 || !strings.Contains(r.stdout, "BIN="+root+"/bin/truesense\n") {
		t.Errorf("bin/truesense must win over dist/: %d %q %q", r.code, r.stdout, r.stderr)
	}
	override := filepath.Join(t.TempDir(), "mine")
	program(t, override, sourceVersion)
	if r := findBinary(t, root, "TRUESENSE_BIN="+override); r.code != 0 || !strings.Contains(r.stdout, "BIN="+override+"\n") {
		t.Errorf("TRUESENSE_BIN must win: %d %q %q", r.code, r.stdout, r.stderr)
	}
}

func TestFindBinaryRefusesAStaleBinary(t *testing.T) {
	t.Parallel()
	root := sourceTree(t, sourceVersion, true)
	program(t, filepath.Join(root, "bin/truesense"), "0.3.16")
	r := findBinary(t, root)
	if r.code != 1 {
		t.Fatalf("exit %d, want 1: %q", r.code, r.stderr)
	}
	for _, want := range []string{"0.3.16", sourceVersion, "stale", "make build"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal lacks %q: %q", want, r.stderr)
		}
	}
	if strings.Contains(r.stdout, "BIN=") {
		t.Errorf("a stale binary was accepted: %q", r.stdout)
	}
}

func TestFindBinaryExplainsAMissingBinary(t *testing.T) {
	t.Parallel()
	// no program, no go.mod: nothing to build
	// no program, a go.mod, but no Go toolchain: the same explanation (PATH holds nothing)
	for name, root := range map[string]string{"no go.mod": sourceTree(t, sourceVersion, false), "no go installed": sourceTree(t, sourceVersion, true)} {
		r := findBinary(t, root, "PATH="+t.TempDir())
		if r.code != 1 {
			t.Errorf("%s: exit %d, want 1", name, r.code)
		}
		for _, want := range []string{"no program to install", "bin/truesense", "make build", "releases"} {
			if !strings.Contains(r.stderr, want) {
				t.Errorf("%s: the explanation lacks %q: %q", name, want, r.stderr)
			}
		}
	}
	// a program that exists but is not executable does not count
	root := sourceTree(t, sourceVersion, false)
	writeFile(t, filepath.Join(root, "bin/truesense"), "#!/bin/sh\necho truesense "+sourceVersion+"\n", 0o644)
	if r := findBinary(t, root); r.code != 1 || !strings.Contains(r.stderr, "no program to install") {
		t.Errorf("non-executable bin/truesense: %d %q", r.code, r.stderr)
	}
}

func TestFindBinaryRefusesABinaryThatDoesNotRun(t *testing.T) {
	t.Parallel()
	root := sourceTree(t, sourceVersion, false)
	writeFile(t, filepath.Join(root, "bin/truesense"), "#!/bin/sh\nexit 1\n", 0o755)
	if r := findBinary(t, root); r.code != 1 || !strings.Contains(r.stderr, "does not run on this machine") {
		t.Errorf("%d %q", r.code, r.stderr)
	}
}

func TestFindBinaryAcceptsATreeWithoutAVersionFile(t *testing.T) {
	t.Parallel()
	root := sourceTree(t, "", false)
	program(t, filepath.Join(root, "bin/truesense"), "9.9.9")
	if r := findBinary(t, root); r.code != 0 || !strings.Contains(r.stdout, "BIN="+root+"/bin/truesense\n") {
		t.Errorf("nothing to compare with: %d %q %q", r.code, r.stdout, r.stderr)
	}
}

func TestFindBinaryBuildsFromSourceWhenThereIsNoBinaryAndGoIsInstalled(t *testing.T) {
	t.Parallel()
	root := sourceTree(t, sourceVersion, true)
	tools := t.TempDir()
	log := filepath.Join(t.TempDir(), "go.log")
	// the fake go records how it was called and writes the program where -o says
	fakeProgram(t, tools, "go", `echo "CGO_ENABLED=$CGO_ENABLED $*" >> "`+log+`"
while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
printf '#!/bin/sh\necho "truesense `+sourceVersion+`"\n' > "$out"; chmod +x "$out"`)
	r := findBinary(t, root, "PATH="+tools+":"+pathWith(t, "sh", "mkdir", "chmod", "sed", "head", "awk"))
	if r.code != 0 || !strings.Contains(r.stdout, "BIN="+root+"/dist/truesense\n") {
		t.Fatalf("%d %q %q", r.code, r.stdout, r.stderr)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CGO_ENABLED=0", "build", "-trimpath", "-buildvcs=false", "-o dist/truesense", "./cmd/truesense"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("go was called without %q: %q", want, b)
		}
	}

	// a build that fails stops the install with a clear message
	failing := t.TempDir()
	fakeProgram(t, failing, "go", "exit 1")
	r = findBinary(t, sourceTree(t, sourceVersion, true), "PATH="+failing+":"+pathWith(t, "sh", "mkdir", "sed", "head", "awk"))
	if r.code != 1 || !strings.Contains(r.stderr, "go build failed") {
		t.Errorf("failing build: %d %q", r.code, r.stderr)
	}

	// a freshly built program of another version is just as stale as a downloaded one
	wrong := t.TempDir()
	fakeProgram(t, wrong, "go", `while [ $# -gt 0 ]; do [ "$1" = -o ] && out=$2; shift; done
printf '#!/bin/sh\necho "truesense 0.0.1"\n' > "$out"; chmod +x "$out"`)
	r = findBinary(t, sourceTree(t, sourceVersion, true), "PATH="+wrong+":"+pathWith(t, "sh", "mkdir", "chmod", "sed", "head", "awk"))
	if r.code != 1 || !strings.Contains(r.stderr, "stale") {
		t.Errorf("build of the wrong version: %d %q", r.code, r.stderr)
	}
}

// ---- packaging --------------------------------------------------------------------------------------------------------------------------

func TestPackagesBuildTheGoProgramAndNeedNoPythonOrSocat(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"packaging/arch/PKGBUILD", "packaging/aur/PKGBUILD.template"} {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), f))
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		for _, want := range []string{"makedepends=('go' 'gcc')", "go build", "CGO_ENABLED=0", "-trimpath", "./cmd/truesense", "arch=('x86_64')", "truesense-fast", "tools/nvml/truesense-nvml.c", "usr/lib/truesense/truesense-nvml"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q", f, want)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "depends=") && (strings.Contains(line, "python") || strings.Contains(line, "socat")) {
				t.Errorf("%s: %s", f, line)
			}
		}
	}
}
