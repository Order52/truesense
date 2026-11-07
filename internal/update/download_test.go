package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAReleaseWithAMatchingListIsVerifiedAndUnpacked(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.16", releaseOpts{sums: "auto"}))
	u := updaterFor(t, g)
	rel, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rel.Verified || len(rel.SHA256) != 64 {
		t.Fatalf("release = %+v", rel)
	}
	st, err := os.Stat(rel.Installer())
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("installer = %v, %v (must be executable)", st, err)
	}
	if filepath.Base(rel.Root) != "truesense-0.3.16" {
		t.Errorf("root = %s", rel.Root)
	}
	rel.Cleanup()
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("Cleanup left %v", left)
	}
}

func TestADownloadThatDoesNotMatchItsListIsRefusedAndLeavesNothing(t *testing.T) {
	cases := map[string]releaseOpts{
		"tampered bytes":       {sums: "auto", tamper: true},
		"wrong hash in list":   {sums: strings.Repeat("0", 64) + "  truesense-0.3.16.tar.gz\n"},
		"uppercase wrong hash": {sums: strings.Repeat("A", 64) + " *truesense-0.3.16.tar.gz\n"},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			g := newFakeGitHub(t, release(t, "0.3.16", o))
			u := updaterFor(t, g)
			_, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{})
			if !contains(err, "checksum mismatch", "refusing") {
				t.Fatalf("err = %v", err)
			}
			if left := leftovers(t, u); len(left) != 0 {
				t.Errorf("a refused download left %v behind", left)
			}
		})
	}
}

func TestAListThatDoesNotNameTheFileIsRefused(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.16", releaseOpts{sums: strings.Repeat("a", 64) + "  other.tar.gz\n"}))
	u := updaterFor(t, g)
	_, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{})
	if !contains(err, "does not list", "truesense-0.3.16.tar.gz") {
		t.Fatalf("err = %v", err)
	}
	if g.asked(repoPath + "/releases/download/v0.3.16/truesense-0.3.16.tar.gz") {
		t.Error("the asset was downloaded although the list did not vouch for it")
	}
}

func TestANewReleaseWithoutAListIsRefusedNotSkipped(t *testing.T) {
	for _, version := range []string{"0.3.16", "0.3.17", "1.0.0"} {
		g := newFakeGitHub(t, release(t, version, releaseOpts{sums: nil, archive: true}))
		u := updaterFor(t, g)
		_, err := u.Download(context.Background(), "v"+version, DownloadOptions{})
		if !contains(err, "no SHA256SUMS", "refusing") {
			t.Errorf("%s: err = %v", version, err)
		}
		if g.asked(repoPath+"/archive/refs/tags/v"+version+".tar.gz") || g.asked(repoPath+"/releases/download/v"+version+"/truesense-"+version+".tar.gz") {
			t.Errorf("%s: something unverifiable was downloaded", version)
		}
	}
}

func TestAnOldReleaseWithoutAListStillInstallsFromTheTagArchive(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.15", releaseOpts{sums: nil, noAsset: true, archive: true}))
	u := updaterFor(t, g)
	rel, err := u.Download(context.Background(), "v0.3.15", DownloadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer rel.Cleanup()
	if rel.Verified {
		t.Error("an unverifiable old release must not be reported as verified")
	}
	if !g.asked(repoPath + "/archive/refs/tags/v0.3.15.tar.gz") {
		t.Error("the tag archive was not used")
	}
}

func TestAServerErrorOnTheListIsAnErrorNotAFallback(t *testing.T) {
	for _, code := range []int{500, 403, 429} {
		g := newFakeGitHub(t, release(t, "0.3.15", releaseOpts{sums: code, archive: true}))
		u := updaterFor(t, g)
		_, err := u.Download(context.Background(), "v0.3.15", DownloadOptions{})
		if !contains(err, "could not fetch the release checksums") {
			t.Errorf("HTTP %d: err = %v", code, err)
		}
		if g.asked(repoPath + "/archive/refs/tags/v0.3.15.tar.gz") {
			t.Errorf("HTTP %d on the list fell back to the unverified archive", code)
		}
	}
}

func TestOfflineNeverSkipsVerification(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.15", releaseOpts{sums: "auto", archive: true}))
	u := updaterFor(t, g)
	g.Close() // the network is gone
	_, err := u.Download(context.Background(), "v0.3.15", DownloadOptions{})
	if !contains(err, "could not fetch the release checksums", "try again later") {
		t.Fatalf("err = %v", err)
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestATarballThatSaysAnotherVersionIsRefused(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.16", releaseOpts{sums: "auto", inside: "0.3.15"}))
	u := updaterFor(t, g)
	_, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{})
	if !contains(err, "says version 0.3.15", "not v0.3.16", "refusing") {
		t.Fatalf("err = %v", err)
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestAPinnedHashMustAlsoMatch(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.16", releaseOpts{sums: "auto"}))
	u := updaterFor(t, g)
	if _, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{PinSHA256: strings.Repeat("f", 64)}); !contains(err, "checksum mismatch", "pinned") {
		t.Fatalf("err = %v", err)
	}
	right := sha(releaseTarball(t, "0.3.16", ""))
	rel, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{PinSHA256: strings.ToUpper(right)})
	if err != nil {
		t.Fatalf("the right pin was refused: %v", err)
	}
	rel.Cleanup()
}

func TestAMalformedTagIsRefusedBeforeAnythingIsFetched(t *testing.T) {
	g := newFakeGitHub(t, nil)
	u := updaterFor(t, g)
	for _, tag := range []string{"1.0; rm -rf /", "", "v1.2", "latest", "../../x", "v1.2.3-rc1"} {
		if _, err := u.Download(context.Background(), tag, DownloadOptions{}); !contains(err, "not a version tag") {
			t.Errorf("%q: err = %v", tag, err)
		}
	}
	if len(g.paths) != 0 {
		t.Errorf("requests were made: %v", g.paths)
	}
}

func TestAnArchiveThatIsNotATrueSenseReleaseIsRefused(t *testing.T) {
	top := "truesense-0.3.16/"
	cases := map[string][]entry{
		"no installer":  {{name: top + "src/truesense/__init__.py", body: `__version__ = "0.3.16"`}, {name: top + "kernel/truesense-wmi/truesense_wmi.c", body: "x"}},
		"two top dirs":  {{name: top + "tools/install.sh", body: "x"}, {name: "other/file", body: "x"}},
		"no version":    {{name: top + "tools/install.sh", body: "x"}, {name: top + "src/truesense/__init__.py", body: "nothing"}, {name: top + "kernel/truesense-wmi/truesense_wmi.c", body: "x"}},
		"a bare file":   {{name: "install.sh", body: "x"}},
		"empty archive": {},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			blob := archive(t, entries...)
			g := newFakeGitHub(t, map[string]any{
				repoPath + "/releases/download/v0.3.16/SHA256SUMS":              sha(blob) + "  truesense-0.3.16.tar.gz\n",
				repoPath + "/releases/download/v0.3.16/truesense-0.3.16.tar.gz": blob,
			})
			u := updaterFor(t, g)
			if _, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{}); err == nil {
				t.Fatal("accepted")
			}
			if left := leftovers(t, u); len(left) != 0 {
				t.Errorf("left %v", left)
			}
		})
	}
}

func TestAnArchiveWithAMaliciousMemberIsRefusedEvenWhenItsHashMatches(t *testing.T) {
	// The list vouches for the bytes, not for their safety: the extractor still refuses.
	top := "truesense-0.3.16/"
	good := []entry{{name: top + "tools/install.sh", body: "x"}, {name: top + "src/truesense/__init__.py", body: `__version__ = "0.3.16"`}, {name: top + "kernel/truesense-wmi/truesense_wmi.c", body: "x"}}
	blob := archive(t, append(good, entry{name: top + "../../evil", body: "pwned"})...)
	g := newFakeGitHub(t, map[string]any{
		repoPath + "/releases/download/v0.3.16/SHA256SUMS":              sha(blob) + "  truesense-0.3.16.tar.gz\n",
		repoPath + "/releases/download/v0.3.16/truesense-0.3.16.tar.gz": blob,
	})
	u := updaterFor(t, g)
	_, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{})
	if !contains(err, "unsafe path") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(u.TempDir, "..", "evil")); statErr == nil {
		t.Fatal("a file was written outside the download folder")
	}
}

func TestAnOversizedDownloadIsRefused(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.16", releaseOpts{sums: "auto"}))
	u := updaterFor(t, g)
	u.MaxDownload = 100
	if _, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{}); !contains(err, "unexpectedly large") {
		t.Fatalf("err = %v", err)
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestADownloadHTTPErrorOnTheAssetIsReported(t *testing.T) {
	files := release(t, "0.3.16", releaseOpts{sums: "auto", noAsset: true})
	files[repoPath+"/releases/download/v0.3.16/truesense-0.3.16.tar.gz"] = 502
	g := newFakeGitHub(t, files)
	u := updaterFor(t, g)
	if _, err := u.Download(context.Background(), "v0.3.16", DownloadOptions{}); !contains(err, "502") {
		t.Fatalf("err = %v", err)
	}
	if left := leftovers(t, u); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestACancelledContextStopsTheDownload(t *testing.T) {
	g := newFakeGitHub(t, release(t, "0.3.16", releaseOpts{sums: "auto"}))
	u := updaterFor(t, g)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.Download(ctx, "v0.3.16", DownloadOptions{}); err == nil {
		t.Fatal("download ran on a cancelled context")
	}
}
