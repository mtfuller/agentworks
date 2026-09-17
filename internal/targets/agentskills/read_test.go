package agentskills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestReadParsesSkillMD(t *testing.T) {
	srcDir := t.TempDir()
	content := "---\nname: csv-analyzer\ndescription: Analyze a CSV.\n---\n\n# CSV Analyzer\n\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}

	a, err := Read(srcDir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if a.Kind != artifact.KindSkill {
		t.Errorf("Kind = %q, want skill", a.Kind)
	}
	if a.Name != "csv-analyzer" {
		t.Errorf("Name = %q, want csv-analyzer", a.Name)
	}
	if a.Description != "Analyze a CSV." {
		t.Errorf("Description = %q, want %q", a.Description, "Analyze a CSV.")
	}
	if a.Body != "# CSV Analyzer\n\nDo the thing.\n" {
		t.Errorf("Body = %q", a.Body)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	a := newTestSkill(t)
	a.Extra["license"] = "Apache-2.0"
	a.Extra["compatibility"] = "Requires Python 3.10+"
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindSkill)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	destDir := filepath.Join(t.TempDir(), "csv-analyzer")
	if err := Write(reloaded, destDir); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	roundTripped, err := Read(destDir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if roundTripped.Name != reloaded.Name {
		t.Errorf("Name = %q, want %q", roundTripped.Name, reloaded.Name)
	}
	if roundTripped.Description != reloaded.Description {
		t.Errorf("Description = %q, want %q", roundTripped.Description, reloaded.Description)
	}
	if roundTripped.ExtraString("license") != "Apache-2.0" {
		t.Errorf("license = %q, want Apache-2.0", roundTripped.ExtraString("license"))
	}
	if roundTripped.ExtraString("compatibility") != "Requires Python 3.10+" {
		t.Errorf("compatibility = %q, want Requires Python 3.10+", roundTripped.ExtraString("compatibility"))
	}
}

func TestReadPreservesUnknownExtras(t *testing.T) {
	srcDir := t.TempDir()
	content := "---\nname: demo\ndescription: x\ncustom_field: keep-me\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}

	a, err := Read(srcDir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if a.ExtraString("custom_field") != "keep-me" {
		t.Errorf("custom_field = %q, want keep-me", a.ExtraString("custom_field"))
	}
}

func TestReadMissingSkillMD(t *testing.T) {
	if _, err := Read(t.TempDir()); err == nil {
		t.Fatal("Read() of a directory with no SKILL.md expected error, got nil")
	}
}

func TestReadMalformedFrontmatter(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("not a valid skill file"), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
	if _, err := Read(srcDir); err == nil {
		t.Fatal("Read() of a malformed SKILL.md expected error, got nil")
	}
}

func TestIsSkillDir(t *testing.T) {
	srcDir := t.TempDir()
	if IsSkillDir(srcDir) {
		t.Error("IsSkillDir() = true for an empty directory, want false")
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
	if !IsSkillDir(srcDir) {
		t.Error("IsSkillDir() = false for a directory with SKILL.md, want true")
	}
}
