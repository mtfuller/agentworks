package geminicli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestExportSkill(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "csv-analyzer", scaffold.Options{Description: "Analyze a CSV and flag rows that stand out."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	outDir := t.TempDir()

	dest, err := exportSkill(a, outDir)
	if err != nil {
		t.Fatalf("exportSkill() error = %v", err)
	}
	wantDir := filepath.Join(outDir, "csv-analyzer")
	if dest != wantDir {
		t.Errorf("exportSkill() dest = %q, want %q", dest, wantDir)
	}

	data, err := os.ReadFile(filepath.Join(wantDir, "gemini-extension.json"))
	if err != nil {
		t.Fatalf("reading gemini-extension.json: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing gemini-extension.json: %v", err)
	}
	if m.Name != "csv-analyzer" {
		t.Errorf("manifest.Name = %q, want csv-analyzer", m.Name)
	}
	if m.Description != "Analyze a CSV and flag rows that stand out." {
		t.Errorf("manifest.Description = %q, want the artifact description", m.Description)
	}
	if m.MCPServers != nil {
		t.Errorf("manifest.MCPServers = %v, want nil for a skill", m.MCPServers)
	}

	if _, err := os.Stat(filepath.Join(wantDir, "GEMINI.md")); err != nil {
		t.Errorf("expected GEMINI.md to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "scripts", "main.py")); err != nil {
		t.Errorf("expected the skill's supporting files to be copied: %v", err)
	}
}
