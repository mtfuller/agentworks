package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/marketplace"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets"
)

// marketplaceGHSchema is the $schema value written into the GitHub Copilot
// marketplace.json, mirroring the agent-plugins.org plugin.schema.json /
// mcp.schema.json constants in internal/targets/githubcopilot/plugin.go.
// claude-code's marketplace.json gets no $schema, matching that its own
// plugin.json doesn't declare one either.
const marketplaceGHSchema = "https://agent-plugins.org/schemas/1.0.0/marketplace.schema.json"

// marketplaceTarget is one vendor this command knows how to publish a
// repo-level marketplace.json for. Only claude-code and github-copilot
// share the marketplace.json convention (see internal/marketplace's
// WellKnown) -- chatgpt has no such concept.
type marketplaceTarget struct {
	id           string
	manifestPath string // project-root-relative
	schema       string
}

var marketplaceTargets = []marketplaceTarget{
	{id: "claude-code", manifestPath: filepath.Join(".claude-plugin", "marketplace.json")},
	{id: "github-copilot", manifestPath: filepath.Join(".github", "plugin", "marketplace.json"), schema: marketplaceGHSchema},
}

var (
	marketplaceTargetFlag []string
	marketplaceSingle     bool
	marketplaceOut        string
	marketplaceCheck      bool
)

var marketplaceCmd = &cobra.Command{
	Use:   "marketplace",
	Short: "Publish this project's artifacts as a plugin marketplace repo",
	Long: `Turns an AgentWorks project into a repo a team can point Claude Code or
GitHub Copilot at directly: exports every eligible artifact into committed
plugin directories under --out (default: ./plugins), then writes a
marketplace.json listing them -- .claude-plugin/marketplace.json for
claude-code, .github/plugin/marketplace.json for github-copilot (see
"agentworks targets" for what each vendor otherwise supports).

Skills, agents, tools, and hooks are grouped into one bundled plugin per
namespace (unnamespaced artifacts land in one plugin named after the
project) -- pass --single to collapse all of them into one plugin
regardless of namespace.

Re-running this command regenerates --out and both marketplace.json files
from scratch, so it stays in sync as artifacts are added, removed, or
renamed. Exports are recorded in agentworks.lock exactly like "agentworks
export", so "agentworks status" also reports on them.

With --check nothing is written: the marketplace is regenerated into a
temporary directory and compared with what is committed, and the command
exits non-zero if either marketplace.json or any plugin directory differs.
Run it in CI to catch a source artifact changing without the committed
marketplace being republished.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		selected, err := resolveMarketplaceTargets(marketplaceTargetFlag)
		if err != nil {
			return err
		}

		root, err := projectRoot()
		if err != nil {
			return err
		}
		m, err := project.Load(root)
		if err != nil {
			return err
		}
		lf, err := lockfile.Load(root)
		if err != nil {
			return err
		}

		found, errs := project.Discover(root)
		for _, e := range errs {
			color.Warning("%v", e)
		}
		if len(found) == 0 {
			color.Info("No artifacts found -- nothing to publish.")
			return nil
		}

		pluginsRoot := marketplaceOut
		if !filepath.IsAbs(pluginsRoot) {
			pluginsRoot = filepath.Join(root, pluginsRoot)
		}

		if marketplaceCheck {
			return checkMarketplace(selected, found, m.Name, root, pluginsRoot)
		}

		for _, mt := range selected {
			if err := publishMarketplaceTarget(mt, found, m.Name, root, pluginsRoot, lf); err != nil {
				return err
			}
		}

		if err := lf.Save(root); err != nil {
			color.Warning("failed to update %s: %v", lockfile.FileName, err)
		}
		return nil
	},
}

// checkMarketplace regenerates the marketplace into a scratch directory
// laid out like the repo (same relative plugins/ and marketplace.json
// paths) and compares it with what's committed. The artifacts are still
// read from the real project; only the output destination differs, which is
// why publishMarketplaceTarget takes the output root separately and skips
// lockfile bookkeeping when handed no lockfile.
func checkMarketplace(selected []marketplaceTarget, found []*artifact.Artifact, projectName, root, pluginsRoot string) error {
	stage, err := os.MkdirTemp("", "agentworks-marketplace-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	stagePlugins := filepath.Join(stage, "plugins")
	items := []marketplaceCheckItem{}
	drifted := 0
	for _, mt := range selected {
		// Progress messages from the generator are noise for a check.
		if err := quietly(func() error {
			return publishMarketplaceTarget(mt, found, projectName, stage, stagePlugins, nil)
		}); err != nil {
			return err
		}

		// Both sides are compared as directory/file hashes: the plugin tree
		// for this target, and its marketplace.json.
		pairs := []struct{ label, want, have string }{
			{"plugins/" + mt.id, filepath.Join(stagePlugins, mt.id), filepath.Join(pluginsRoot, mt.id)},
			{filepath.ToSlash(mt.manifestPath), filepath.Join(stage, mt.manifestPath), filepath.Join(root, mt.manifestPath)},
		}
		for _, p := range pairs {
			status := compareTrees(p.want, p.have)
			if status != "in sync" {
				drifted++
				color.Error("%s: %s -- run 'agentworks marketplace' and commit the result", mt.id, p.label+" is "+status)
			} else {
				color.Success("%s: %s in sync", mt.id, p.label)
			}
			items = append(items, marketplaceCheckItem{Target: mt.id, Path: p.label, Status: status})
		}
	}

	var failErr error
	if drifted > 0 {
		failErr = fmt.Errorf("%d marketplace path(s) out of date", drifted)
	}
	if jsonFlag {
		if err := emitJSON(marketplaceCheckDoc{envelope: newEnvelope("marketplace", failErr == nil), Checked: items}); err != nil {
			return err
		}
	}
	return failErr
}

// compareTrees reports "in sync" when have (a file or directory) matches
// want byte for byte, "missing" when have doesn't exist, else "stale". want
// not existing means the generator produced nothing, so any have is stale.
func compareTrees(want, have string) string {
	wantHash, wantErr := lockfile.HashDir(want)
	haveHash, haveErr := lockfile.HashDir(have)
	switch {
	case wantErr != nil && haveErr != nil:
		return "in sync" // neither exists: nothing to publish, nothing committed
	case haveErr != nil:
		return "missing"
	case wantErr != nil || wantHash != haveHash:
		return "stale"
	}
	return "in sync"
}

type marketplaceCheckDoc struct {
	envelope
	Checked []marketplaceCheckItem `json:"checked"`
}

type marketplaceCheckItem struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	// Status is "in sync", "stale" (committed content differs from what the
	// source artifacts would generate), or "missing" (nothing committed).
	Status string `json:"status"`
}

func resolveMarketplaceTargets(requested []string) ([]marketplaceTarget, error) {
	if len(requested) == 0 {
		return marketplaceTargets, nil
	}
	var out []marketplaceTarget
	for _, id := range requested {
		found := false
		for _, mt := range marketplaceTargets {
			if mt.id == id {
				out = append(out, mt)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown marketplace target %q (want one of: claude-code, github-copilot)", id)
		}
	}
	return out, nil
}

// publishMarketplaceTarget regenerates one vendor's plugin output and
// marketplace.json from the current state of the project.
func publishMarketplaceTarget(mt marketplaceTarget, all []*artifact.Artifact, projectName, root, pluginsRoot string, lf *lockfile.Lockfile) error {
	exporter, err := targets.GetExporter(mt.id)
	if err != nil {
		return err
	}
	bundler, ok := exporter.(targets.BundleExporter)
	if !ok {
		return fmt.Errorf("%s doesn't support bundling artifacts into a plugin -- can't publish a marketplace for it", mt.id)
	}

	supported := filterByTarget(all, mt.id)
	if len(supported) == 0 {
		color.Warning("%s: no artifacts this target supports -- skipping", mt.id)
		return nil
	}
	groups := groupArtifacts(supported, marketplaceSingle, projectName)

	groupNames := make([]string, 0, len(groups))
	for name := range groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	// Warn about hand-edited output before anything gets cleared --
	// regenerating this target's whole plugin tree from scratch is this
	// command's job, but a hand-edit to a previous run's output is worth
	// flagging first, exactly like a plain `export` does. (lf is nil for
	// --check, which writes to a scratch directory and records nothing.)
	if lf != nil {
		for _, name := range groupNames {
			warnIfHandEdited(lf, mt.id, "bundle:"+name)
		}
	}

	targetOut := filepath.Join(pluginsRoot, mt.id)
	if err := os.RemoveAll(targetOut); err != nil {
		return fmt.Errorf("clearing %s: %w", targetOut, err)
	}

	var entries []marketplace.Entry
	for _, name := range groupNames {
		members := groups[name]
		for _, a := range members {
			warnSecurityRisk(a)
		}

		out, err := bundler.ExportBundle(name, bundleDescription(members), members, targetOut, targets.ExportOptions{})
		if err != nil {
			return fmt.Errorf("%s: bundling %q: %w", mt.id, name, err)
		}

		if lf != nil {
			memberDirs := make([]string, len(members))
			for i, a := range members {
				memberDirs[i] = a.Dir
			}
			if err := recordExport(root, lf, mt.id, "bundle:"+name, memberDirs, out); err != nil {
				color.Warning("exported successfully, but failed to record it in %s: %v", lockfile.FileName, err)
			}
		}

		src, err := marketplaceSourcePath(root, out)
		if err != nil {
			return err
		}
		entries = append(entries, marketplace.Entry{Name: name, Description: bundleDescription(members), Source: src})
		color.Success("%s: exported %s (%d artifact(s)) to %s", mt.id, name, len(members), out)
	}

	manifestPath := filepath.Join(root, mt.manifestPath)
	if err := marketplace.WriteManifest(manifestPath, mt.schema, projectName, entries); err != nil {
		return err
	}
	color.Success("%s: wrote %s (%d plugin(s))", mt.id, mt.manifestPath, len(entries))
	return nil
}

func filterByTarget(all []*artifact.Artifact, targetID string) []*artifact.Artifact {
	var out []*artifact.Artifact
	for _, a := range all {
		if !targets.Supports(targetID, a.Kind) {
			continue
		}
		if reason := targets.UnsupportedReason(targetID, a); reason != "" {
			color.Warning("%s: skipping %s (%s) -- %s", targetID, a.DisplayName(), a.Kind, reason)
			continue
		}
		out = append(out, a)
	}
	return out
}

// groupArtifacts buckets skill/agent/tool/hook artifacts into the plugin
// they'll be bundled into: one per namespace, with unnamespaced artifacts
// (and, when single is set, every artifact regardless of namespace)
// collapsed into one group named after the project.
func groupArtifacts(all []*artifact.Artifact, single bool, projectName string) map[string][]*artifact.Artifact {
	groups := map[string][]*artifact.Artifact{}
	for _, a := range all {
		key := projectName
		if !single && a.Namespace != "" {
			key = a.Namespace
		}
		groups[key] = append(groups[key], a)
	}
	return groups
}

// marketplaceSourcePath turns an export's output path into the
// project-root-relative form marketplace.json's bare-string source expects
// (see internal/marketplace's manifestEntry.resolve).
func marketplaceSourcePath(root, out string) (string, error) {
	absOut, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absOut)
	if err != nil {
		return "", err
	}
	return "./" + filepath.ToSlash(rel), nil
}

func init() {
	rootCmd.AddCommand(marketplaceCmd)
	marketplaceCmd.Flags().StringSliceVar(&marketplaceTargetFlag, "target", nil, "vendor target(s) to publish for (repeatable; default: claude-code and github-copilot)")
	marketplaceCmd.Flags().BoolVar(&marketplaceSingle, "single", false, "bundle every skill/agent/tool/hook into one plugin instead of grouping by namespace")
	marketplaceCmd.Flags().BoolVar(&marketplaceCheck, "check", false, "write nothing; exit non-zero if the committed plugins/ and marketplace.json differ from what the project would generate")
	marketplaceCmd.Flags().StringVar(&marketplaceOut, "out", "plugins", "directory plugin output is written to (committed, unlike export's --out dist)")
}
