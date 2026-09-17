package agentskills

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func newTestSkill(t *testing.T) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "csv-analyzer", scaffold.Options{
		Description: "Analyze a CSV and flag rows that stand out.",
	})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "samples", "sample.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("writing sample file: %v", err)
	}
	return a
}

func TestWrite(t *testing.T) {
	a := newTestSkill(t)
	destDir := filepath.Join(t.TempDir(), "csv-analyzer")

	if err := Write(a, destDir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	skillMD, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	content := string(skillMD)
	if !strings.Contains(content, "name: csv-analyzer") {
		t.Errorf("SKILL.md missing name field, got:\n%s", content)
	}
	if !strings.Contains(content, "description: Analyze a CSV") {
		t.Errorf("SKILL.md missing description field, got:\n%s", content)
	}
	for _, leaked := range []string{"version:", "targets:", "entrypoint:", "kind:"} {
		if strings.Contains(content, leaked) {
			t.Errorf("SKILL.md leaked AgentWorks-only field %q, got:\n%s", leaked, content)
		}
	}

	if _, err := os.Stat(filepath.Join(destDir, "scripts", "main.py")); err != nil {
		t.Errorf("expected scripts/main.py to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "samples", "sample.csv")); err != nil {
		t.Errorf("expected samples/sample.csv to be copied: %v", err)
	}
}

func TestWriteCarriesOptionalFields(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["license"] = "Apache-2.0"
	a.Extra["compatibility"] = "Requires Python 3.10+"
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindSkill)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "demo")
	if err := Write(reloaded, destDir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	if !strings.Contains(string(content), "license: Apache-2.0") {
		t.Errorf("SKILL.md missing license field, got:\n%s", content)
	}
	if !strings.Contains(string(content), "compatibility: Requires Python") {
		t.Errorf("SKILL.md missing compatibility field, got:\n%s", content)
	}
}

func TestWriteRejectsNonSkill(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if err := Write(a, t.TempDir()); err == nil {
		t.Fatal("Write() of an agent expected error, got nil")
	}
}

func TestZip(t *testing.T) {
	a := newTestSkill(t)
	destDir := filepath.Join(t.TempDir(), "csv-analyzer")
	if err := Write(a, destDir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	zipPath, err := Zip(destDir)
	if err != nil {
		t.Fatalf("Zip() error = %v", err)
	}
	if !strings.HasSuffix(zipPath, "csv-analyzer.zip") {
		t.Fatalf("Zip() path = %q, want it to end in csv-analyzer.zip", zipPath)
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
	if !found["csv-analyzer/SKILL.md"] {
		t.Errorf("zip missing csv-analyzer/SKILL.md, got entries: %v", found)
	}
}
