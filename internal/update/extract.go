package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractLimits bound what an archive may unpack to, so a hostile or corrupt download cannot
// fill the disk (a "zip bomb": a few kilobytes that expand to gigabytes).
type ExtractLimits struct {
	MaxFiles int   // entries of any kind
	MaxBytes int64 // total uncompressed size of the regular files
}

// DefaultExtractLimits are generous for a release (a few hundred files, a few MB) and far
// below what could hurt.
var DefaultExtractLimits = ExtractLimits{MaxFiles: 20000, MaxBytes: 400 << 20}

// UnsafeArchiveError says why an archive was refused. The messages keep the 0.3.x wording.
type UnsafeArchiveError struct{ Reason string }

func (e *UnsafeArchiveError) Error() string { return e.Reason }

func unsafe(format string, args ...any) error {
	return &UnsafeArchiveError{Reason: fmt.Sprintf(format, args...)}
}

// ExtractTarGz unpacks a .tar.gz into dest, which must already exist and should be empty.
//
// It is strict by construction rather than by checking what could go wrong afterwards: only
// regular files and directories are ever created. Symbolic links, hard links, device nodes
// and FIFOs are refused outright, so no later entry can be redirected through a link; every
// path is made relative and cleaned, and a ".." component or an absolute name refuses the
// whole archive; files are created with O_EXCL (never overwriting, never following a link);
// modes are reduced to 0644/0755 (no setuid, no sticky, no ownership). Nothing is written
// before an entry has passed all checks, and on any error the caller is expected to delete
// dest (Download does).
func ExtractTarGz(r io.Reader, dest string, lim ExtractLimits) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("the download is not a gzip archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	if err := extractTar(tar.NewReader(gz), dest, lim); err != nil {
		return err
	}
	// The tar reader stops at the end-of-archive marker, before the gzip trailer: read on, so a
	// download cut short or corrupted there (bad checksum) is refused, not half-trusted.
	if _, err := io.Copy(io.Discard, io.LimitReader(gz, lim.MaxBytes)); err != nil {
		return fmt.Errorf("the download is not a valid archive: %w", err)
	}
	return nil
}

func extractTar(tr *tar.Reader, dest string, lim ExtractLimits) error {
	var entries int
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the download is not a valid archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader { // GitHub's archives start with one (the commit id): metadata, not a file
			continue
		}
		if entries++; entries > lim.MaxFiles {
			return unsafe("the download has too many entries; refusing")
		}
		rel, err := cleanName(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if rel == "" {
				continue
			}
			if err := os.MkdirAll(filepath.Join(dest, rel), 0o755); err != nil {
				return fmt.Errorf("unpack %s: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if rel == "" {
				return unsafe("the download contains an unsafe path (%s); refusing", hdr.Name)
			}
			if total += hdr.Size; hdr.Size < 0 || total > lim.MaxBytes {
				return unsafe("the download is unexpectedly large; refusing")
			}
			if err := writeFile(tr, filepath.Join(dest, rel), hdr); err != nil {
				return fmt.Errorf("unpack %s: %w", hdr.Name, err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return unsafe("the download contains an unsafe link (%s); refusing", hdr.Name)
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return unsafe("the download contains a device file; refusing")
		default:
			return unsafe("the download contains an unsupported entry type %q (%s); refusing", string(hdr.Typeflag), hdr.Name)
		}
	}
}

// cleanName turns an archive member name into a relative slash-free-of-surprises path, or
// refuses it. "" means the archive's root directory itself ("./").
func cleanName(name string) (string, error) {
	if name == "" || len(name) > 1024 || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", unsafe("the download contains an unsafe path (%s); refusing", printable(name))
	}
	var parts []string
	for _, p := range strings.Split(name, "/") {
		switch p {
		case "", ".":
		case "..":
			return "", unsafe("the download contains an unsafe path (%s); refusing", printable(name))
		default:
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/"), nil
}

func printable(s string) string {
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

func writeFile(src io.Reader, path string, hdr *tar.Header) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if hdr.Mode&0o100 != 0 {
		perm = 0o755
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) // #nosec G304 -- path is dest + a cleaned relative name
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, src, hdr.Size); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil { // the umask may have masked the executable bit
		_ = f.Close()
		return err
	}
	return f.Close()
}
