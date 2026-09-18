package githubcopilot

import (
	"encoding/json"
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

	dest, err := (exporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "demo") {
		t.Errorf("Export() dest = %q, want %s/demo", dest, outDir)
	}

	data, err := os.ReadFile(filepath.Join(dest, "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}
	if m.Schema != pluginSchema {
		t.Errorf("Schema = %q, want %q", m.Schema, pluginSchema)
	}
	if m.Name != "demo" {
		t.Errorf("Name = %q, want demo", m.Name)
	}
	if m.Description != "A demo skill." {
		t.Errorf("Description = %q, want %q", m.Description, "A demo skill.")
	}

	skillMD := filepath.Join(dest, "skills", "demo", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Errorf("expected %s to exist: %v", skillMD, err)
	}
}

func TestExportZip(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	dest, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{Zip: true})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if filepath.Ext(dest) != ".zip" {
		t.Fatalf("Export() with Zip=true dest = %q, want *.zip", dest)
	}
}

func TestExportTool(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "jira-fetch", scaffold.Options{Description: "Fetch a Jira ticket."})
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
	reloaded, err := artifact.Load(a.Dir, artifact.KindMCP)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := (exporter{}).Export(reloaded, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "jira-fetch") {
		t.Errorf("Export() dest = %q, want %s/jira-fetch", dest, outDir)
	}

	var pm pluginManifest
	pmData, err := os.ReadFile(filepath.Join(dest, "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	if err := json.Unmarshal(pmData, &pm); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}
	if pm.Name != "jira-fetch" {
		t.Errorf("plugin.json Name = %q, want jira-fetch", pm.Name)
	}

	mcpData, err := os.ReadFile(filepath.Join(dest, "mcp.json"))
	if err != nil {
		t.Fatalf("reading mcp.json: %v", err)
	}
	content := string(mcpData)
	if !strings.Contains(content, mcpSchema) {
		t.Errorf("mcp.json missing $schema, got:\n%s", content)
	}
	if !strings.Contains(content, "python3 src/main.py") {
		t.Errorf("mcp.json missing tool command, got:\n%s", content)
	}
	if !strings.Contains(content, `"JIRA_API_TOKEN": "${JIRA_API_TOKEN}"`) {
		t.Errorf("mcp.json missing env var reference, got:\n%s", content)
	}

	if _, err := os.Stat(filepath.Join(dest, "src", "main.py")); err != nil {
		t.Errorf("expected src/main.py at the plugin root: %v", err)
	}
	// A tool has no skills/ directory -- that's only for the skill export path.
	if _, err := os.Stat(filepath.Join(dest, "skills")); err == nil {
		t.Error("tool export should not create a skills/ directory")
	}
}

func TestExportMCPRequiresCommand(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindMCP, "jira-fetch", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["command"] = "" // the default scaffold is runnable; simulate an author who cleared it
	if _, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() with no command set expected error, got nil")
	}
}

func TestExportRejectsUnknownKind(t *testing.T) {
	// github-copilot now handles all five real artifact.Kind values;
	// exercise the defensive default branch with a value that can't come
	// from artifact.ParseKind.
	a := &artifact.Artifact{Frontmatter: artifact.Frontmatter{Kind: artifact.Kind("bogus"), Name: "x", Description: "x"}}
	if _, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() of an unknown kind expected error, got nil")
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
