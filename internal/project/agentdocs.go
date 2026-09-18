package project

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// embeddedDocs holds the AGENTS.md template and the skills written into every
// new project. Edit the markdown under embedded/ directly; nothing here needs
// Go string escaping.
//
//go:embed embedded
var embeddedDocs embed.FS

const (
	agentsMDTemplatePath = "embedded/AGENTS.md"
	embeddedSkillsDir    = "embedded/skills"
)

// AgentSkillNames returns the name of every skill Init writes under
// .agents/skills/, in a stable order.
func AgentSkillNames() ([]string, error) {
	entries, err := fs.ReadDir(embeddedDocs, embeddedSkillsDir)
	if err != nil {
		return nil, fmt.Errorf("reading embedded skills: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// writeAgentDocs writes AGENTS.md and every embedded skill under
// .agents/skills/<name>/ into a freshly-initialized project, so a coding
// agent working in it immediately knows how to drive the agentworks CLI and
// author each artifact kind. It does not overwrite files already present, so
// re-running init-adjacent tooling never clobbers hand edits.
func writeAgentDocs(dir, name string) error {
	tmpl, err := embeddedDocs.ReadFile(agentsMDTemplatePath)
	if err != nil {
		return fmt.Errorf("reading embedded AGENTS.md: %w", err)
	}
	agentsMD := strings.ReplaceAll(string(tmpl), "{{project}}", name)
	if err := writeIfAbsent(filepath.Join(dir, "AGENTS.md"), agentsMD); err != nil {
		return err
	}

	destRoot := filepath.Join(dir, ".agents", "skills")
	return fs.WalkDir(embeddedDocs, embeddedSkillsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, embeddedSkillsDir), "/")
		if rel == "" {
			return nil
		}
		dest := filepath.Join(destRoot, filepath.FromSlash(rel))
		if d.IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", dest, err)
			}
			return nil
		}
		content, err := embeddedDocs.ReadFile(path.Clean(p))
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", p, err)
		}
		return writeIfAbsent(dest, string(content))
	})
}

func writeIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
