package githubcopilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// pluginSchema is the constant $schema value the Agent Plugins spec
// requires every plugin.json to declare.
const pluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

// mcpSchema is the constant $schema value for an Agent Plugins mcp.json.
const mcpSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

// pluginManifest is the (partial) Agent Plugins plugin.json shape --
// AgentWorks only fills the fields it has real values for; the spec's other
// optional fields (author, homepage, license, ...) are left for a human to
// add if they want them. Shared by every export path that produces a
// plugin: skill, tool, agent, hook, and workflow.
type pluginManifest struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func writePluginManifest(pluginDir string, a *artifact.Artifact) error {
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", pluginDir, err)
	}
	m := pluginManifest{Schema: pluginSchema, Name: a.Name, Description: a.Description}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding plugin.json: %w", err)
	}
	path := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func writeMCPFile(path string, file mcpconfig.File) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
