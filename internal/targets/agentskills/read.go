package agentskills

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// IsSkillDir reports whether dir looks like a spec-compliant skill
// directory -- i.e. it has a SKILL.md at its root.
func IsSkillDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

// Read parses the SKILL.md at srcDir's root into an artifact -- the mirror
// of Write, so this package stays the one place that knows the Agent
// Skills format, in both directions. Dir is left unset; the caller decides
// where the artifact lands before calling Save(). Whatever the SKILL.md
// declared beyond name/description/license/compatibility rides along
// through Frontmatter.Extra automatically, since it's parsed with the same
// artifact.Parse every <kind>.md file uses.
func Read(srcDir string) (*artifact.Artifact, error) {
	path := filepath.Join(srcDir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	fm, body, err := artifact.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	fm.Kind = artifact.KindSkill // SKILL.md has no kind: field of its own
	return &artifact.Artifact{Frontmatter: fm, Body: body}, nil
}
