package geminicli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// exportSkill writes a Gemini CLI extension directory at outDir/<name>/:
// gemini-extension.json (name/version/description) + GEMINI.md (the
// skill's body, Gemini CLI's own persistent-context file) + the skill's
// supporting files. This is the closest real, distributable unit Gemini
// CLI has to a Claude Code plugin-wrapped skill -- Gemini CLI has no native
// Agent Skills/SKILL.md support.
func exportSkill(a *artifact.Artifact, outDir string) (string, error) {
	extDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(extDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", extDir, err)
	}

	if err := writeManifest(extDir, manifest{Name: a.Name, Version: a.Version, Description: a.Description}); err != nil {
		return "", err
	}

	geminiMD := filepath.Join(extDir, "GEMINI.md")
	if err := os.WriteFile(geminiMD, []byte(a.Body), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", geminiMD, err)
	}

	if err := filecopy.CopyArtifactFiles(a, extDir); err != nil {
		return "", err
	}

	return extDir, nil
}
