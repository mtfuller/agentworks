package filecopy

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildZip writes a zip archive to a temp file containing the given
// entries (path -> content) and returns its path. A trailing "/" path
// creates a directory entry.
func buildZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zw.Create(%q) error = %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("writing %q error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close() error = %v", err)
	}
	return path
}

// buildTarGz writes a gzip-compressed tar archive to a temp file containing
// the given regular-file entries and returns its path.
func buildTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing %q error = %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz.Close() error = %v", err)
	}
	return path
}

func TestUnzipExtractsTree(t *testing.T) {
	archive := buildZip(t, map[string]string{
		"repo-main/SKILL.md":          "---\nname: x\n---\n",
		"repo-main/scripts/main.py":   "print('hi')",
		"repo-main/references/notes/": "",
	})
	destDir := filepath.Join(t.TempDir(), "out")

	if err := Unzip(archive, destDir); err != nil {
		t.Fatalf("Unzip() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "repo-main", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading extracted SKILL.md: %v", err)
	}
	if !bytes.Contains(data, []byte("name: x")) {
		t.Errorf("SKILL.md content = %q, want it to contain %q", data, "name: x")
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo-main", "scripts", "main.py")); err != nil {
		t.Errorf("scripts/main.py not extracted: %v", err)
	}
}

func TestUntarGzExtractsTree(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"repo-main/SKILL.md":        "---\nname: x\n---\n",
		"repo-main/scripts/main.py": "print('hi')",
	})
	destDir := filepath.Join(t.TempDir(), "out")

	if err := UntarGz(archive, destDir); err != nil {
		t.Fatalf("UntarGz() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo-main", "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "repo-main", "scripts", "main.py")); err != nil {
		t.Errorf("scripts/main.py not extracted: %v", err)
	}
}

func TestUnzipRejectsPathTraversal(t *testing.T) {
	archive := buildZip(t, map[string]string{"../../evil.txt": "pwned"})
	destDir := filepath.Join(t.TempDir(), "out")

	if err := Unzip(archive, destDir); err == nil {
		t.Fatal("Unzip() with a path-traversal entry expected error, got nil")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(destDir), "..", "evil.txt")); err == nil {
		t.Error("Unzip() wrote outside destDir despite returning an error")
	}
}

func TestUntarGzRejectsPathTraversal(t *testing.T) {
	archive := buildTarGz(t, map[string]string{"../../evil.txt": "pwned"})
	destDir := filepath.Join(t.TempDir(), "out")

	if err := UntarGz(archive, destDir); err == nil {
		t.Fatal("UntarGz() with a path-traversal entry expected error, got nil")
	}
}

func TestUntarGzRejectsAbsolutePath(t *testing.T) {
	archive := buildTarGz(t, map[string]string{"/etc/evil.txt": "pwned"})
	destDir := filepath.Join(t.TempDir(), "out")

	if err := UntarGz(archive, destDir); err == nil {
		t.Fatal("UntarGz() with an absolute entry path expected error, got nil")
	}
}

func TestUntarGzSkipsSymlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "evil-link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz.Close() error = %v", err)
	}
	f.Close()

	destDir := filepath.Join(t.TempDir(), "out")
	if err := UntarGz(path, destDir); err != nil {
		t.Fatalf("UntarGz() error = %v, want nil (symlinks are silently skipped)", err)
	}
	if _, err := os.Lstat(filepath.Join(destDir, "evil-link")); err == nil {
		t.Error("UntarGz() extracted a symlink entry, want it skipped")
	}
}

func TestExtractRejectsTooManyEntries(t *testing.T) {
	entries := make(map[string]string, maxArchiveEntries+1)
	for i := 0; i < maxArchiveEntries+1; i++ {
		entries[fmt.Sprintf("f/%d.txt", i)] = "x"
	}
	archive := buildZip(t, entries)
	destDir := filepath.Join(t.TempDir(), "out")

	if err := Unzip(archive, destDir); err == nil {
		t.Fatal("Unzip() with too many entries expected error, got nil")
	}
}

func TestExtractRejectsOversizeTotal(t *testing.T) {
	big := make([]byte, maxArchiveBytes/2+1)
	archive := buildZip(t, map[string]string{
		"a.bin": string(big),
		"b.bin": string(big),
	})
	destDir := filepath.Join(t.TempDir(), "out")

	if err := Unzip(archive, destDir); err == nil {
		t.Fatal("Unzip() with oversize total content expected error, got nil")
	}
}

// Scripts must stay runnable (a hook or MCP server is often exec'd directly),
// but no other archive-supplied permission bit may be trusted.
func TestExtractKeepsOnlyTheExecuteBit(t *testing.T) {
	modes := map[string]int64{
		"plain.txt":  0o644,
		"run.sh":     0o755,
		"partial.sh": 0o700,  // owner-execute only
		"setuid.sh":  0o4755, // setuid + executable
		"open.txt":   0o666,  // world-writable
		"secret.txt": 0o600,  // private
	}
	want := map[string]os.FileMode{
		"plain.txt": 0o644, "run.sh": 0o755, "partial.sh": 0o755,
		"setuid.sh": 0o755, "open.txt": 0o644, "secret.txt": 0o644,
	}

	// tar
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, mode := range modes {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte("x"))
	}
	tw.Close()
	gz.Close()
	tarPath := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(tarPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	tarDest := t.TempDir()
	if err := UntarGz(tarPath, tarDest); err != nil {
		t.Fatalf("UntarGz() error = %v", err)
	}

	// zip
	zipPath := filepath.Join(t.TempDir(), "a.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for name, mode := range modes {
		hdr := &zip.FileHeader{Name: name}
		hdr.SetMode(os.FileMode(mode))
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("x"))
	}
	zw.Close()
	zf.Close()
	zipDest := t.TempDir()
	if err := Unzip(zipPath, zipDest); err != nil {
		t.Fatalf("Unzip() error = %v", err)
	}

	for format, dest := range map[string]string{"tar": tarDest, "zip": zipDest} {
		for name, wantMode := range want {
			info, err := os.Stat(filepath.Join(dest, name))
			if err != nil {
				t.Fatal(err)
			}
			// Compare ignoring group/other write, which the process umask may clear.
			if got := info.Mode().Perm() &^ 0o022; got != wantMode&^0o022 {
				t.Errorf("%s: %s extracted as %v, want %v", format, name, info.Mode().Perm(), wantMode)
			}
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
				t.Errorf("%s: %s kept a setuid/setgid/sticky bit", format, name)
			}
		}
	}
}

func TestCopyFileKeepsOnlyTheExecuteBit(t *testing.T) {
	dir := t.TempDir()
	for name, mode := range map[string]os.FileMode{"a.txt": 0o644, "run.sh": 0o755, "secret": 0o600} {
		src := filepath.Join(dir, "src-"+name)
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(src, mode); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "dst-"+name)
		if err := CopyFile(src, dst); err != nil {
			t.Fatalf("CopyFile(%s) error = %v", name, err)
		}
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		wantExec := mode&0o111 != 0
		if gotExec := info.Mode()&0o111 != 0; gotExec != wantExec {
			t.Errorf("%s: executable = %v, want %v (mode %v)", name, gotExec, wantExec, info.Mode())
		}
	}
}
