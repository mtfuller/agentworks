package claudecode

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func newTestSkill(t *testing.T) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "csv-analyzer", scaffold.Options{
		Description: "Analyze a CSV and flag rows that stand out.",
		Targets:     []string{TargetID},
	})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	// Add a sample file to confirm supporting files get copied across.
	if err := os.WriteFile(filepath.Join(a.Dir, "samples", "sample.csv"), []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("writing sample file: %v", err)
	}
	return a
}

func TestExportSkill(t *testing.T) {
	a := newTestSkill(t)
	outDir := t.TempDir()

	e := exporter{}
	dest, err := e.Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "csv-analyzer") {
		t.Errorf("Export() dest = %q, want %s/csv-analyzer", dest, outDir)
	}

	skillMD, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
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
	// AgentWorks-only fields must not leak into the vendor file.
	for _, leaked := range []string{"version:", "targets:", "entrypoint:", "kind:"} {
		if strings.Contains(content, leaked) {
			t.Errorf("SKILL.md leaked AgentWorks-only field %q, got:\n%s", leaked, content)
		}
	}

	if _, err := os.Stat(filepath.Join(dest, "scripts", "main.py")); err != nil {
		t.Errorf("expected scripts/main.py to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "samples", "sample.csv")); err != nil {
		t.Errorf("expected samples/sample.csv to be copied: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("reading %s: %v", dest, err)
	}
	count := 0
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "skill.md") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one skill.md/SKILL.md entry (the exported SKILL.md), found %d", count)
	}
}

func TestExportSkillZip(t *testing.T) {
	a := newTestSkill(t)
	outDir := t.TempDir()

	e := exporter{}
	dest, err := e.Export(a, outDir, targets.ExportOptions{Zip: true})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if !strings.HasSuffix(dest, ".zip") {
		t.Fatalf("Export() with Zip=true dest = %q, want *.zip", dest)
	}

	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer zr.Close()

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["csv-analyzer/SKILL.md"] {
		t.Errorf("zip missing csv-analyzer/SKILL.md, got entries: %v", names)
	}
}

func TestExportRejectsUnsupportedKind(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if _, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() of an agent expected error, got nil")
	}
}

func newTestTool(t *testing.T) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{
		Description: "Fetch a Jira ticket.",
		Targets:     []string{TargetID},
	})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["command"] = "python3 src/main.py"
	a.Extra["auth"] = []string{"JIRA_API_TOKEN"}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Dir, "src", "main.py"), []byte("# jira-fetch\n"), 0o644); err != nil {
		t.Fatalf("writing src/main.py: %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindTool)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return reloaded
}

func TestExportTool(t *testing.T) {
	a := newTestTool(t)
	outDir := t.TempDir()

	dest, err := (exporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "jira-fetch") {
		t.Errorf("Export() dest = %q, want %s/jira-fetch", dest, outDir)
	}

	data, err := os.ReadFile(filepath.Join(dest, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"command": "sh"`) {
		t.Errorf(".mcp.json missing sh command, got:\n%s", content)
	}
	if !strings.Contains(content, "python3 src/main.py") {
		t.Errorf(".mcp.json missing tool command, got:\n%s", content)
	}
	if !strings.Contains(content, `"JIRA_API_TOKEN": "${JIRA_API_TOKEN}"`) {
		t.Errorf(".mcp.json missing env var reference, got:\n%s", content)
	}
	if strings.Contains(content, "$schema") {
		t.Errorf(".mcp.json should not have a $schema field (not part of Claude Code's own format), got:\n%s", content)
	}

	if _, err := os.Stat(filepath.Join(dest, "src", "main.py")); err != nil {
		t.Errorf("expected src/main.py to be copied: %v", err)
	}
}

func TestExportToolRequiresCommand(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if _, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() with no command set expected error, got nil")
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
