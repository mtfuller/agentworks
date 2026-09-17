package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
	"github.com/mtfuller/agentworks/internal/targets/workflowsteps"
)

// commandFrontmatter is a Claude Code slash command's frontmatter.
type commandFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// exportWorkflow bundles a workflow's referenced agents and tools into a
// real Claude Code plugin: a subagent file per agent step, one merged
// .mcp.json for every tool step, and a generated slash command that walks
// through the steps in order -- Claude Code's own agent loop does the
// actual orchestrating when someone runs it, AgentWorks doesn't execute
// anything itself.
func exportWorkflow(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	steps, err := workflowsteps.Resolve(a)
	if err != nil {
		return "", err
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}

	if err := writeClaudePluginManifest(pluginDir, a); err != nil {
		return "", err
	}

	mcpServers := map[string]mcpconfig.Server{}
	for _, s := range steps {
		switch s.Kind {
		case artifact.KindAgent:
			path := filepath.Join(pluginDir, "agents", s.Name+".md")
			if err := writeClaudeAgentFile(path, s.Artifact); err != nil {
				return "", err
			}
		case artifact.KindTool:
			server, err := mcpconfig.ServerFor(s.Artifact)
			if err != nil {
				return "", fmt.Errorf("workflow step %q: %w", s.Name, err)
			}
			mcpServers[s.Name] = server
		}
	}
	if len(mcpServers) > 0 {
		if err := writeMCPFile(filepath.Join(pluginDir, ".mcp.json"), mcpconfig.File{MCPServers: mcpServers}); err != nil {
			return "", err
		}
	}

	cmdPath := filepath.Join(pluginDir, "commands", a.Name+".md")
	if err := writeClaudeCommandFile(cmdPath, a, steps); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}

func writeClaudeCommandFile(path string, a *artifact.Artifact, steps []workflowsteps.Step) error {
	fm := commandFrontmatter{Name: a.Name, Description: a.Description}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("encoding %s frontmatter: %w", path, err)
	}

	var body strings.Builder
	body.WriteString(a.Body)
	if len(steps) > 0 {
		body.WriteString("\n\n## Steps\n\n")
		for i, s := range steps {
			switch s.Kind {
			case artifact.KindAgent:
				fmt.Fprintf(&body, "%d. Use @%s to: %s\n", i+1, s.Name, s.Artifact.Description)
			case artifact.KindTool:
				fmt.Fprintf(&body, "%d. Use the `%s` MCP tool to: %s\n", i+1, s.Name, s.Artifact.Description)
			}
		}
	}

	content := "---\n" + string(data) + "---\n\n" + strings.TrimLeft(body.String(), "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
