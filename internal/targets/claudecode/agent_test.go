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

func TestExportAgentToolsAndModel(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Researches things."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["tools"] = []string{"web-search", "read-files"}
	a.Extra["model"] = "powerful"
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindAgent)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	dest, err := (exporter{}).Export(reloaded, t.TempDir(), targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	agentData, err := os.ReadFile(filepath.Join(dest, "agents", "researcher.md"))
	if err != nil {
		t.Fatalf("reading agents/researcher.md: %v", err)
	}
	content := string(agentData)
	if !strings.Contains(content, "tools: Glob, Grep, Read, WebFetch, WebSearch") {
		t.Errorf("agent file missing mapped tools field, got:\n%s", content)
	}
	if !strings.Contains(content, "model: opus") {
		t.Errorf("agent file missing mapped model field, got:\n%s", content)
	}
}

func TestExportAgentOmitsToolsAndModelWhenUnset(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Researches things."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	dest, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	agentData, err := os.ReadFile(filepath.Join(dest, "agents", "researcher.md"))
	if err != nil {
		t.Fatalf("reading agents/researcher.md: %v", err)
	}
	// Only the frontmatter block matters here -- the scaffolded body's own
	// prose legitimately mentions "tools:"/"model:" as backtick-quoted
	// frontmatter field names.
	frontmatter := strings.SplitN(string(agentData), "---", 3)[1]
	if strings.Contains(frontmatter, "tools:") || strings.Contains(frontmatter, "model:") {
		t.Errorf("agent frontmatter with no tools/model set should omit both fields, got:\n%s", frontmatter)
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
