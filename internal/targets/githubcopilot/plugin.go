package githubcopilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// pluginSchema is the constant $schema value the Agent Plugins spec
// requires every plugin.json to declare.
const pluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

// mcpSchema is the constant $schema value for an Agent Plugins mcp.json.
const mcpSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

// pluginManifest is the Agent Plugins plugin.json. The spec's schema is
// closed (no unknown fields), requires "$schema" and "name", and constrains
// the name (see targets.PluginName). AgentWorks fills version and the
// publisher fields from the project's agentworks.yaml (targets.PluginMeta)
// and leaves the rest to a human. Shared by every export path that produces
// a plugin: skill, mcp, agent, and hook.
type pluginManifest struct {
	Schema      string        `json:"$schema"`
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

// writePluginManifest is the common case: a plugin built from one artifact,
// named/described after it.
func writePluginManifest(pluginDir string, a *artifact.Artifact, meta targets.PluginMeta) error {
	return writePluginManifestNamed(pluginDir, a.Name, a.Description, meta.VersionFor(a.Version), meta)
}

// writePluginManifestNamed is the general form, for a bundle plugin that
// isn't tied to any single artifact's name/description.
func writePluginManifestNamed(pluginDir, name, description, version string, meta targets.PluginMeta) error {
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", pluginDir, err)
	}
	m := pluginManifest{
		Schema: pluginSchema, Name: targets.PluginName(name), Version: version, Description: description,
		Homepage: meta.Homepage, Repository: meta.Repository, License: meta.License,
	}
	if !meta.Author.IsZero() {
		m.Author = &pluginAuthor{Name: meta.Author.Name, Email: meta.Author.Email, URL: meta.Author.URL}
	}
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

// agentPluginsServers converts the shared server entries to Agent Plugins'
// dialect: its schema names the streamable HTTP transport "streamable-http",
// where Claude Code's own name for it is "http".
func agentPluginsServers(in map[string]mcpconfig.Server) map[string]mcpconfig.Server {
	out := make(map[string]mcpconfig.Server, len(in))
	for name, s := range in {
		if s.Type == mcpconfig.TransportHTTP {
			s.Type = "streamable-http"
		}
		out[name] = s
	}
	return out
}

func writeMCPFile(path string, file mcpconfig.File) error {
	file.MCPServers = agentPluginsServers(file.MCPServers)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
