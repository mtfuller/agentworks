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
	a, err := scaffold.New(root, artifact.KindTool, "demo", scaffold.Options{Description: "x"})
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

func TestZipDir(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindTool, "demo", scaffold.Options{Description: "x"})
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
