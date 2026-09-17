package claudecode

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// agentFrontmatter is a Claude Code subagent's minimal, documented
// frontmatter. Richer fields (model, effort, tools, ...) are human tuning
// AgentWorks has no real value for, so they're left out rather than
// invented.
type agentFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// exportAgent bundles a single agent into a real Claude Code plugin: a
// plugin.json plus one subagent file. For an agent's role inside a
// workflow bundle instead, see workflow.go -- both call
// writeClaudeAgentFile so the subagent file itself is identical either way.
func exportAgent(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writeClaudePluginManifest(pluginDir, a); err != nil {
		return "", err
	}
	if err := writeClaudeAgentFile(filepath.Join(pluginDir, "agents", a.Name+".md"), a); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}

func writeClaudeAgentFile(path string, a *artifact.Artifact) error {
	fm := agentFrontmatter{Name: a.Name, Description: a.Description}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("encoding %s frontmatter: %w", path, err)
	}
	content := "---\n" + string(data) + "---\n\n" + a.Body
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
