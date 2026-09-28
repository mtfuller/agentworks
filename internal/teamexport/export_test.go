package teamexport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

func TestRunRendersResolvedPlanForBothVendors(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	out := t.TempDir()
	outputs, err := Run(Request{Root: root, Team: "engineering", Targets: []string{TargetGitHubCopilot, TargetClaudeCode}, OutDir: out, Providers: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 2 {
		t.Fatalf("outputs=%#v", outputs)
	}
	claude := filepath.Join(out, TargetClaudeCode, "engineering")
	copilot := filepath.Join(out, TargetGitHubCopilot, "engineering")
	for _, path := range []string{
		filepath.Join(claude, ".claude-plugin", "plugin.json"), filepath.Join(claude, "agents", "implementer.md"),
		filepath.Join(claude, "skills", "testing", "SKILL.md"), filepath.Join(claude, ".mcp.json"),
		filepath.Join(copilot, "plugin.json"), filepath.Join(copilot, "com.github.copilot", "agents", "implementer.agent.md"),
		filepath.Join(copilot, "skills", "testing", "SKILL.md"), filepath.Join(copilot, "mcp.json"),
		filepath.Join(copilot, ".agentworks-plan.json"), filepath.Join(copilot, "AGENTS.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", path, err)
		}
	}
	var config mcpconfig.File
	data, err := os.ReadFile(filepath.Join(claude, ".mcp.json"))
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatalf("Claude MCP config: %v", err)
	}
	server := config.MCPServers["jira"]
	if server.Command != "sh" || len(server.Args) != 2 || !strings.Contains(server.Args[1], "${CLAUDE_PLUGIN_ROOT}/tools/jira") || server.Env["JIRA_TOKEN"] != "${JIRA_TOKEN}" {
		t.Fatalf("server=%#v", server)
	}
	validateAgentPluginFile(t, "plugin", filepath.Join(copilot, "plugin.json"))
	validateAgentPluginFile(t, "mcp", filepath.Join(copilot, "mcp.json"))
}

func validateAgentPluginFile(t *testing.T, schemaName, path string) {
	t.Helper()
	schemaPath := filepath.Join("..", "targets", "testdata", "agentplugins", schemaName+".schema.json")
	schemaFile, err := os.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(schemaFile)
	schemaFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	if schemaName == "plugin" {
		root := document.(map[string]any)
		name := root["properties"].(map[string]any)["name"].(map[string]any)
		if pattern, ok := name["pattern"].(string); ok {
			name["pattern"] = strings.Replace(pattern, `^(?!.*(?:--|\.\.))`, "^", 1)
			name["not"] = map[string]any{"pattern": `--|\.\.`}
		}
	}
	compiler := jsonschema.NewCompiler()
	url := "https://agent-plugins.org/schemas/1.0.0/" + schemaName + ".schema.json"
	if err := compiler.AddResource(url, document); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := jsonschema.UnmarshalJSON(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(instance); err != nil {
		t.Fatalf("%s violates Agent Plugins schema: %v", path, err)
	}
}

func TestRunRejectsUnsupportedTarget(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	outputs, err := Run(Request{Root: root, Team: "engineering", Targets: []string{"unknown"}, OutDir: t.TempDir()})
	if err == nil || len(outputs) != 0 {
		t.Fatalf("outputs=%#v err=%v", outputs, err)
	}
}
