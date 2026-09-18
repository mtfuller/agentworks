package cursor

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// agentFrontmatter is a Cursor subagent's frontmatter (see
// cursor.com/docs/subagents). Only name/description are populated --
// Cursor's docs don't confirm a "tools:" allowlist field on subagents at
// all, and "model:" only accepts "inherit" or a literal, fast-moving model
// ID with no documented stable tier alias (unlike Claude Code's
// haiku/sonnet/opus, or Gemini CLI's "-latest" aliases), so there's nothing
// honest to map AgentWorks' tools/model vocabulary to yet -- the same
// "don't guess a mapping the vendor hasn't confirmed" call already made for
// GitHub Copilot's agent export (see internal/targets/githubcopilot/agent.go).
type agentFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// exportAgent writes a Cursor subagent file at
// outDir/.cursor/agents/<name>.md.
func exportAgent(a *artifact.Artifact, outDir string) (string, error) {
	agentsDir := filepath.Join(outDir, ".cursor", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", agentsDir, err)
	}

	fm := agentFrontmatter{Name: a.Name, Description: a.Description}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("encoding %s.md frontmatter: %w", a.Name, err)
	}
	content := "---\n" + string(data) + "---\n\n" + a.Body
	path := filepath.Join(agentsDir, a.Name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
