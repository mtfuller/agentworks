package githubcopilot

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	dest, err := (skillExporter{}).Export(a, t.TempDir(), targets.ExportOptions{Zip: true})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if filepath.Ext(dest) != ".zip" {
		t.Fatalf("Export() with Zip=true dest = %q, want *.zip", dest)
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
