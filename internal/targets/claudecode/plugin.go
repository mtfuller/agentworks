package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// pluginManifest is the (partial) Claude Code plugin.json shape -- only
// `name` is required by the schema; AgentWorks doesn't invent values for
// the rest (author, version, ...). Shared by every export path that
// produces a plugin: workflow, agent, and hook.
type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func writeClaudePluginManifest(pluginDir string, a *artifact.Artifact) error {
	dir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	m := pluginManifest{Name: a.Name, Description: a.Description}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding plugin.json: %w", err)
	}
	path := filepath.Join(dir, "plugin.json")
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
