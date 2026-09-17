package githubcopilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func newTestWorkflowProject(t *testing.T) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	if _, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Researches things."}); err != nil {
		t.Fatalf("scaffold.New(agent) error = %v", err)
	}

	tool, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{Description: "Fetch a ticket."})
	if err != nil {
		t.Fatalf("scaffold.New(tool) error = %v", err)
	}
	tool.Extra["command"] = "python3 src/main.py"
	tool.Extra["auth"] = []string{"JIRA_API_TOKEN"}
	if err := tool.Save(); err != nil {
		t.Fatalf("Save(tool) error = %v", err)
	}

	wf, err := scaffold.New(root, artifact.KindWorkflow, "software-factory", scaffold.Options{Description: "Research then fetch."})
	if err != nil {
		t.Fatalf("scaffold.New(workflow) error = %v", err)
	}
	wf.Extra["steps"] = []any{
		map[string]any{"agent": "researcher"},
		map[string]any{"tool": "jira-fetch"},
	}
	if err := wf.Save(); err != nil {
		t.Fatalf("Save(workflow) error = %v", err)
	}

	reloaded, err := artifact.Load(wf.Dir, artifact.KindWorkflow)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return reloaded
}

func TestExportWorkflow(t *testing.T) {
	a := newTestWorkflowProject(t)
	outDir := t.TempDir()

	dest, err := (exporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "software-factory") {
		t.Errorf("Export() dest = %q, want %s/software-factory", dest, outDir)
	}

	var pm pluginManifest
	pmData, err := os.ReadFile(filepath.Join(dest, "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	if err := json.Unmarshal(pmData, &pm); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}
	if pm.Schema != pluginSchema {
		t.Errorf("plugin.json Schema = %q, want %q", pm.Schema, pluginSchema)
	}
	if pm.Name != "software-factory" {
		t.Errorf("plugin.json Name = %q, want software-factory", pm.Name)
	}

	agentData, err := os.ReadFile(filepath.Join(dest, "com.github.copilot", "agents", "researcher.agent.md"))
	if err != nil {
		t.Fatalf("reading com.github.copilot/agents/researcher.agent.md: %v", err)
	}
	if !strings.Contains(string(agentData), "name: researcher") {
		t.Errorf("agent file missing name field, got:\n%s", agentData)
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

	cmdData, err := os.ReadFile(filepath.Join(dest, "com.github.copilot", "commands", "software-factory.md"))
	if err != nil {
		t.Fatalf("reading com.github.copilot/commands/software-factory.md: %v", err)
	}
	cmd := string(cmdData)
	if !strings.Contains(cmd, "name: software-factory") {
		t.Errorf("command file missing name field, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, `"researcher" custom agent`) {
		t.Errorf("command file missing researcher agent mention, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, `"jira-fetch" MCP tool`) {
		t.Errorf("command file missing jira-fetch tool mention, got:\n%s", cmd)
	}
}

func TestExportWorkflowZip(t *testing.T) {
	a := newTestWorkflowProject(t)
	dest, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{Zip: true})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if filepath.Ext(dest) != ".zip" {
		t.Fatalf("Export() with Zip=true dest = %q, want *.zip", dest)
	}
}

func TestExportWorkflowMissingStepFails(t *testing.T) {
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	wf, err := scaffold.New(root, artifact.KindWorkflow, "broken", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	wf.Extra["steps"] = []any{map[string]any{"tool": "does-not-exist"}}
	if err := wf.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(wf.Dir, artifact.KindWorkflow)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if _, err := (exporter{}).Export(reloaded, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() with a missing referenced tool expected error, got nil")
	}
}
