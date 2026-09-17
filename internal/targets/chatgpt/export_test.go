package chatgpt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func TestExportSkill(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "A demo skill."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := (skillExporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "demo") {
		t.Errorf("Export() dest = %q, want %s/demo", dest, outDir)
	}
	content, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading SKILL.md: %v", err)
	}
	if !strings.Contains(string(content), "name: demo") {
		t.Errorf("SKILL.md missing name field, got:\n%s", content)
	}
}

func TestExportRejectsNonSkill(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if _, err := (skillExporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() of a tool expected error, got nil")
	}
}

func TestRegisteredWithTargets(t *testing.T) {
	e, err := targets.GetExporter(TargetID)
	if err != nil {
		t.Fatalf("GetExporter(%s) error = %v", TargetID, err)
	}
	if e.TargetID() != TargetID {
		t.Errorf("TargetID() = %q, want %q", e.TargetID(), TargetID)
	}
}
