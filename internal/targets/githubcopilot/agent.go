package githubcopilot

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// agentFrontmatter is a Copilot custom agent's frontmatter. Public docs for
// com.github.copilot/*.agent.md don't give a full field table the way
// Claude Code's agents/*.md docs do (this corner of the Agent Plugins spec
// is newer and less documented) -- name/description + body is the one
// concretely confirmed shape, so that's what AgentWorks generates rather
// than guessing at unconfirmed fields like model/tools.
//
// Revisited alongside AgentWorks' agentcaps package (which maps a
// vendor-agnostic "tools"/"model" to Claude Code's confirmed subagent
// fields and Microsoft 365's confirmed declarative-agent capabilities):
// still deferred here for the same reason as above, not overlooked. Same
// treatment as the ChatGPT decision in AGENTS.md -- re-open once a
// confirmed spec exists, don't guess at one now.
type agentFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// exportAgent bundles a single agent into an Agent Plugin: a plugin.json
// plus one custom agent file under com.github.copilot/agents/. For an
// agent's role inside a workflow bundle instead, see workflow.go -- both
// call writeCopilotAgentFile so the agent file itself is identical either
// way.
func exportAgent(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writePluginManifest(pluginDir, a, opts.Meta); err != nil {
		return "", err
	}
	path := filepath.Join(pluginDir, "com.github.copilot", "agents", a.Name+".agent.md")
	if err := writeCopilotAgentFile(path, a); err != nil {
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
	content := "---\n" + string(data) + "---\n\n" + a.BodyWithRequirements()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
