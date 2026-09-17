package claudecode

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

// newTestWorkflowProject builds a project with a researcher agent, a
// jira-fetch tool (command + auth set, so it's export-ready), and a
// software-factory workflow whose steps reference both.
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

	// plugin.json under .claude-plugin/
	var pm pluginManifest
	pmData, err := os.ReadFile(filepath.Join(dest, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("reading .claude-plugin/plugin.json: %v", err)
	}
	if err := json.Unmarshal(pmData, &pm); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}
	if pm.Name != "software-factory" {
		t.Errorf("plugin.json Name = %q, want software-factory", pm.Name)
	}

	// agents/researcher.md
	agentData, err := os.ReadFile(filepath.Join(dest, "agents", "researcher.md"))
	if err != nil {
		t.Fatalf("reading agents/researcher.md: %v", err)
	}
	if !strings.Contains(string(agentData), "name: researcher") {
		t.Errorf("agents/researcher.md missing name field, got:\n%s", agentData)
	}

	// .mcp.json with the jira-fetch server
	mcpData, err := os.ReadFile(filepath.Join(dest, ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpData), "python3 src/main.py") {
		t.Errorf(".mcp.json missing tool command, got:\n%s", mcpData)
	}
	if !strings.Contains(string(mcpData), `"JIRA_API_TOKEN": "${JIRA_API_TOKEN}"`) {
		t.Errorf(".mcp.json missing env var reference, got:\n%s", mcpData)
	}

	// commands/software-factory.md
	cmdData, err := os.ReadFile(filepath.Join(dest, "commands", "software-factory.md"))
	if err != nil {
		t.Fatalf("reading commands/software-factory.md: %v", err)
	}
	cmd := string(cmdData)
	if !strings.Contains(cmd, "name: software-factory") {
		t.Errorf("command file missing name field, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "@researcher") {
		t.Errorf("command file missing @researcher mention, got:\n%s", cmd)
	}
	if !strings.Contains(cmd, "`jira-fetch` MCP tool") {
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
	wf.Extra["steps"] = []any{map[string]any{"agent": "does-not-exist"}}
	if err := wf.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(wf.Dir, artifact.KindWorkflow)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if _, err := (exporter{}).Export(reloaded, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() with a missing referenced agent expected error, got nil")
	}
}
