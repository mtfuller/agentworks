package cursor

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// ruleFrontmatter is a Cursor project rule's .mdc frontmatter (see
// cursor.com/docs/rules). AlwaysApply is always false and Description is
// always set so the rule is agent-selected by relevance -- the same
// "discovered by description" model Agent Skills already uses -- rather
// than injected into every conversation regardless of relevance.
type ruleFrontmatter struct {
	Description string `yaml:"description"`
	AlwaysApply bool   `yaml:"alwaysApply"`
}

// exportSkill writes a Cursor project rule at
// outDir/.cursor/rules/<name>.mdc, plus the skill's own supporting files
// alongside it at outDir/.cursor/rules/<name>/ -- Cursor has no native
// Agent Skills format to consume those files with, but keeping them
// available next to the rule lets its instructions still reference them by
// the same relative paths the artifact's author wrote (e.g.
// "run scripts/main.py").
func exportSkill(a *artifact.Artifact, outDir string) (string, error) {
	rulesDir := filepath.Join(outDir, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", rulesDir, err)
	}

	fm := ruleFrontmatter{Description: a.Description, AlwaysApply: false}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("encoding %s.mdc frontmatter: %w", a.Name, err)
	}
	content := "---\n" + string(data) + "---\n\n" + a.Body
	rulePath := filepath.Join(rulesDir, a.Name+".mdc")
	if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", rulePath, err)
	}

	if err := filecopy.CopyArtifactFiles(a, filepath.Join(rulesDir, a.Name)); err != nil {
		return "", err
	}

	return rulePath, nil
}
