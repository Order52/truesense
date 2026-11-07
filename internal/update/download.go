package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SumsRequiredFrom is the first release built by the pipeline that publishes SHA256SUMS. From
// this version on a missing list is refused, never skipped: otherwise anyone able to delete
// the file (or to answer 404 for it) could turn verification off.
var SumsRequiredFrom = Version{0, 3, 16}

// Layout says what a release archive must contain to count as a TrueSense release, and where
// the version it claims to be is written, so a tag cannot be paired with another version's code.
type Layout struct {
	Required       []string       // files that must exist under the archive's single top folder
	VersionFile    string         // the file that states the version
	VersionPattern *regexp.Regexp // one capture group: the version text
}

// DefaultLayout is the 0.3.x release layout.
func DefaultLayout() Layout {
	return Layout{
		Required:       []string{"tools/install.sh", "src/truesense/__init__.py", "kernel/truesense-wmi/truesense_wmi.c"},
		VersionFile:    "src/truesense/__init__.py",
		VersionPattern: regexp.MustCompile(`__version__\s*=\s*"([^"]+)"`),
	}
}

// Release is a verified, unpacked release in a private temporary folder.
type Release struct {
	Root     string // the unpacked top folder; Root/tools/install.sh is the installer
	SHA256   string
	Verified bool // matched the release's SHA256SUMS (false only for an old release that had none)
	workDir  string
}

// Cleanup removes the download and everything unpacked from it.
func (r *Release) Cleanup() { _ = os.RemoveAll(r.workDir) }

// Installer is the path of the release's installer script.
func (r *Release) Installer() string { return filepath.Join(r.Root, "tools", "install.sh") }

func (u *Updater) readSmall(ctx context.Context, url string, limit int64) ([]byte, int, error) {
	resp, cancel, err := u.get(ctx, url, "", 20*time.Second)
	if err != nil {
		return nil, 0, err
	}
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, resp.StatusCode, err
}

// Checksums fetches the release's SHA256SUMS as {file name: sha256}. It returns nil (and no
// error) only when the release has no list at all (HTTP 404, an old release). Every other
// failure is an error: a missing list must never silently turn verification off.
func (u *Updater) Checksums(ctx context.Context, tag string) (map[string]string, error) {
	data, code, err := u.readSmall(ctx, fmt.Sprintf("%s/%s/releases/download/%s/SHA256SUMS", u.WebBase, u.Repo, tag), 1<<20)
	switch {
	case err != nil:
		return nil, fmt.Errorf("could not fetch the release checksums (%w); try again later", err)
	case code == http.StatusNotFound:
		return nil, nil
	case code != http.StatusOK:
		return nil, fmt.Errorf("could not fetch the release checksums (HTTP %d); try again later", code)
	}
	return ParseSums(data)
}

// DownloadOptions tune Download.
type DownloadOptions struct {
	// PinSHA256, if set, must also match the archive (the TRUESENSE_SHA256 of tools/get.sh).
	PinSHA256 string
}

// Download fetches and unpacks a tagged release into a fresh private folder.
//
// A release made by the release pipeline is downloaded as its published asset and must match
// its SHA256SUMS (Verified = true); an old release without one is fetched as GitHub's tag
// archive (Verified = false). Either way the version inside has to match the tag. On any
// failure nothing is left behind.
func (u *Updater) Download(ctx context.Context, tag string, opts DownloadOptions) (rel *Release, err error) {
	want, ok := ParseVersion(tag)
	if !ok {
		return nil, fmt.Errorf("not a version tag: %q", tag)
	}
	asset := fmt.Sprintf("truesense-%s.tar.gz", want)
	sums, err := u.Checksums(ctx, tag)
	if err != nil {
		return nil, err
	}
	var expected, url string
	if sums == nil {
		if want.AtLeast(SumsRequiredFrom) {
			return nil, fmt.Errorf("release %s has no SHA256SUMS: refusing to install something that cannot be verified", tag)
		}
		url = fmt.Sprintf("%s/%s/archive/refs/tags/%s.tar.gz", u.WebBase, u.Repo, tag)
	} else {
		if expected = sums[asset]; expected == "" {
			return nil, fmt.Errorf("SHA256SUMS of %s does not list %s: refusing", tag, asset)
		}
		url = fmt.Sprintf("%s/%s/releases/download/%s/%s", u.WebBase, u.Repo, tag, asset)
	}
	work, err := os.MkdirTemp(u.TempDir, "truesense-update-")
	if err != nil {
		return nil, fmt.Errorf("make a download folder: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(work)
		}
	}()
	if err = os.Chmod(work, 0o700); err != nil {
		return nil, err
	}
	archive := filepath.Join(work, "release.tar.gz")
	sum, err := u.fetchFile(ctx, url, archive)
	if err != nil {
		return nil, err
	}
	if expected != "" && sum != expected {
		return nil, fmt.Errorf("checksum mismatch for %s (expected %s…, got %s…): refusing", asset, short(expected), short(sum))
	}
	if pin := strings.ToLower(opts.PinSHA256); pin != "" && sum != pin {
		return nil, fmt.Errorf("checksum mismatch for %s (pinned %s…, got %s…): refusing", asset, short(pin), short(sum))
	}
	src := filepath.Join(work, "src")
	if err = os.Mkdir(src, 0o700); err != nil {
		return nil, err
	}
	if err = u.unpack(archive, src); err != nil {
		return nil, err
	}
	root, err := u.validate(src, want, tag)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(filepath.Join(root, "tools", "install.sh"), 0o755); err != nil { // #nosec G302 -- the installer must be executable
		return nil, err
	}
	return &Release{Root: root, SHA256: sum, Verified: expected != "", workDir: work}, nil
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// fetchFile streams url into path, hashing as it goes, and refuses a download over u.MaxDownload.
func (u *Updater) fetchFile(ctx context.Context, url, path string) (string, error) {
	resp, cancel, err := u.get(ctx, url, "", 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %w", url, &HTTPError{Code: resp.StatusCode, URL: url})
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, u.MaxDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return "", fmt.Errorf("download %s: %w", url, err)
	case n > u.MaxDownload:
		return "", errors.New("the download is unexpectedly large; refusing")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (u *Updater) unpack(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return ExtractTarGz(f, dest, DefaultExtractLimits)
}

// validate checks that dest holds exactly one top folder that is a TrueSense release of the
// version the tag says, and returns that folder.
func (u *Updater) validate(dest string, want Version, tag string) (string, error) {
	entries, err := os.ReadDir(dest)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return "", errors.New("unexpected layout in the download")
	}
	root := filepath.Join(dest, entries[0].Name())
	for _, need := range u.Layout.Required {
		if st, err := os.Stat(filepath.Join(root, need)); err != nil || !st.Mode().IsRegular() {
			return "", fmt.Errorf("the download is not a TrueSense release (missing %s)", need)
		}
	}
	text, err := os.ReadFile(filepath.Join(root, u.Layout.VersionFile))
	if err != nil {
		return "", fmt.Errorf("the download is not a TrueSense release (missing %s)", u.Layout.VersionFile)
	}
	inside := "?"
	if m := u.Layout.VersionPattern.FindSubmatch(text); m != nil {
		inside = string(m[1])
	}
	if got, ok := ParseVersion(inside); !ok || got != want {
		return "", fmt.Errorf("the download says version %s, not %s; refusing", inside, tag)
	}
	return root, nil
}
