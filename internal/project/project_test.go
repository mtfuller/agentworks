package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestInitAndLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproject")

	m, err := Init(dir, "myproject", []string{"claude-code"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.Name != "myproject" {
		t.Errorf("Name = %q, want myproject", m.Name)
	}

	for _, k := range artifact.Kinds() {
		if info, err := os.Stat(filepath.Join(dir, k.DirName())); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s to exist", k.DirName())
		}
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != "myproject" || len(loaded.Targets) != 1 || loaded.Targets[0] != "claude-code" {
		t.Errorf("Load() = %+v, want name=myproject targets=[claude-code]", loaded)
	}
}

func TestInitRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "proj", nil); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	if _, err := Init(dir, "proj", nil); err == nil {
		t.Fatal("second Init() expected error, got nil")
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "proj", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	nested := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	found, err := FindRoot(nested)
	if err != nil {
		t.Fatalf("FindRoot() error = %v", err)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	realFound, _ := filepath.EvalSymlinks(found)
	if realFound != realRoot {
		t.Errorf("FindRoot() = %q, want %q", realFound, realRoot)
	}

	if _, err := FindRoot(t.TempDir()); err != ErrNotFound {
		t.Errorf("FindRoot() in empty dir error = %v, want ErrNotFound", err)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "proj", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindSkill, Name: "demo", Description: "A demo."},
		Dir:         filepath.Join(root, "skills", "demo"),
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// A stray subdirectory with no manifest file should be skipped, not error.
	if err := os.MkdirAll(filepath.Join(root, "skills", "not-an-artifact"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	found, errs := Discover(root)
	if len(errs) != 0 {
		t.Fatalf("Discover() errs = %v", errs)
	}
	if len(found) != 1 || found[0].Name != "demo" {
		t.Fatalf("Discover() = %+v, want one artifact named demo", found)
	}

	foundSkillsOnly, errs := Discover(root, artifact.KindSkill)
	if len(errs) != 0 || len(foundSkillsOnly) != 1 {
		t.Fatalf("Discover(KindSkill) = %+v, errs=%v", foundSkillsOnly, errs)
	}
	if none, errs := Discover(root, artifact.KindAgent); len(none) != 0 || len(errs) != 0 {
		t.Fatalf("Discover(KindAgent) = %+v, errs=%v, want empty", none, errs)
	}
}
