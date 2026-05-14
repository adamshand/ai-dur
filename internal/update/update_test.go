package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsDevVersion(t *testing.T) {
	tests := map[string]bool{
		"dev":               true,
		"dev-20260514-0116": true,
		"20260514-0116":     false,
		"development":       false,
	}
	for version, want := range tests {
		if got := isDevVersion(version); got != want {
			t.Fatalf("isDevVersion(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestAssetName(t *testing.T) {
	got := assetName("20260514-0116", "linux", "amd64")
	want := "dur-20260514-0116-linux-amd64.tar.gz"
	if got != want {
		t.Fatalf("assetName() = %q, want %q", got, want)
	}
}

func TestExpectedChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	archive := "dur-20260514-0116-linux-amd64.tar.gz"
	hash := strings.Repeat("a", sha256.Size*2)
	content := fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", hash, archive, strings.Repeat("b", sha256.Size*2))
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := expectedChecksum(path, archive)
	if err != nil {
		t.Fatal(err)
	}
	if got != hash {
		t.Fatalf("expectedChecksum() = %q, want %q", got, hash)
	}
}

func TestExpectedChecksumMissingArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", sha256.Size*2)+"  other.tar.gz\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := expectedChecksum(path, "dur-20260514-0116-linux-amd64.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing checksum error, got %v", err)
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	data := []byte("hello")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := verifySHA256(path, hash); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(path, strings.Repeat("0", sha256.Size*2)); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestExtractDurBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "dur.tar.gz")
	if err := writeTarGz(archive, map[string]string{"README.md": "docs", "dur": "binary"}); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dur")
	if err := extractDurBinary(archive, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Fatalf("extracted %q, want binary", got)
	}
}

func TestExtractDurBinaryRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "dur.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "dur", Typeflag: tar.TypeSymlink, Linkname: "/tmp/dur"}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := extractDurBinary(archive, filepath.Join(dir, "dur")); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestInstallBinaryPreservesMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dur")
	src := filepath.Join(dir, "new-dur")
	if err := os.WriteFile(target, []byte("old"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	u := updater{options: Options{ExecutablePath: target}}
	installed, err := u.installBinary(src)
	if err != nil {
		t.Fatal(err)
	}
	wantInstalled, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if installed != wantInstalled {
		t.Fatalf("installed path = %q, want %q", installed, wantInstalled)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("target content = %q, want new", got)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0750 {
		t.Fatalf("target mode = %v, want 0750", st.Mode().Perm())
	}
}

func TestInstallBinaryResolvesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-dur")
	link := filepath.Join(dir, "dur")
	src := filepath.Join(dir, "new-dur")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	u := updater{options: Options{ExecutablePath: link}}
	installed, err := u.installBinary(src)
	if err != nil {
		t.Fatal(err)
	}
	wantInstalled, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if installed != wantInstalled {
		t.Fatalf("installed path = %q, want %q", installed, wantInstalled)
	}
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("install replaced symlink instead of symlink target")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("target content = %q, want new", got)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("/tmp/dur update/it's/dur")
	want := "'/tmp/dur update/it'\\''s/dur'"
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func writeTarGz(path string, files map[string]string) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(content))}); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0600)
}
