package shelltest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layout says what the fake release bundle contains.
type layout struct {
	inside    string // the version buildinfo.go declares ("" = the version of the release)
	legacy    bool   // a Python-era tree: the version is in src/truesense/__init__.py, not in buildinfo.go
	noInstall bool   // no tools/install.sh: not a TrueSense release
	noVersion bool   // neither version file
}

// bundle builds truesense-<version>.tar.gz the way the release pipeline lays it out (one top directory).
func bundle(t *testing.T, version string, l layout) []byte {
	t.Helper()
	inside := l.inside
	if inside == "" {
		inside = version
	}
	files := map[string]string{"README.md": "TrueSense\n"}
	if !l.noInstall {
		files["tools/install.sh"] = "#!/bin/sh\n"
	}
	switch {
	case l.noVersion:
	case l.legacy:
		files["src/truesense/__init__.py"] = fmt.Sprintf("__version__ = %q\n", inside)
	default:
		files["internal/buildinfo/buildinfo.go"] = fmt.Sprintf("package buildinfo\n\nconst Version = %q\n", inside)
		files["bin/truesense"] = "binary"
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "truesense-" + version + "/" + name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// files maps a URL path on the fake GitHub to its answer: []byte is a 200 body, int a bare status; a missing path is a 404.
type files map[string]any

// fakeGitHub serves files the way GitHub's CDN does: explicit length, closed connection.
func fakeGitHub(t *testing.T, f files) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		switch v := f[r.URL.Path].(type) {
		case nil:
			http.NotFound(w, r)
		case int:
			w.WriteHeader(v)
		case []byte:
			_, _ = w.Write(v)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const base = "/Order52/truesense/releases/download/"

// sums selects what the release's SHA256SUMS holds.
type sums struct {
	text   string // "" with auto: the right line
	auto   bool
	status int // a bare status instead of a list
	none   bool
}

var (
	rightSums = sums{auto: true}
	noSums    = sums{none: true}
)

// release is what a release of version has on the fake GitHub.
func release(t *testing.T, version string, s sums, l layout, withAsset, withArchive bool) files {
	t.Helper()
	blob := bundle(t, version, l)
	f := files{}
	if withAsset {
		f[base+"v"+version+"/truesense-"+version+".tar.gz"] = blob
	}
	switch {
	case s.none:
	case s.status != 0:
		f[base+"v"+version+"/SHA256SUMS"] = s.status
	case s.auto:
		h := sha256.Sum256(blob)
		f[base+"v"+version+"/SHA256SUMS"] = []byte(hex.EncodeToString(h[:]) + "  truesense-" + version + ".tar.gz\n")
	default:
		f[base+"v"+version+"/SHA256SUMS"] = []byte(s.text)
	}
	if withArchive {
		f["/Order52/truesense/archive/refs/tags/v"+version+".tar.gz"] = blob
	}
	return f
}

func merge(a, b files) files {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// runGet runs tools/get.sh against the fake GitHub with TRUESENSE_GET_ONLY=1 (verify and unpack, never install).
func runGet(t *testing.T, f files, version string, extra ...string) result {
	t.Helper()
	url := fakeGitHub(t, f)
	env := scriptEnv(append([]string{"TRUESENSE_WEB=" + url, "TRUESENSE_API=" + url, "TRUESENSE_GET_ONLY=1"}, extra...)...)
	if version != "" {
		env = append(env, "TRUESENSE_VERSION="+version)
	}
	return bash(t, env, filepath.Join(repoRoot(t), "tools", "get.sh"))
}

func contains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q: %q", what, w, got)
		}
	}
}

func TestAReleaseWithAMatchingListIsVerifiedAndUnpacked(t *testing.T) {
	t.Parallel()
	r := runGet(t, release(t, "0.4.0", rightSums, layout{}, true, false), "0.4.0")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	contains(t, "stdout", r.stdout, "TrueSense 0.4.0: downloading", "checksum verified", "unpacked")
}

func TestAPythonEraReleaseWithAListStillWorks(t *testing.T) {
	t.Parallel()
	r := runGet(t, release(t, "0.3.16", rightSums, layout{legacy: true}, true, false), "0.3.16")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	contains(t, "stdout", r.stdout, "checksum verified", "unpacked")
}

func TestADownloadThatDoesNotMatchItsListIsRefused(t *testing.T) {
	t.Parallel()
	r := runGet(t, release(t, "0.4.0", sums{text: strings.Repeat("0", 64) + "  truesense-0.4.0.tar.gz\n"}, layout{}, true, false), "0.4.0")
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "checksum mismatch", "refusing to install")
}

func TestAListThatDoesNotNameTheFileIsRefused(t *testing.T) {
	t.Parallel()
	r := runGet(t, release(t, "0.4.0", sums{text: strings.Repeat("a", 64) + "  other.tar.gz\n"}, layout{}, true, false), "0.4.0")
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "does not list")
}

func TestABinaryModeMarkerInTheListIsAccepted(t *testing.T) {
	t.Parallel()
	f := release(t, "0.4.0", rightSums, layout{}, true, false)
	key := base + "v0.4.0/SHA256SUMS"
	f[key] = []byte(strings.Replace(string(f[key].([]byte)), "  truesense-", " *truesense-", 1)) // `sha256sum -b` writes "hash *name"
	if r := runGet(t, f, "0.4.0"); r.code != 0 {
		t.Errorf("exit %d: %s", r.code, r.stderr)
	}
}

func TestANewReleaseWithoutAListIsRefusedNotSkipped(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"0.3.16", "0.3.17", "0.4.0", "1.0.0", "0.10.0"} {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			r := runGet(t, release(t, v, noSums, layout{}, true, true), v)
			if r.code != 1 {
				t.Errorf("exit %d, want 1", r.code)
			}
			contains(t, "stderr", r.stderr, "no SHA256SUMS")
		})
	}
}

func TestAnOldReleaseWithoutAListStillInstallsAndSaysSo(t *testing.T) {
	t.Parallel()
	for name, l := range map[string]layout{"python tree": {legacy: true}, "go tree": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := runGet(t, release(t, "0.3.15", noSums, l, false, true), "0.3.15")
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			contains(t, "stdout", r.stdout, "predates published checksums")
		})
	}
}

func TestAServerErrorOnTheListIsAnErrorNotAFallback(t *testing.T) {
	t.Parallel()
	r := runGet(t, release(t, "0.3.15", sums{status: 500}, layout{legacy: true}, false, true), "0.3.15")
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "could not fetch the release checksums", "HTTP 500")
}

func TestABundleThatSaysAnotherVersionIsRefused(t *testing.T) {
	t.Parallel()
	for name, l := range map[string]layout{
		"go tree":     {inside: "0.3.15"},
		"python tree": {inside: "0.3.15", legacy: true},
		"no version":  {noVersion: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := runGet(t, release(t, "0.4.0", rightSums, l, true, false), "0.4.0")
			if r.code != 1 {
				t.Errorf("exit %d, want 1", r.code)
			}
			contains(t, "stderr", r.stderr, "the download says version", "not 0.4.0", "refusing")
		})
	}
}

func TestADownloadThatIsNotATrueSenseReleaseIsRefused(t *testing.T) {
	t.Parallel()
	r := runGet(t, release(t, "0.4.0", rightSums, layout{noInstall: true}, true, false), "0.4.0")
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "not a TrueSense release")
}

func TestAPinnedHashMustAlsoMatch(t *testing.T) {
	t.Parallel()
	f := release(t, "0.4.0", rightSums, layout{}, true, false)
	if r := runGet(t, f, "0.4.0", "TRUESENSE_SHA256="+strings.Repeat("f", 64)); r.code == 0 {
		t.Errorf("a wrong pinned hash was accepted: %s", r.stdout)
	}
	blob := f[base+"v0.4.0/truesense-0.4.0.tar.gz"].([]byte)
	h := sha256.Sum256(blob)
	if r := runGet(t, f, "0.4.0", "TRUESENSE_SHA256="+hex.EncodeToString(h[:])); r.code != 0 {
		t.Errorf("the right pinned hash was refused: %s", r.stderr)
	}
}

func TestTheNewestVersionIsFoundFromTheTagsWhenNoneIsPinned(t *testing.T) {
	t.Parallel()
	// GitHub pretty-prints the tag list: one name per line; junk and older tags must not win, and 0.10.0 beats 0.9.0 (version order, not text order)
	tags, err := json.MarshalIndent([]map[string]string{{"name": "v0.4.0"}, {"name": "v0.9.0"}, {"name": "v0.3.15"}, {"name": "junk"}, {"name": "v0.10.0"}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f := merge(release(t, "0.10.0", rightSums, layout{}, true, false), files{"/repos/Order52/truesense/tags": tags})
	r := runGet(t, f, "")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	contains(t, "stdout", r.stdout, "TrueSense 0.10.0")
}

func TestAnUnreachableTagListSaysHowToPinAVersion(t *testing.T) {
	t.Parallel()
	r := runGet(t, files{"/repos/Order52/truesense/tags": []byte("[]")}, "")
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "could not find the latest version", "TRUESENSE_VERSION")
}

func TestAMalformedVersionIsRefusedBeforeAnythingIsFetched(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"1.0; rm -rf /", "1.0", "v1.0.0", "1.0.0-rc1", "$(id)"} {
		r := runGet(t, files{}, v)
		if r.code != 1 {
			t.Errorf("%q: exit %d", v, r.code)
		}
		contains(t, v, r.stderr, "not a version")
	}
}

func TestItInsistsOnRootUnlessToldOtherwise(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("running as root: the refusal cannot be observed")
	}
	// no TRUESENSE_GET_ONLY: the refusal comes before anything is fetched or run
	env := scriptEnv()
	r := bash(t, env, filepath.Join(repoRoot(t), "tools", "get.sh"))
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "run as root")
}

func TestItNeedsCurlTarAndSha256sum(t *testing.T) {
	t.Parallel()
	only := pathWith(t, "id") // none of the three
	r := bash(t, scriptEnv("PATH="+only, "TRUESENSE_GET_ONLY=1"), filepath.Join(repoRoot(t), "tools", "get.sh"))
	if r.code != 1 {
		t.Errorf("exit %d", r.code)
	}
	contains(t, "stderr", r.stderr, "curl is required")
}

// ---- the build provenance check (TRUESENSE_VERIFY_ATTESTATION=1) ----------------------------------------------------------------------

func TestAttestationVerificationUsesTheGitHubCLI(t *testing.T) {
	t.Parallel()
	f := release(t, "0.4.0", rightSums, layout{}, true, false)
	system := os.Getenv("PATH")

	good, log := t.TempDir(), filepath.Join(t.TempDir(), "gh.log")
	fakeProgram(t, good, "gh", `echo "$*" >> "`+log+`"`)
	r := runGet(t, f, "0.4.0", "TRUESENSE_VERIFY_ATTESTATION=1", "PATH="+good+":"+system)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	contains(t, "stdout", r.stdout, "build provenance verified")
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "gh arguments", string(args), "attestation verify", "ts.tar.gz", "--repo Order52/truesense")

	bad := t.TempDir()
	fakeProgram(t, bad, "gh", "exit 1")
	r = runGet(t, f, "0.4.0", "TRUESENSE_VERIFY_ATTESTATION=1", "PATH="+bad+":"+system)
	if r.code != 1 {
		t.Errorf("a failing attestation was accepted (exit %d)", r.code)
	}
	contains(t, "stderr", r.stderr, "does not verify", "refusing")

	// without gh at all: curl, tar and the coreutils the script uses, but no gh
	noGH := pathWith(t, "curl", "tar", "sha256sum", "sed", "awk", "sort", "head", "tail", "mktemp", "rm", "id", "tr", "cut", "cat")
	r = runGet(t, f, "0.4.0", "TRUESENSE_VERIFY_ATTESTATION=1", "PATH="+noGH)
	if r.code != 1 {
		t.Errorf("exit %d without gh", r.code)
	}
	contains(t, "stderr", r.stderr, "needs the GitHub CLI")
}
