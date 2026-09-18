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
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// newTestBundleMembers builds a skill, an agent, an export-ready tool, and
// two hooks (both targeting the same event, to exercise the
// append-not-overwrite merge) in a fresh project.
func newTestBundleMembers(t *testing.T) []*artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}

	skill, err := scaffold.New(root, artifact.KindSkill, "s1", scaffold.Options{Description: "A skill."})
	if err != nil {
		t.Fatalf("scaffold.New(skill) error = %v", err)
	}

	agent, err := scaffold.New(root, artifact.KindAgent, "a1", scaffold.Options{Description: "An agent."})
	if err != nil {
		t.Fatalf("scaffold.New(agent) error = %v", err)
	}

	tool, err := scaffold.New(root, artifact.KindTool, "t1", scaffold.Options{Description: "A tool."})
	if err != nil {
		t.Fatalf("scaffold.New(tool) error = %v", err)
	}
	tool.Extra["command"] = "python3 src/main.py"
	if err := tool.Save(); err != nil {
		t.Fatalf("Save(tool) error = %v", err)
	}
	tool, err = artifact.Load(tool.Dir, artifact.KindTool)
	if err != nil {
		t.Fatalf("Load(tool) error = %v", err)
	}

	hook1, err := scaffold.New(root, artifact.KindHook, "h1", scaffold.Options{Description: "First hook."})
	if err != nil {
		t.Fatalf("scaffold.New(hook1) error = %v", err)
	}
	hook1.Extra["events"] = []string{"PreToolUse"}
	hook1.Extra["command"] = "echo one"
	if err := hook1.Save(); err != nil {
		t.Fatalf("Save(hook1) error = %v", err)
	}
	hook1, err = artifact.Load(hook1.Dir, artifact.KindHook)
	if err != nil {
		t.Fatalf("Load(hook1) error = %v", err)
	}

	hook2, err := scaffold.New(root, artifact.KindHook, "h2", scaffold.Options{Description: "Second hook."})
	if err != nil {
		t.Fatalf("scaffold.New(hook2) error = %v", err)
	}
	hook2.Extra["events"] = []string{"PreToolUse"}
	hook2.Extra["command"] = "echo two"
	if err := hook2.Save(); err != nil {
		t.Fatalf("Save(hook2) error = %v", err)
	}
	hook2, err = artifact.Load(hook2.Dir, artifact.KindHook)
	if err != nil {
		t.Fatalf("Load(hook2) error = %v", err)
	}

	return []*artifact.Artifact{skill, agent, tool, hook1, hook2}
}

func TestExportBundle(t *testing.T) {
	members := newTestBundleMembers(t)
	outDir := t.TempDir()

	dest, err := (exporter{}).ExportBundle("demo-kit", "A demo kit.", members, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}
	if dest != filepath.Join(outDir, "demo-kit") {
		t.Fatalf("ExportBundle() dest = %q, want %s/demo-kit", dest, outDir)
	}

	var pm pluginManifest
	pmData, err := os.ReadFile(filepath.Join(dest, "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	if err := json.Unmarshal(pmData, &pm); err != nil {
		t.Fatalf("parsing plugin.json: %v", err)
	}
	if pm.Name != "demo-kit" {
		t.Errorf("plugin.json Name = %q, want demo-kit", pm.Name)
	}
	if pm.Description != "A demo kit." {
		t.Errorf("plugin.json Description = %q, want %q", pm.Description, "A demo kit.")
	}

	if _, err := os.Stat(filepath.Join(dest, "skills", "s1", "SKILL.md")); err != nil {
		t.Errorf("skills/s1/SKILL.md not written: %v", err)
	}

	agentData, err := os.ReadFile(filepath.Join(dest, "com.github.copilot", "agents", "a1.agent.md"))
	if err != nil {
		t.Fatalf("reading com.github.copilot/agents/a1.agent.md: %v", err)
	}
	if !strings.Contains(string(agentData), "name: a1") {
		t.Errorf("a1.agent.md missing name field, got:\n%s", agentData)
	}

	mcpData, err := os.ReadFile(filepath.Join(dest, "mcp.json"))
	if err != nil {
		t.Fatalf("reading mcp.json: %v", err)
	}
	var mcpFile mcpconfig.File
	if err := json.Unmarshal(mcpData, &mcpFile); err != nil {
		t.Fatalf("parsing mcp.json: %v", err)
	}
	if mcpFile.Schema != mcpSchema {
		t.Errorf("mcp.json Schema = %q, want %q", mcpFile.Schema, mcpSchema)
	}
	if got := mcpFile.MCPServers["t1"].Args[1]; got != "cd 'tools/t1' && python3 src/main.py" {
		t.Errorf("mcp.json t1 command = %q, want cd-wrapped command", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "tools", "t1")); err != nil {
		t.Errorf("tools/t1 dir not written: %v", err)
	}

	var doc copilotHooksDoc
	hooksData, err := os.ReadFile(filepath.Join(dest, "com.github.copilot", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("reading com.github.copilot/hooks/hooks.json: %v", err)
	}
	if err := json.Unmarshal(hooksData, &doc); err != nil {
		t.Fatalf("parsing hooks.json: %v", err)
	}
	entries := doc.Hooks["PreToolUse"]
	if len(entries) != 2 {
		t.Fatalf("hooks.json PreToolUse entries = %d, want 2 (appended, not overwritten)", len(entries))
	}
	var commands []string
	for _, e := range entries {
		commands = append(commands, e.Bash)
	}
	if !strings.Contains(strings.Join(commands, ","), "echo one") || !strings.Contains(strings.Join(commands, ","), "echo two") {
		t.Errorf("hooks.json commands = %v, want both echo one and echo two", commands)
	}
}

func TestExportBundleZip(t *testing.T) {
	members := newTestBundleMembers(t)
	dest, err := (exporter{}).ExportBundle("demo-kit", "A demo kit.", members, t.TempDir(), targets.ExportOptions{Zip: true})
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}
	if filepath.Ext(dest) != ".zip" {
		t.Fatalf("ExportBundle() with Zip=true dest = %q, want *.zip", dest)
	}
}
