package importer

import (
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
)

// planSkill builds a single-artifact Plan for a bare Agent Skill (a
// directory whose root is SKILL.md).
func planSkill(root string, src Source, contentDir string, opts Options) (*Plan, error) {
	a, err := agentskills.Read(contentDir)
	if err != nil {
		return nil, err
	}
	if err := finalizeArtifact(root, a, opts.Name, src); err != nil {
		return nil, err
	}
	a.Dir = filepath.Join(root, artifact.KindSkill.DirName(), a.Name)

	return &Plan{
		Source:    src,
		Artifacts: []*artifact.Artifact{a},
		srcDirs:   map[string]string{a.Dir: contentDir},
	}, nil
}
