package claudecode

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

func TestExportAgent(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Researches things."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := (exporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "researcher") {
		t.Errorf("Export() dest = %q, want %s/researcher", dest, outDir)
	}

	var pm pluginManifest
	pmData, err := os.ReadFile(filepath.Join(dest, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("reading .claude-plugin/plugin.json: %v", err)
	}
	if err := json.Unmarshal(pmData, &pm); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}
	if pm.Name != "researcher" {
		t.Errorf("plugin.json Name = %q, want researcher", pm.Name)
	}

	agentData, err := os.ReadFile(filepath.Join(dest, "agents", "researcher.md"))
	if err != nil {
		t.Fatalf("reading agents/researcher.md: %v", err)
	}
	content := string(agentData)
	if !strings.Contains(content, "name: researcher") {
		t.Errorf("agent file missing name field, got:\n%s", content)
	}
	if !strings.Contains(content, "description: Researches things.") {
		t.Errorf("agent file missing description field, got:\n%s", content)
	}
}

func TestExportAgentZip(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "x"})
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
