// Package githubcopilot implements the "github-copilot" export target: it
// wraps a skill in a minimal Agent Plugin (https://agent-plugins.org), the
// open plugin format GitHub Copilot adopted in its "Agent Plugins 1.0"
// release -- a plugin.json manifest plus one Agent Skills directory (see
// internal/targets/agentskills) under skills/<name>/.
package githubcopilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
)

func init() {
	targets.Register(skillExporter{})
}

const TargetID = "github-copilot"

// pluginSchema is the constant $schema value the Agent Plugins spec
// requires every plugin.json to declare.
const pluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

// pluginManifest is the (partial) Agent Plugins plugin.json shape --
// AgentWorks only fills the fields it has real values for; the spec's other
// optional fields (author, homepage, license, ...) are left for a human to
// add if they want them.
type pluginManifest struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type skillExporter struct{}

func (skillExporter) TargetID() string { return TargetID }

func (skillExporter) Export(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	if a.Kind != artifact.KindSkill {
		return "", fmt.Errorf("github-copilot export only supports skills right now (got %s)", a.Kind)
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}

	if err := writePluginManifest(pluginDir, a); err != nil {
		return "", err
	}
	// Per the Agent Plugins spec, skills live under skills/<name>/.
	if err := agentskills.Write(a, filepath.Join(pluginDir, "skills", a.Name)); err != nil {
		return "", err
	}

	if opts.Zip {
		return agentskills.Zip(pluginDir)
	}
	return pluginDir, nil
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
