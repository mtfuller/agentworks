package importer

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
	"github.com/mtfuller/agentworks/internal/targets/claudecode"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// Options customizes a Prepare call.
type Options struct {
	// Name overrides the derived (slugified) artifact name. Only valid
	// when importing a single bare skill -- a multi-artifact plugin import
	// can't apply one override name to N artifacts.
	Name string
}

// Plan is a fully-resolved, not-yet-written import: one or more artifacts
// ready to Save(), plus anything the source declared that isn't supported
// yet. Nothing is written to disk until Apply() is called, so a collision
// on artifact 4 of 5 never leaves the first 3 written with no way back.
type Plan struct {
	Source      Source
	PluginName  string // "" for a bare-skill import
	Artifacts   []*artifact.Artifact
	Unsupported []string // e.g. "hooks/hooks.json (hook import not supported yet)"

	tempDir string
	srcDirs map[string]string // artifact.Dir -> fetched dir to copy supporting files from
}

// Describe renders a human-readable summary of what Apply would do.
func (p *Plan) Describe() string {
	var b strings.Builder
	if p.PluginName != "" {
		fmt.Fprintf(&b, "Plugin %q from %s:\n", p.PluginName, p.Source)
	} else {
		fmt.Fprintf(&b, "From %s:\n", p.Source)
	}
	for _, a := range p.Artifacts {
		fmt.Fprintf(&b, "  + %s %s -> %s\n", a.Kind, a.Name, a.Dir)
	}
	for _, u := range p.Unsupported {
		fmt.Fprintf(&b, "  ! %s\n", u)
	}
	return b.String()
}

// Apply writes every planned artifact to disk. It collision-checks every
// destination directory (against both the filesystem and other artifacts
// in this same plan) before writing any of them, so an import is
// all-or-nothing.
func (p *Plan) Apply() error {
	seen := make(map[string]bool, len(p.Artifacts))
	for _, a := range p.Artifacts {
		if seen[a.Dir] {
			return fmt.Errorf("two artifacts both resolve to %s -- rename one and retry", a.Dir)
		}
		seen[a.Dir] = true
		if _, err := os.Stat(a.Dir); err == nil {
			return fmt.Errorf("%s already exists", a.Dir)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	for _, a := range p.Artifacts {
		if err := a.Validate(); err != nil {
			return err
		}
		if err := a.Save(); err != nil {
			return err
		}
		if src, ok := p.srcDirs[a.Dir]; ok {
			if err := filecopy.CopyDirExcept(src, a.Dir, "SKILL.md", a.Kind.FileName()); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close removes the plan's fetched temp directory. Safe to call on a Plan
// with no temp dir (e.g. one built directly in a test).
func (p *Plan) Close() error {
	if p.tempDir == "" {
		return nil
	}
	return os.RemoveAll(p.tempDir)
}

// detect decides what a fetched directory actually is and dispatches to
// the matching planner. It only looks at contentDir's root (after Fetch
// has already resolved any explicit ref/subpath) -- no recursive guessing
// through subdirectories.
func detect(root string, src Source, contentDir string, opts Options) (*Plan, error) {
	if claudecode.IsMarketplaceDir(contentDir) {
		return nil, fmt.Errorf("%s is a plugin marketplace, not a single plugin -- browse it with `agentworks tui`'s marketplace search (press \"a\"), or point at one of its plugins directly", src)
	}
	if claudecode.IsPluginDir(contentDir) {
		return planPlugin(root, src, contentDir, opts)
	}
	if agentskills.IsSkillDir(contentDir) {
		return planSkill(root, src, contentDir, opts)
	}
	return nil, fmt.Errorf("%s doesn't look like a skill (no SKILL.md) or a Claude Code plugin (no .claude-plugin/plugin.json) at its root", src)
}

// finalizeArtifact fills in everything a freshly-Read artifact doesn't
// have yet: a valid slug name (real marketplace names routinely violate
// artifact.Validate's namePattern), a description fallback, this project's
// default targets, and a source: provenance block recording where it came
// from for a future `agentworks update`.
func finalizeArtifact(root string, a *artifact.Artifact, nameOverride string, src Source) error {
	original := a.Name
	name := nameOverride
	if name == "" {
		name = a.Name
	}
	slug, err := slugify(name)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	a.Name = slug

	if a.Description == "" {
		a.Description = "Imported from " + src.String()
	}
	if len(a.Targets) == 0 {
		if m, err := project.Load(root); err == nil {
			a.Targets = m.Targets
		}
	}

	if a.Extra == nil {
		a.Extra = map[string]any{}
	}
	provenance := map[string]any{
		"url":      src.String(),
		"imported": time.Now().UTC().Format("2006-01-02"),
	}
	if src.Kind == SourceGitHub {
		provenance["ref"] = src.Ref
		if src.Path != "" {
			provenance["path"] = src.Path
		}
	}
	if slug != original {
		provenance["name"] = original
	}
	a.Extra["source"] = provenance
	return nil
}
