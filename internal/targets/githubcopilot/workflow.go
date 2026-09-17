package githubcopilot

import (
	"encoding/json"
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

// agentFrontmatter is a Copilot custom agent's frontmatter. Public docs for
// com.github.copilot/*.agent.md don't give a full field table the way
// Claude Code's agents/*.md docs do (this corner of the Agent Plugins spec
// is newer and less documented) -- name/description + body is the one
// concretely confirmed shape, so that's what AgentWorks generates rather
// than guessing at unconfirmed fields like model/tools.
type agentFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// commandFrontmatter is a Copilot custom command's frontmatter, following
// the same documented-fields-only policy as agentFrontmatter above.
type commandFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// exportWorkflow bundles a workflow's referenced agents and tools into an
// Agent Plugin: a custom agent file per agent step under
// com.github.copilot/agents/, one merged mcp.json for every tool step, and
// a generated command under com.github.copilot/commands/ that walks
// through the steps in order.
func exportWorkflow(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	steps, err := workflowsteps.Resolve(a)
	if err != nil {
		return "", err
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writePluginManifest(pluginDir, a); err != nil {
		return "", err
	}

	copilotDir := filepath.Join(pluginDir, "com.github.copilot")
	mcpServers := map[string]mcpconfig.Server{}
	for _, s := range steps {
		switch s.Kind {
		case artifact.KindAgent:
			path := filepath.Join(copilotDir, "agents", s.Name+".agent.md")
			if err := writeCopilotAgentFile(path, s.Artifact); err != nil {
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
		file := mcpconfig.File{Schema: mcpSchema, MCPServers: mcpServers}
		if err := writeMCPFile(filepath.Join(pluginDir, "mcp.json"), file); err != nil {
			return "", err
		}
	}

	cmdPath := filepath.Join(copilotDir, "commands", a.Name+".md")
	if err := writeCopilotCommandFile(cmdPath, a, steps); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}

func writeCopilotAgentFile(path string, a *artifact.Artifact) error {
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

func writeCopilotCommandFile(path string, a *artifact.Artifact, steps []workflowsteps.Step) error {
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
				fmt.Fprintf(&body, "%d. Invoke the %q custom agent to: %s\n", i+1, s.Name, s.Artifact.Description)
			case artifact.KindTool:
				fmt.Fprintf(&body, "%d. Use the %q MCP tool to: %s\n", i+1, s.Name, s.Artifact.Description)
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
