package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractInto unpacks blob into <parent>/dest and returns both, so tests can prove nothing
// escaped into parent.
func extractInto(t testing.TB, blob []byte, lim ExtractLimits) (parent, dest string, err error) {
	t.Helper()
	parent = t.TempDir()
	dest = filepath.Join(parent, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	return parent, dest, ExtractTarGz(bytes.NewReader(blob), dest, lim)
}

// onlyDest fails the test if anything exists next to dest, and returns every path inside it.
func onlyDest(t testing.TB, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "dest" {
		t.Fatalf("something was written outside the destination: %v", entries)
	}
	var paths []string
	err = filepath.WalkDir(filepath.Join(parent, "dest"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			t.Errorf("a symlink was created: %s", p)
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			t.Errorf("a special file was created: %s (%v)", p, d.Type())
		}
		rel, _ := filepath.Rel(filepath.Join(parent, "dest"), p)
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestExtractionOfAnOrdinaryArchive(t *testing.T) {
	blob := archive(t,
		entry{name: "pkg/", typeflag: tar.TypeDir, mode: 0o700},
		entry{name: "./pkg/bin/run.sh", body: "echo", mode: 0o4755}, // setuid must not survive
		entry{name: "pkg/data.txt", body: "hello", mode: 0o600},
		entry{name: "pkg/sub/deep/file", body: ""},
		entry{name: "./", typeflag: tar.TypeDir},
	)
	parent, dest, err := extractInto(t, blob, DefaultExtractLimits)
	if err != nil {
		t.Fatal(err)
	}
	onlyDest(t, parent)
	check := func(rel string, perm os.FileMode, body string) {
		st, err := os.Lstat(filepath.Join(dest, rel))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(dest, rel))
		if st.Mode() != perm || string(got) != body {
			t.Errorf("%s: mode %v body %q, want %v %q", rel, st.Mode(), got, perm, body)
		}
	}
	check("pkg/bin/run.sh", 0o755, "echo")
	check("pkg/data.txt", 0o644, "hello")
	check("pkg/sub/deep/file", 0o644, "")
	check("pkg", fs.ModeDir|0o755, "")
}

func TestGitHubArchivesStartWithAPaxGlobalHeaderWhichIsMetadata(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "0123456789abcdef"}}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "r/f", Typeflag: tar.TypeReg, Mode: 0o644, Size: 2}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("ok"))
	_ = tw.Close()
	_ = gz.Close()
	parent, _, err := extractInto(t, buf.Bytes(), DefaultExtractLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got := onlyDest(t, parent); len(got) != 3 { // ".", "r", "r/f": no file called pax_global_header
		t.Fatalf("paths = %v", got)
	}
}

func TestMaliciousArchivesAreRefusedAndWriteNothingOutside(t *testing.T) {
	cases := []struct {
		name    string
		entries []entry
		reason  string
	}{
		{"parent traversal", []entry{{name: "../evil", body: "x"}}, "unsafe path"},
		{"nested traversal", []entry{{name: "a/b/../../../evil", body: "x"}}, "unsafe path"},
		{"traversal behind a dot", []entry{{name: "./../evil", body: "x"}}, "unsafe path"},
		{"traversal as a directory", []entry{{name: "../evildir/", typeflag: tar.TypeDir}}, "unsafe path"},
		{"traversal only in the middle", []entry{{name: "ok/", typeflag: tar.TypeDir}, {name: "ok/../../evil", body: "x"}}, "unsafe path"},
		{"absolute path", []entry{{name: "/tmp/evil", body: "x"}}, "unsafe path"},
		{"absolute directory", []entry{{name: "/etc/", typeflag: tar.TypeDir}}, "unsafe path"},
		{"symlink out", []entry{{name: "link", typeflag: tar.TypeSymlink, link: "../../etc"}}, "unsafe link"},
		{"symlink to an absolute path", []entry{{name: "link", typeflag: tar.TypeSymlink, link: "/etc/passwd"}}, "unsafe link"},
		{"harmless-looking symlink inside the tree", []entry{{name: "a", body: "x"}, {name: "b", typeflag: tar.TypeSymlink, link: "a"}}, "unsafe link"},
		{"symlink then write through it", []entry{{name: "d", typeflag: tar.TypeSymlink, link: "/tmp"}, {name: "d/evil", body: "x"}}, "unsafe link"},
		{"hard link", []entry{{name: "a", body: "x"}, {name: "h", typeflag: tar.TypeLink, link: "a"}}, "unsafe link"},
		{"hard link to a system file", []entry{{name: "h", typeflag: tar.TypeLink, link: "/etc/passwd"}}, "unsafe link"},
		{"char device", []entry{{name: "null", typeflag: tar.TypeChar}}, "device file"},
		{"block device", []entry{{name: "sda", typeflag: tar.TypeBlock}}, "device file"},
		{"fifo", []entry{{name: "pipe", typeflag: tar.TypeFifo}}, "device file"},
		{"empty name", []entry{{name: "", body: "x"}}, "unsafe path"},
		{"unsupported entry type", []entry{{name: "x", typeflag: tar.TypeCont}}, "unsupported entry type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent, _, err := extractInto(t, archive(t, tc.entries...), DefaultExtractLimits)
			var ua *UnsafeArchiveError
			if !errors.As(err, &ua) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err = %v, want an UnsafeArchiveError containing %q", err, tc.reason)
			}
			onlyDest(t, parent)
		})
	}
}

// rawArchive writes tar headers by hand: archive/tar's writer refuses to produce several of
// the hostile names a real attacker's tool can.
func rawArchive(name string, typeflag byte, body string) []byte {
	hdr := make([]byte, 512)
	copy(hdr[0:100], name)
	copy(hdr[100:108], "0000644\x00")
	copy(hdr[108:116], "0000000\x00")
	copy(hdr[116:124], "0000000\x00")
	copy(hdr[124:136], fmt.Sprintf("%011o\x00", len(body)))
	copy(hdr[136:148], "00000000000\x00")
	hdr[156] = typeflag
	copy(hdr[257:263], "ustar\x00")
	copy(hdr[263:265], "00")
	copy(hdr[148:156], "        ")
	sum := 0
	for _, b := range hdr {
		sum += int(b)
	}
	copy(hdr[148:156], fmt.Sprintf("%06o\x00 ", sum))
	out := append([]byte(nil), hdr...)
	if body != "" {
		blk := make([]byte, (len(body)+511)/512*512)
		copy(blk, body)
		out = append(out, blk...)
	}
	return append(out, make([]byte, 1024)...)
}

func gzipped(raw []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(raw)
	_ = gz.Close()
	return buf.Bytes()
}

func TestHostileHeadersTheTarWriterCannotMake(t *testing.T) {
	for name, raw := range map[string][]byte{
		"a regular file called ./":    rawArchive("./", '0', "x"),
		"traversal as a raw name":     rawArchive("../../evil", '0', "x"),
		"a raw absolute name":         rawArchive("/etc/cron.d/evil", '0', "x"),
		"a link with an empty target": rawArchive("l", '2', ""),
		"GNU sparse entry":            rawArchive("s", 'S', ""),
	} {
		t.Run(name, func(t *testing.T) {
			parent, _, err := extractInto(t, gzipped(raw), DefaultExtractLimits)
			if err == nil {
				t.Fatal("accepted")
			}
			onlyDest(t, parent)
		})
	}
	// control: the helper itself builds a valid archive
	parent, dest, err := extractInto(t, gzipped(rawArchive("ok/file", '0', "hi")), DefaultExtractLimits)
	if err != nil {
		t.Fatalf("the raw archive helper is broken: %v", err)
	}
	onlyDest(t, parent)
	if b, _ := os.ReadFile(filepath.Join(dest, "ok", "file")); string(b) != "hi" {
		t.Fatalf("content %q", b)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{"a": "a", "./a/./b": "a/b", "a//b": "a/b", "./": "", ".": "", "a/b/": "a/b"} {
		if got, err := cleanName(in); err != nil || got != want {
			t.Errorf("cleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "/", "/a", "..", "a/..", "a/../b", "../a", "a\x00b", strings.Repeat("a", 1025)} {
		if _, err := cleanName(in); err == nil {
			t.Errorf("cleanName(%q) accepted", in)
		}
	}
}

func TestAnEntryBeforeTheMaliciousOneMayBeWrittenButNothingOutsideIs(t *testing.T) {
	parent, dest, err := extractInto(t, archive(t, entry{name: "good", body: "x"}, entry{name: "../evil", body: "x"}), DefaultExtractLimits)
	if err == nil {
		t.Fatal("accepted")
	}
	onlyDest(t, parent)
	if _, err := os.Stat(filepath.Join(dest, "good")); err != nil {
		t.Log("good was not written (also fine)")
	}
}

func TestADuplicateNeverOverwritesAndAFileNeverBecomesADirectory(t *testing.T) {
	_, dest, err := extractInto(t, archive(t, entry{name: "f", body: "first"}, entry{name: "f", body: "second"}), DefaultExtractLimits)
	if err == nil {
		t.Fatal("a duplicate was accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "f")); string(b) != "first" {
		t.Errorf("the first file was overwritten: %q", b)
	}
	if _, _, err := extractInto(t, archive(t, entry{name: "f", body: "x"}, entry{name: "f/child", body: "y"}), DefaultExtractLimits); err == nil {
		t.Fatal("a file was used as a directory")
	}
	if _, _, err := extractInto(t, archive(t, entry{name: "d/", typeflag: tar.TypeDir}, entry{name: "d", body: "y"}), DefaultExtractLimits); err == nil {
		t.Fatal("a directory was replaced by a file")
	}
}

func TestExtractionIsBounded(t *testing.T) {
	big := entry{name: "big", body: strings.Repeat("A", 1<<16)}
	if _, _, err := extractInto(t, archive(t, big, entry{name: "big2", body: strings.Repeat("A", 1<<16)}), ExtractLimits{MaxFiles: 10, MaxBytes: 100_000}); !contains(err, "unexpectedly large") {
		t.Errorf("size: %v", err)
	}
	var many []entry
	for i := 0; i < 20; i++ {
		many = append(many, entry{name: "f" + strings.Repeat("x", i), body: "x"})
	}
	if _, _, err := extractInto(t, archive(t, many...), ExtractLimits{MaxFiles: 5, MaxBytes: 1 << 20}); !contains(err, "too many entries") {
		t.Errorf("count: %v", err)
	}
	if _, _, err := extractInto(t, archive(t, entry{name: strings.Repeat("a/", 600) + "f", body: "x"}), DefaultExtractLimits); !contains(err, "unsafe path") {
		t.Errorf("name length: %v", err)
	}
}

func TestAnArchiveThatLiesAboutItsSizeIsRefused(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "f", Typeflag: tar.TypeReg, Mode: 0o644, Size: 10})
	_, _ = tw.Write([]byte("0123456789"))
	_ = tw.Close()
	_ = gz.Close()
	truncated := buf.Bytes()
	for _, n := range []int{len(truncated) - 1, len(truncated) / 2, 5, 0} {
		if _, _, err := extractInto(t, truncated[:n], DefaultExtractLimits); err == nil {
			t.Errorf("a download cut to %d bytes was accepted", n)
		}
	}
	if _, _, err := extractInto(t, []byte("this is not gzip"), DefaultExtractLimits); !contains(err, "not a gzip archive") {
		t.Errorf("plain text: %v", err)
	}
}

func FuzzExtract(f *testing.F) {
	// The fuzzer mutates the TAR stream; the test wraps it in gzip so mutations are not
	// rejected by the compression checksum before the extractor sees them.
	seed := func(entries ...entry) {
		var raw bytes.Buffer
		tw := tar.NewWriter(&raw)
		for _, e := range entries {
			tf := e.typeflag
			if tf == 0 {
				tf = tar.TypeReg
			}
			h := &tar.Header{Name: e.name, Typeflag: tf, Mode: 0o644, Linkname: e.link}
			if tf == tar.TypeReg {
				h.Size = int64(len(e.body))
			}
			_ = tw.WriteHeader(h)
			if tf == tar.TypeReg {
				_, _ = tw.Write([]byte(e.body))
			}
		}
		_ = tw.Close()
		f.Add(raw.Bytes())
	}
	seed(entry{name: "r/", typeflag: tar.TypeDir}, entry{name: "r/f", body: "hello"})
	seed(entry{name: "../evil", body: "x"})
	seed(entry{name: "/abs", body: "x"})
	seed(entry{name: "l", typeflag: tar.TypeSymlink, link: "/etc"}, entry{name: "l/x", body: "y"})
	seed(entry{name: "a/../../b", body: "x"})
	f.Fuzz(func(t *testing.T, tarBytes []byte) {
		var gzBuf bytes.Buffer
		gz := gzip.NewWriter(&gzBuf)
		_, _ = gz.Write(tarBytes)
		_ = gz.Close()
		parent := t.TempDir()
		dest := filepath.Join(parent, "dest")
		if err := os.Mkdir(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = ExtractTarGz(bytes.NewReader(gzBuf.Bytes()), dest, ExtractLimits{MaxFiles: 100, MaxBytes: 1 << 20})
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 1 {
			t.Fatalf("something was written outside dest: %v %v", entries, err)
		}
		err = filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && !d.Type().IsRegular() {
				t.Fatalf("special file %s (%v)", p, d.Type())
			}
			if info, err := d.Info(); err == nil && info.Mode().Perm()&^0o755 != 0 {
				t.Fatalf("%s has mode %v", p, info.Mode())
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
