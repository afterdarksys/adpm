package pkgarchive

import (
	"archive/tar"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "hello"), []byte("hello\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "META.json"), []byte(`{"name":"fixture","version":"1.0.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func checkRoundTrip(t *testing.T, archive string) {
	t.Helper()
	out := t.TempDir()
	if err := ExtractAuto(archive, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "bin", "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello\n" {
		t.Fatalf("unexpected payload %q", b)
	}
}

func TestCPIORoundTrip(t *testing.T) {
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "package.cpio")
			if err := WriteCPIO(fixture(t), archive, compression); err != nil {
				t.Fatal(err)
			}
			checkRoundTrip(t, archive)
		})
	}
}

func TestBzip2CPIORoundTrip(t *testing.T) {
	if _, err := exec.LookPath("bzip2"); err != nil {
		t.Skip("bzip2 unavailable")
	}
	archive := filepath.Join(t.TempDir(), "package.adpm")
	if err := WriteCPIO(fixture(t), archive, "bzip2"); err != nil {
		t.Fatal(err)
	}
	checkRoundTrip(t, archive)
}

func TestTarGzRoundTrip(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "package.tgz")
	if err := WriteTarGz(fixture(t), archive); err != nil {
		t.Fatal(err)
	}
	checkRoundTrip(t, archive)
}

func TestTarTraversalRejected(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0644, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	archive := filepath.Join(t.TempDir(), "bad.tar")
	if err := os.WriteFile(archive, b.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractAuto(archive, t.TempDir()); err == nil {
		t.Fatal("expected traversal archive to be rejected")
	}
}

func TestTarSymlinkEscapeRejected(t *testing.T) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: "lib/link", Linkname: "../../outside", Typeflag: tar.TypeSymlink, Mode: 0777}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "bad-link.tar")
	if err := os.WriteFile(archive, b.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ExtractAuto(archive, t.TempDir()); err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
}
