package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/lockfile"
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
	// Namespace overrides the namespace imported artifacts are filed under
	// (see Source.DefaultNamespace for what it defaults to). Imports are
	// always namespaced, so plugin-sourced artifacts stay distinguishable
	// from ones the user wrote themselves.
	Namespace string
}

// Plan is a fully-resolved, not-yet-written import: one or more artifacts
// ready to Save(), plus anything the source declared that isn't supported
// yet. Nothing is written to disk until Apply() is called, so a collision
// on artifact 4 of 5 never leaves the first 3 written with no way back.
type Plan struct {
	Source     Source
	PluginName string // "" for a bare-skill import
	// Commit is the exact GitHub commit the fetch resolved to ("" for an
	// archive URL, which doesn't name one). A branch or tag moves; this is
	// what was actually imported.
	Commit      string
	Artifacts   []*artifact.Artifact
	Unsupported []string // things in the source AgentWorks can't represent, e.g. a prompt-type hook
	// Warnings are things that were imported but need a human's attention: an
	// ignored field, a file a command references that wasn't in the plugin.
	Warnings []string

	// HashSources maps each artifact's (plan-computed) Dir to the raw
	// fetched location its content came from -- a directory for a skill,
	// a single file for an agent. Callers (see cmd/add.go, cmd/update.go)
	// content-hash this to pin exactly what was imported in
	// internal/lockfile, independent of AgentWorks' own
	// finalizeArtifact-applied renaming/description fallback.
	HashSources map[string]string
	// Subpaths maps each artifact's Dir to its path relative to this
	// Plan's Source root (e.g. "skills/foo", "agents/bar.md", or "" for a
	// bare single-skill import where the artifact IS the source root).
	// Recorded in the lockfile so `agentworks update` can find the same
	// spot again after a fresh Fetch of the same Source.
	Subpaths map[string]string

	tempDir   string
	extraTemp []string          // staging directories for MCP/hook artifacts, removed by Close
	srcDirs   map[string]string // artifact.Dir -> fetched dir to copy supporting files from
}

// newStage makes a scratch directory for an artifact that has no directory of
// its own in the fetched source (an MCP server or hook is an entry inside a
// JSON file): it holds the files that entry references, plus a marker of the
// raw entry, and is what gets hashed for the lockfile and copied into the
// project.
func (p *Plan) newStage() (string, error) {
	dir, err := os.MkdirTemp("", "agentworks-import-stage-*")
	if err != nil {
		return "", fmt.Errorf("creating staging dir: %w", err)
	}
	p.extraTemp = append(p.extraTemp, dir)
	return dir, nil
}

// addArtifact registers a staged artifact (see newStage). subpath is the
// virtual locator `agentworks update` uses to find it again in a fresh fetch,
// e.g. "mcp:db-server" or "hook:script:scripts/format.sh".
func (p *Plan) addArtifact(a *artifact.Artifact, stage, subpath string) {
	p.Artifacts = append(p.Artifacts, a)
	p.srcDirs[a.Dir] = stage
	p.HashSources[a.Dir] = stage
	p.Subpaths[a.Dir] = subpath
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
		fmt.Fprintf(&b, "  + %s %s -> %s\n", a.Kind, a.DisplayName(), a.Dir)
	}
	for _, u := range p.Unsupported {
		fmt.Fprintf(&b, "  ! not imported: %s\n", u)
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "  ~ %s\n", w)
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
			if err := filecopy.CopyDirExcept(src, a.Dir, "SKILL.md", a.Kind.FileName(), importMarker); err != nil {
				return err
			}
		}
	}
	return nil
}

// ApplyForce is Apply that replaces an artifact already on disk instead of
// refusing: for `agentworks add --force`, which re-imports over an existing
// artifact. Two planned artifacts resolving to the same directory is still an
// error, since that is a conflict inside the plan rather than with the
// project.
func (p *Plan) ApplyForce() error {
	seen := make(map[string]bool, len(p.Artifacts))
	for _, a := range p.Artifacts {
		if seen[a.Dir] {
			return fmt.Errorf("two artifacts both resolve to %s -- rename one and retry", a.Dir)
		}
		seen[a.Dir] = true
	}
	for _, a := range p.Artifacts {
		if err := p.Overwrite(a, a.Dir); err != nil {
			return err
		}
	}
	return nil
}

// RecordLockEntries pins what Apply actually wrote for every artifact in
// the plan into lf (an already-loaded internal/lockfile.Lockfile) -- a
// content hash of the plan's raw HashSources for each artifact, plus where
// it came from, so `agentworks update` has something to compare a future
// fetch against. Callers (cmd/add.go, the TUI marketplace import) still
// own lf.Save themselves, so a batch of several imports in a row only
// writes agentworks.lock once.
func (p *Plan) RecordLockEntries(root string, lf *lockfile.Lockfile) error {
	imported := time.Now().UTC().Format("2006-01-02")
	for _, a := range p.Artifacts {
		hash, err := lockfile.HashDir(p.HashSources[a.Dir])
		if err != nil {
			return err
		}
		key, err := lockfile.RelKey(root, a.Dir)
		if err != nil {
			return err
		}
		local, err := lockfile.HashDir(a.Dir)
		if err != nil {
			return err
		}
		lf.SetImport(key, lockfile.ImportEntry{
			Kind: string(a.Kind),
			Source: lockfile.SourceRef{
				Type: string(p.Source.Kind),
				Repo: p.Source.Repo,
				Ref:  p.Source.Ref,
				Path: p.Source.Path,
				URL:  p.Source.URL,
			},
			Commit:        p.Commit,
			SourceSubpath: p.Subpaths[a.Dir],
			ContentSHA256: hash,
			LocalSHA256:   local,
			Imported:      imported,
		})
	}
	return nil
}

// Close removes the plan's fetched temp directory. Safe to call on a Plan
// with no temp dir (e.g. one built directly in a test).
func (p *Plan) Close() error {
	var firstErr error
	for _, dir := range p.extraTemp {
		if err := os.RemoveAll(dir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if p.tempDir != "" {
		if err := os.RemoveAll(p.tempDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Overwrite writes exactly one artifact from the plan to dir, replacing
// whatever is already there. Unlike Apply, it has no collision check
// (replacing existing content is the point) and only ever touches dir --
// used by `agentworks update --apply` to refresh a single already-imported
// artifact without re-importing every sibling artifact a multi-artifact
// plugin source might also contain, which Apply's all-or-nothing collision
// check would otherwise reject wholesale (those siblings already exist on
// disk too, from the original import).
//
// a.Dir is a plan-computed directory (from Artifacts/HashSources/Subpaths)
// that may differ from dir (an update keeps an artifact at its existing
// local directory even if a fresh fetch would slugify it differently); the
// copy-source lookup keys off a's original Dir, so pass a exactly as it
// came from p.Artifacts.
func (p *Plan) Overwrite(a *artifact.Artifact, dir string) error {
	orig := a.Dir
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clearing %s: %w", dir, err)
	}
	a.Dir = dir
	// An artifact imported before imports were namespaced lives directly
	// under its kind directory; keep it there rather than moving it.
	if filepath.Base(filepath.Dir(dir)) == a.Kind.DirName() {
		a.Namespace = ""
	}
	if err := a.Validate(); err != nil {
		return err
	}
	if err := a.Save(); err != nil {
		return err
	}
	if src, ok := p.srcDirs[orig]; ok {
		if err := filecopy.CopyDirExcept(src, dir, "SKILL.md", a.Kind.FileName(), importMarker); err != nil {
			return err
		}
	}
	return nil
}

// detect decides what a fetched directory actually is and dispatches to
// the matching planner. It only looks at contentDir's root (after Fetch
// has already resolved any explicit ref/subpath) -- no recursive guessing
// through subdirectories.
func detect(root string, src Source, contentDir string, opts Options) (*Plan, error) {
	if claudecode.IsMarketplaceDir(contentDir) {
		return nil, fmt.Errorf("%s is a plugin marketplace, not a single plugin -- browse it with `agentworks tui`'s plugin browser (press \"p\"), or point at one of its plugins directly", src)
	}
	if claudecode.IsPluginDir(contentDir) {
		return planPlugin(root, src, contentDir, opts)
	}
	if agentskills.IsSkillDir(contentDir) {
		return planSkill(root, src, contentDir, opts)
	}
	return nil, fmt.Errorf("%s doesn't look like a skill (no SKILL.md) or a Claude Code plugin (no .claude-plugin/plugin.json) at its root", src)
}

// resolveNamespace picks the namespace an import is filed under: the
// caller's override if given (slugified), else src's default.
func resolveNamespace(src Source, override string) (string, error) {
	if override == "" {
		override = src.DefaultNamespace()
	}
	ns, err := slugify(strings.TrimPrefix(override, "@"))
	if err != nil {
		return "", fmt.Errorf("namespace for %s: %w", src, err)
	}
	return ns, nil
}

// finalizeArtifact fills in everything a freshly-Read artifact doesn't
// have yet: a valid slug name (real marketplace names routinely violate
// artifact.Validate's namePattern), a description fallback, its namespace,
// and a source: provenance block recording where it came from for a future
// `agentworks update`.
func finalizeArtifact(a *artifact.Artifact, nameOverride, namespace string, src Source) error {
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
	a.Namespace = namespace

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
