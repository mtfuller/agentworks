package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
)

// projectRoot resolves the AgentWorks project root from the global
// --project flag, walking upward the same way git finds a repo root.
func projectRoot() (string, error) {
	root, err := project.FindRoot(projectFlag)
	if err != nil {
		return "", fmt.Errorf("%w (run 'agentworks init' first, or pass --project)", err)
	}
	return root, nil
}

// loadArtifactAtPath loads the artifact rooted at path, which may either be
// an artifact's directory (e.g. "skills/demo") or a direct path to its
// <kind>.md manifest.
func loadArtifactAtPath(path string) (*artifact.Artifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !info.IsDir() {
		if !strings.HasSuffix(path, ".md") {
			return nil, fmt.Errorf("%s: expected an artifact directory or a <kind>.md file", path)
		}
		return artifact.LoadFile(path)
	}

	for _, k := range artifact.Kinds() {
		manifest := path + string(os.PathSeparator) + k.FileName()
		if _, err := os.Stat(manifest); err == nil {
			return artifact.Load(path, k)
		}
	}
	return nil, fmt.Errorf("%s: no agent.md/skill.md/tool.md/hook.md/workflow.md found", path)
}
