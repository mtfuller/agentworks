package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// pluginManifest is the Claude Code plugin.json shape. Only `name` is
// required; version and author are what `claude plugin validate` recommends
// (it warns without them), and the rest are optional publisher details. They
// come from the project's agentworks.yaml (targets.PluginMeta) -- AgentWorks
// doesn't invent values beyond a default version.
type pluginManifest struct {
	Name        string        `json:"name"`
	Version     string        `json:"version,omitempty"`
	Description string        `json:"description,omitempty"`
	Author      *pluginAuthor `json:"author,omitempty"`
	Homepage    string        `json:"homepage,omitempty"`
	Repository  string        `json:"repository,omitempty"`
	License     string        `json:"license,omitempty"`
}

type pluginAuthor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// pluginRootVar is what Claude Code expands to the plugin's install directory
// in hook commands and MCP server command/args/env.
const pluginRootVar = "${CLAUDE_PLUGIN_ROOT}"

// writeClaudePluginManifest is the common case: a plugin built from one
// artifact, named/described after it.
func writeClaudePluginManifest(pluginDir string, a *artifact.Artifact, meta targets.PluginMeta) error {
	return writeClaudePluginManifestNamed(pluginDir, a.Name, a.Description, meta.VersionFor(a.Version), meta)
}

// writeClaudePluginManifestNamed is the general form, for a bundle plugin
// that isn't tied to any single artifact's name/description.
func writeClaudePluginManifestNamed(pluginDir, name, description, version string, meta targets.PluginMeta) error {
	dir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	m := pluginManifest{
		Name: targets.PluginName(name), Version: version, Description: description,
		Homepage: meta.Homepage, Repository: meta.Repository, License: meta.License,
	}
	if !meta.Author.IsZero() {
		m.Author = &pluginAuthor{Name: meta.Author.Name, Email: meta.Author.Email, URL: meta.Author.URL}
	}
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
