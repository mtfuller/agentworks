// Package export is the one place a whole-project export is orchestrated:
// which artifacts go where, into which plugin(s) or archives, for which
// targets, and what gets recorded in agentworks.lock. Both `agentworks
// export` and the TUI's export form call Run, so the two never disagree
// about what an export is.
//
// Targets are a project-level concern (agentworks.yaml `targets:`), not
// something each artifact declares, and every plugin-format export bundles
// artifacts together -- see Format for the three shapes an export can take.
package export

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"

	// Every exporter registers itself with the targets registry from its
	// package's init(); importing them here means anything that can Run an
	// export can reach all of them.
	_ "github.com/mtfuller/agentworks/internal/targets/chatgpt"
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
	_ "github.com/mtfuller/agentworks/internal/targets/cursor"
	_ "github.com/mtfuller/agentworks/internal/targets/geminicli"
	_ "github.com/mtfuller/agentworks/internal/targets/githubcopilot"
)

// Format is the shape an export takes.
type Format string

const (
	// FormatPlugin bundles artifacts into vendor plugin(s), one set per
	// target -- a single plugin holding everything, or (with Namespaces) one
	// plugin per chosen namespace.
	FormatPlugin Format = "plugin"
	// FormatSkillsZip packs every skill into one .zip (a folder per skill at
	// the archive root). Vendor-neutral, so it needs no target.
	FormatSkillsZip Format = "skills.zip"
	// FormatSkillFiles writes each skill as its own .skill archive (a zip of
	// one skill folder, the form Claude uploads accept). Needs no target.
	FormatSkillFiles Format = "skill"
)

// UnnamespacedToken selects artifacts with no namespace (the ones the
// project's own author wrote) in Request.Namespaces.
const UnnamespacedToken = "."

// Request describes one export run.
type Request struct {
	Root        string
	ProjectName string
	// Targets are the vendor targets a FormatPlugin export writes for.
	// Callers resolve this from --target or the manifest (see
	// ResolveTargets); FormatSkillsZip/FormatSkillFiles ignore it.
	Targets []string
	OutDir  string
	Format  Format
	// Namespaces, for FormatPlugin, switches from one plugin holding
	// everything to one plugin per listed namespace (UnnamespacedToken for
	// the project's own artifacts). Artifacts in other namespaces are left
	// out. A leading "@" is accepted.
	Namespaces []string
	// Name names the single plugin/archive of a non-namespace export;
	// defaults to ProjectName.
	Name string
	// Zip additionally zips each plugin.
	Zip bool
	// Artifacts restricts the export to these; nil exports the whole project.
	Artifacts []*artifact.Artifact
}

// Output is one thing an export wrote.
type Output struct {
	Target  string // "" for the vendor-neutral skill formats
	Name    string
	Path    string
	Members int
}

// Result reports what Run did. Warnings never fail an export.
type Result struct {
	Outputs  []Output
	Warnings []string
}

// ResolveTargets returns the explicit targets if any were given, else the
// project manifest's, else an error saying how to set them.
func ResolveTargets(root string, explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	m, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	if len(m.Targets) == 0 {
		return nil, fmt.Errorf("no export targets: pass --target, or set `targets:` in %s (see 'agentworks targets' for the options)", project.ManifestFile)
	}
	return m.Targets, nil
}

// Run performs the export described by req, recording every output in lf
// (the caller loads and saves it). A failure exporting one group doesn't
// stop the rest; Run returns whatever succeeded plus an error naming what
// didn't.
func Run(req Request, lf *lockfile.Lockfile) (Result, error) {
	var res Result

	arts := req.Artifacts
	if arts == nil {
		found, errs := project.Discover(req.Root)
		for _, e := range errs {
			res.Warnings = append(res.Warnings, e.Error())
		}
		arts = found
	}
	if len(arts) == 0 {
		return res, fmt.Errorf("nothing to export: the project has no artifacts")
	}
	for _, a := range arts {
		for _, w := range a.LintSecurity() {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", w.Dir, w.Message))
		}
	}

	name := req.Name
	if name == "" {
		name = req.ProjectName
	}

	switch req.Format {
	case FormatSkillsZip, FormatSkillFiles:
		if len(req.Namespaces) > 0 {
			return res, fmt.Errorf("choosing namespaces only applies to plugin exports")
		}
		return res, exportSkills(req, name, arts, lf, &res)
	case FormatPlugin, "":
		return res, exportPlugins(req, name, arts, lf, &res)
	default:
		return res, fmt.Errorf("unknown export format %q", req.Format)
	}
}

// group is one plugin's worth of artifacts.
type group struct {
	name    string
	members []*artifact.Artifact
}

func planGroups(req Request, name string, arts []*artifact.Artifact) ([]group, error) {
	if len(req.Namespaces) == 0 {
		return []group{{name: name, members: arts}}, nil
	}

	var groups []group
	seen := map[string]bool{}
	for _, raw := range req.Namespaces {
		ns := strings.TrimPrefix(strings.TrimSpace(raw), "@")
		if ns == UnnamespacedToken || ns == "" {
			ns = ""
		}
		if seen[ns] {
			continue
		}
		seen[ns] = true

		g := group{name: ns}
		if ns == "" {
			g.name = req.ProjectName
		}
		for _, a := range arts {
			if a.Namespace == ns {
				g.members = append(g.members, a)
			}
		}
		if len(g.members) == 0 {
			return nil, fmt.Errorf("no artifacts in namespace %q (have: %s)", raw, strings.Join(Namespaces(arts), ", "))
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// Namespaces lists the namespaces present in arts, sorted, with
// UnnamespacedToken standing in for artifacts that have none.
func Namespaces(arts []*artifact.Artifact) []string {
	set := map[string]bool{}
	for _, a := range arts {
		if a.Namespace == "" {
			set[UnnamespacedToken] = true
		} else {
			set[a.Namespace] = true
		}
	}
	out := make([]string, 0, len(set))
	for ns := range set {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

func exportPlugins(req Request, name string, arts []*artifact.Artifact, lf *lockfile.Lockfile, res *Result) error {
	if len(req.Targets) == 0 {
		return fmt.Errorf("no export targets given")
	}
	groups, err := planGroups(req, name, arts)
	if err != nil {
		return err
	}

	var failures []string
	for _, target := range req.Targets {
		exporter, err := targets.GetExporter(target)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		targetOut := filepath.Join(req.OutDir, target)
		opts := targets.ExportOptions{Zip: req.Zip}
		bundler, canBundle := exporter.(targets.BundleExporter)

		for _, g := range groups {
			var bundleable, standalone []*artifact.Artifact
			for _, a := range g.members {
				switch {
				case !targets.Supports(target, a.Kind):
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s: skipping %s (%s) -- %s doesn't support this kind", target, a.DisplayName(), a.Kind, target))
				case targets.UnsupportedReason(target, a) != "":
					res.Warnings = append(res.Warnings, fmt.Sprintf("%s: skipping %s (%s) -- %s", target, a.DisplayName(), a.Kind, targets.UnsupportedReason(target, a)))
				default:
					bundleable = append(bundleable, a)
				}
			}

			switch {
			case len(bundleable) > 0 && canBundle:
				key := "bundle:" + g.name
				res.Warnings = append(res.Warnings, HandEditedWarning(lf, target, key)...)
				out, err := bundler.ExportBundle(g.name, BundleDescription(bundleable), bundleable, targetOut, opts)
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s: plugin %s: %v", target, g.name, err))
					break
				}
				res.Outputs = append(res.Outputs, Output{Target: target, Name: g.name, Path: out, Members: len(bundleable)})
				res.record(req.Root, lf, target, key, bundleable, out)

			case len(bundleable) > 0:
				// No plugin format for this vendor: fall back to one output
				// per artifact rather than refusing the whole export.
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s has no plugin format -- exporting each artifact on its own", target))
				standalone = append(standalone, bundleable...)
			}

			for _, a := range standalone {
				key, err := RootRelKey(req.Root, a.Dir)
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s: %s: %v", target, a.DisplayName(), err))
					continue
				}
				res.Warnings = append(res.Warnings, HandEditedWarning(lf, target, key)...)
				out, err := exporter.Export(a, targetOut, opts)
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s: %s: %v", target, a.DisplayName(), err))
					continue
				}
				res.Outputs = append(res.Outputs, Output{Target: target, Name: a.Name, Path: out, Members: 1})
				res.record(req.Root, lf, target, key, []*artifact.Artifact{a}, out)
			}
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%d export(s) failed:\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
	if len(res.Outputs) == 0 {
		return fmt.Errorf("nothing was exported: none of the selected artifacts are supported by %s", strings.Join(req.Targets, ", "))
	}
	return nil
}

func exportSkills(req Request, name string, arts []*artifact.Artifact, lf *lockfile.Lockfile, res *Result) error {
	var skills []*artifact.Artifact
	for _, a := range arts {
		if a.Kind == artifact.KindSkill {
			skills = append(skills, a)
		}
	}
	if len(skills) == 0 {
		return fmt.Errorf("no skills to export")
	}
	flat := targets.FlatNames(skills)

	stage, err := os.MkdirTemp("", "agentworks-skills-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.MkdirAll(req.OutDir, 0o755); err != nil {
		return err
	}

	if req.Format == FormatSkillsZip {
		for _, s := range skills {
			if err := agentskills.Write(s, filepath.Join(stage, flat[s])); err != nil {
				return fmt.Errorf("skill %s: %w", s.DisplayName(), err)
			}
		}
		zipPath := filepath.Join(req.OutDir, name+"-skills.zip")
		if _, err := filecopy.ZipDirTo(stage, zipPath, false); err != nil {
			return err
		}
		res.Outputs = append(res.Outputs, Output{Name: name + "-skills", Path: zipPath, Members: len(skills)})
		res.record(req.Root, lf, "skills.zip", "skills:"+name, skills, zipPath)
		return nil
	}

	skillsOut := filepath.Join(req.OutDir, "skills")
	if err := os.MkdirAll(skillsOut, 0o755); err != nil {
		return err
	}
	for _, s := range skills {
		dir := filepath.Join(stage, flat[s])
		if err := agentskills.Write(s, dir); err != nil {
			return fmt.Errorf("skill %s: %w", s.DisplayName(), err)
		}
		path := filepath.Join(skillsOut, flat[s]+".skill")
		if _, err := filecopy.ZipDirTo(dir, path, true); err != nil {
			return err
		}
		key, err := RootRelKey(req.Root, s.Dir)
		if err != nil {
			return err
		}
		res.Outputs = append(res.Outputs, Output{Name: flat[s], Path: path, Members: 1})
		res.record(req.Root, lf, "skill", key, []*artifact.Artifact{s}, path)
	}
	return nil
}

// record pins an output in the lockfile; a failure only warns, since the
// export itself already succeeded.
func (r *Result) record(root string, lf *lockfile.Lockfile, target, key string, members []*artifact.Artifact, out string) {
	dirs := make([]string, len(members))
	for i, m := range members {
		dirs[i] = m.Dir
	}
	if err := Record(root, lf, target, key, dirs, out); err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("exported %s, but couldn't record it in %s: %v", out, lockfile.FileName, err))
	}
}

// BundleDescription auto-generates a plugin.json description for a bundle,
// which isn't itself an artifact and so has no description of its own.
func BundleDescription(arts []*artifact.Artifact) string {
	names := make([]string, len(arts))
	for i, a := range arts {
		names[i] = a.DisplayName()
	}
	return fmt.Sprintf("Bundle of %d artifacts: %s", len(arts), strings.Join(names, ", "))
}

// RootRelKey turns an artifact directory (which may be relative to the
// working directory rather than root) into the project-root-relative form
// used as a lockfile key.
func RootRelKey(root, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return lockfile.RelKey(root, abs)
}

// HandEditedWarning compares an export's previously recorded output hash
// against what's on disk right now -- call it before the exporter clears
// its output. A mismatch means the output was hand-edited since the last
// export and this run will discard that edit.
func HandEditedWarning(lf *lockfile.Lockfile, target, artifactKey string) []string {
	entry, ok := lf.Exports[lockfile.ExportKey(target, artifactKey)]
	if !ok {
		return nil
	}
	current, err := lockfile.HashDir(entry.Output)
	if err != nil || current == entry.OutputSHA256 {
		return nil // nothing there yet, or untouched
	}
	return []string{fmt.Sprintf("%s was hand-edited since the last export -- this run will overwrite and discard those changes", entry.Output)}
}

// Record pins what an export just wrote: a combined hash of its source
// artifact dir(s) and a hash of its output, so a later run can tell "source
// changed, needs re-export" (agentworks status) from "output was
// hand-edited" (HandEditedWarning).
func Record(root string, lf *lockfile.Lockfile, target, artifactKey string, sourceDirs []string, out string) error {
	relDirs := make([]string, len(sourceDirs))
	for i, d := range sourceDirs {
		rel, err := RootRelKey(root, d)
		if err != nil {
			return err
		}
		relDirs[i] = rel
	}
	sourceHash, err := HashDirs(sourceDirs)
	if err != nil {
		return err
	}
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	outputHash, err := lockfile.HashDir(outAbs)
	if err != nil {
		return err
	}
	lf.SetExport(lockfile.ExportKey(target, artifactKey), lockfile.ExportEntry{
		Target:       target,
		Artifact:     strings.Join(relDirs, "+"),
		Output:       outAbs,
		SourceSHA256: sourceHash,
		OutputSHA256: outputHash,
		Exported:     time.Now().UTC().Format("2006-01-02"),
	})
	return nil
}

// HashDirs computes one combined content hash over one or more artifact
// directories, each hashed with lockfile.HashDir and folded together sorted
// by dir so the result doesn't depend on order.
func HashDirs(dirs []string) (string, error) {
	sorted := append([]string(nil), dirs...)
	sort.Strings(sorted)

	h := sha256.New()
	for _, dir := range sorted {
		dh, err := lockfile.HashDir(dir)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%s\n", dir, dh)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
