package geminicli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/agentcaps"
)

// agentFrontmatter is a Gemini CLI subagent's frontmatter (see
// geminicli.com/docs/core/subagents/). Tools/Model are populated from an
// agent artifact's vendor-agnostic "tools"/"model" fields via
// agentcaps.ForGeminiCLI; left empty (omitempty), Gemini CLI's own default
// applies (inherit every parent tool, resolve "inherit" for the model).
type agentFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools,omitempty"`
	Model       string   `yaml:"model,omitempty"`
}

// exportAgent writes a Gemini CLI subagent file at
// outDir/.gemini/agents/<name>.md.
func exportAgent(a *artifact.Artifact, outDir string) (string, error) {
	agentsDir := filepath.Join(outDir, ".gemini", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", agentsDir, err)
	}

	tools, model := agentcaps.ForGeminiCLI(a.ExtraStringSlice("tools"), a.ExtraString("model"))
	fm := agentFrontmatter{Name: a.Name, Description: a.Description, Tools: tools, Model: model}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("encoding %s.md frontmatter: %w", a.Name, err)
	}
	content := "---\n" + string(data) + "---\n\n" + a.BodyWithRequirements()
	path := filepath.Join(agentsDir, a.Name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
