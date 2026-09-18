package filecopy

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestCopyArtifactFilesSkipsManifest(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("writing src/main.go: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "demo")
	if err := CopyArtifactFiles(a, destDir); err != nil {
		t.Fatalf("CopyArtifactFiles() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "src", "main.go")); err != nil {
		t.Errorf("expected src/main.go to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "tool.md")); err == nil {
		t.Error("tool.md manifest should not be copied")
	}
}

func TestCopyArtifactFilesSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "demo-node", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(a.Dir, "node_modules", "some-dep"), 0o755); err != nil {
		t.Fatalf("creating node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "node_modules", "some-dep", "index.js"), []byte("module.exports = {};\n"), 0o644); err != nil {
		t.Fatalf("writing node_modules/some-dep/index.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("writing src/main.go: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "demo-node")
	if err := CopyArtifactFiles(a, destDir); err != nil {
		t.Fatalf("CopyArtifactFiles() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "src", "main.go")); err != nil {
		t.Errorf("expected src/main.go to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "node_modules")); err == nil {
		t.Error("node_modules should not be copied")
	}
}

func TestCopyDirExceptSkipsNamedFiles(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("skip me"), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatalf("writing notes.txt: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "out")
	if err := CopyDirExcept(srcDir, destDir, "SKILL.md"); err != nil {
		t.Fatalf("CopyDirExcept() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "SKILL.md")); err == nil {
		t.Error("SKILL.md should not be copied")
	}
	if _, err := os.Stat(filepath.Join(destDir, "notes.txt")); err != nil {
		t.Errorf("expected notes.txt to be copied: %v", err)
	}
}

func TestCopyDirExceptOnlyMatchesRootRelativePaths(t *testing.T) {
	srcDir := t.TempDir()
	nestedSkip := filepath.Join(srcDir, "references", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(nestedSkip), 0o755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(nestedSkip, []byte("not the manifest"), 0o644); err != nil {
		t.Fatalf("writing nested SKILL.md: %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "out")
	if err := CopyDirExcept(srcDir, destDir, "SKILL.md"); err != nil {
		t.Fatalf("CopyDirExcept() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "references", "SKILL.md")); err != nil {
		t.Errorf("a nested references/SKILL.md is not the excluded root manifest and should be copied: %v", err)
	}
}

func TestZipDir(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	destDir := filepath.Join(t.TempDir(), "demo")
	if err := CopyArtifactFiles(a, destDir); err != nil {
		t.Fatalf("CopyArtifactFiles() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing marker.txt: %v", err)
	}

	zipPath, err := ZipDir(destDir)
	if err != nil {
		t.Fatalf("ZipDir() error = %v", err)
	}
	if zipPath != destDir+".zip" {
		t.Errorf("ZipDir() = %q, want %q", zipPath, destDir+".zip")
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer zr.Close()
	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["demo/marker.txt"] {
		t.Errorf("zip missing demo/marker.txt, got entries: %v", found)
	}
}
