package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	l, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if l.Version != currentVersion || len(l.Imports) != 0 || len(l.Exports) != 0 {
		t.Errorf("Load() of missing file = %+v, want empty v%d lockfile", l, currentVersion)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	l.SetImport("skills/demo", ImportEntry{
		Kind:          "skill",
		Source:        SourceRef{Type: "github", Repo: "owner/repo", Ref: "main"},
		ContentSHA256: "abc123",
		Imported:      "2026-01-01",
	})
	l.SetExport(ExportKey("claude-code", "skills/demo"), ExportEntry{
		Target:       "claude-code",
		Artifact:     "skills/demo",
		Output:       "/tmp/dist/demo",
		SourceSHA256: "src123",
		OutputSHA256: "out123",
		Exported:     "2026-01-02",
	})
	if err := l.Save(dir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reloaded Load() error = %v", err)
	}
	entry, ok := reloaded.Imports["skills/demo"]
	if !ok {
		t.Fatalf("reloaded.Imports missing skills/demo: %+v", reloaded.Imports)
	}
	if entry.Source.Repo != "owner/repo" || entry.ContentSHA256 != "abc123" {
		t.Errorf("reloaded import entry = %+v, want repo=owner/repo hash=abc123", entry)
	}

	exp, ok := reloaded.Exports[ExportKey("claude-code", "skills/demo")]
	if !ok {
		t.Fatalf("reloaded.Exports missing entry: %+v", reloaded.Exports)
	}
	if exp.Output != "/tmp/dist/demo" || exp.SourceSHA256 != "src123" {
		t.Errorf("reloaded export entry = %+v, want output=/tmp/dist/demo source_sha256=src123", exp)
	}
}

func TestRemoveImportAndExport(t *testing.T) {
	l := empty()
	l.SetImport("skills/demo", ImportEntry{Kind: "skill"})
	l.SetExport("claude-code:skills/demo", ExportEntry{Target: "claude-code"})

	l.RemoveImport("skills/demo")
	l.RemoveExport("claude-code:skills/demo")

	if len(l.Imports) != 0 || len(l.Exports) != 0 {
		t.Errorf("after Remove*, l = %+v, want both maps empty", l)
	}
}

func TestHashDirDeterministicAndSensitiveToChanges(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir() error = %v", err)
	}
	h2, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir() error = %v", err)
	}
	if h1 != h2 {
		t.Errorf("HashDir() not deterministic: %q != %q", h1, h2)
	}

	// Changing a file's content changes the hash.
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	h3, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir() error = %v", err)
	}
	if h3 == h1 {
		t.Error("HashDir() unchanged after editing a file's content")
	}

	// Adding a file also changes the hash.
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	h4, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir() error = %v", err)
	}
	if h4 == h3 {
		t.Error("HashDir() unchanged after adding a file")
	}
}

func TestHashDirSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(path, []byte("zip content"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1, err := HashDir(path)
	if err != nil {
		t.Fatalf("HashDir(file) error = %v", err)
	}
	if err := os.WriteFile(path, []byte("different content"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2, err := HashDir(path)
	if err != nil {
		t.Fatalf("HashDir(file) error = %v", err)
	}
	if h1 == h2 {
		t.Error("HashDir(file) unchanged after editing file content")
	}
}

func TestRelKey(t *testing.T) {
	root := filepath.FromSlash("/proj")
	got, err := RelKey(root, filepath.FromSlash("/proj/skills/demo"))
	if err != nil {
		t.Fatalf("RelKey() error = %v", err)
	}
	if got != "skills/demo" {
		t.Errorf("RelKey() = %q, want skills/demo", got)
	}
}
