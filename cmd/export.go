package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/m365copilot"

	_ "github.com/mtfuller/agentworks/internal/targets/chatgpt"
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
	_ "github.com/mtfuller/agentworks/internal/targets/githubcopilot"
)

var (
	exportTarget string
	exportOut    string
	exportZip    bool
	exportAll    bool
	exportKind   string
	exportBundle string
)

var exportCmd = &cobra.Command{
	Use:   "export [path...]",
	Short: "Export one or more artifacts to a vendor target's native format",
	Long: `Transform artifacts into a vendor's native format under --out (default:
./dist). Run "agentworks targets" to see which vendor targets have a real
exporter implemented yet.

  agentworks export skills/demo --target claude-code
      Export a single artifact (today's default behavior).

  agentworks export --all --target claude-code
  agentworks export --kind tool --target claude-code
      Export every artifact in the project, or every artifact of one kind,
      each to its own output. Kinds the target can't consume are skipped
      with a warning rather than failing the whole run.

  agentworks export skills/s1 tools/t1 --target claude-code --bundle my-kit
      Package several artifacts into one plugin instead of one per
      artifact. Only targets whose native format bundles components
      together (claude-code, github-copilot) support this.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateExportFlags(args, exportAll, exportKind, exportBundle); err != nil {
			return err
		}

		exporter, err := targets.GetExporter(exportTarget)
		if err != nil {
			return err
		}

		root, err := projectRoot()
		if err != nil {
			return err
		}
		lf, err := lockfile.Load(root)
		if err != nil {
			return err
		}

		var runErr error
		switch {
		case exportAll || exportKind != "":
			runErr = runBulkExport(exporter, root, lf)
		case len(args) > 1 || exportBundle != "":
			runErr = runBundleExport(exporter, args, root, lf)
		default:
			runErr = runSingleExport(exporter, args[0], root, lf)
		}

		if err := lf.Save(root); err != nil {
			color.Warning("failed to update %s: %v", lockfile.FileName, err)
		}
		return runErr
	},
}

// validateExportFlags checks the flag/arg combinations that don't make
// sense together, before anything looks at the project or the target.
func validateExportFlags(args []string, all bool, kindFlag, bundle string) error {
	if all && kindFlag != "" {
		return fmt.Errorf("--all and --kind are mutually exclusive")
	}
	if (all || kindFlag != "") && len(args) > 0 {
		return fmt.Errorf("--all/--kind export every matching artifact in the project -- they can't be combined with a path")
	}
	if !all && kindFlag == "" && len(args) == 0 {
		return fmt.Errorf("nothing to export: pass a path, or use --all/--kind to export the whole project")
	}
	return nil
}

func runSingleExport(exporter targets.Exporter, path string, root string, lf *lockfile.Lockfile) error {
	a, err := loadArtifactAtPath(path)
	if err != nil {
		return err
	}
	warnSecurityRisk(a)

	artifactKey, err := rootRelKey(root, a.Dir)
	if err != nil {
		return err
	}
	warnIfHandEdited(lf, exportTarget, artifactKey)

	out, err := exporter.Export(a, exportOut, targets.ExportOptions{Zip: exportZip})
	if err != nil {
		return err
	}
	color.Success("Exported %s (%s) to %s for %s", a.Name, a.Kind, out, exportTarget)
	warnIfM365PlaceholderPublisher(a, out)

	if err := recordExport(root, lf, exportTarget, artifactKey, []string{a.Dir}, out); err != nil {
		color.Warning("exported successfully, but failed to record it in %s: %v", lockfile.FileName, err)
	}
	return nil
}

// runBulkExport exports every artifact matching --all/--kind individually.
// A kind the target can't consume is skipped with a warning rather than
// aborting the whole run -- a project mixing skills and tools shouldn't
// make `--all --target chatgpt` fail outright.
func runBulkExport(exporter targets.Exporter, root string, lf *lockfile.Lockfile) error {
	kinds, err := resolveBulkKinds(exportKind)
	if err != nil {
		return err
	}

	found, errs := project.Discover(root, kinds...)
	for _, e := range errs {
		color.Warning("%v", e)
	}

	var exported, skipped, failed int
	for _, a := range found {
		if !targets.Supports(exportTarget, a.Kind) {
			color.Warning("skipping %s (%s): %s doesn't support this kind", a.Name, a.Kind, exportTarget)
			skipped++
			continue
		}
		warnSecurityRisk(a)

		artifactKey, err := rootRelKey(root, a.Dir)
		if err != nil {
			color.Error("%s (%s): %v", a.Name, a.Kind, err)
			failed++
			continue
		}
		warnIfHandEdited(lf, exportTarget, artifactKey)

		out, err := exporter.Export(a, exportOut, targets.ExportOptions{Zip: exportZip})
		if err != nil {
			color.Error("%s (%s): %v", a.Name, a.Kind, err)
			failed++
			continue
		}
		color.Success("Exported %s (%s) to %s", a.Name, a.Kind, out)
		warnIfM365PlaceholderPublisher(a, out)
		if err := recordExport(root, lf, exportTarget, artifactKey, []string{a.Dir}, out); err != nil {
			color.Warning("exported successfully, but failed to record it in %s: %v", lockfile.FileName, err)
		}
		exported++
	}

	color.Info("%d exported, %d skipped, %d failed", exported, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d artifact(s) failed to export", failed)
	}
	return nil
}

func resolveBulkKinds(kindFlag string) ([]artifact.Kind, error) {
	if kindFlag == "" {
		return nil, nil
	}
	k, err := artifact.ParseKind(kindFlag)
	if err != nil {
		return nil, err
	}
	return []artifact.Kind{k}, nil
}

// runBundleExport packages several artifacts into one plugin via the
// target's optional BundleExporter capability.
func runBundleExport(exporter targets.Exporter, paths []string, root string, lf *lockfile.Lockfile) error {
	bundler, ok := exporter.(targets.BundleExporter)
	if !ok {
		return fmt.Errorf("%s doesn't support bundling several artifacts into one plugin -- export them individually instead", exportTarget)
	}
	if exportBundle == "" {
		return fmt.Errorf("bundling multiple artifacts needs a name: pass --bundle <name>")
	}

	members := make([]*artifact.Artifact, 0, len(paths))
	memberDirs := make([]string, 0, len(paths))
	for _, p := range paths {
		a, err := loadArtifactAtPath(p)
		if err != nil {
			return err
		}
		warnSecurityRisk(a)
		members = append(members, a)
		memberDirs = append(memberDirs, a.Dir)
	}

	artifactKey := "bundle:" + exportBundle
	warnIfHandEdited(lf, exportTarget, artifactKey)

	out, err := bundler.ExportBundle(exportBundle, bundleDescription(members), members, exportOut, targets.ExportOptions{Zip: exportZip})
	if err != nil {
		return err
	}
	color.Success("Exported bundle %s (%d artifacts) to %s for %s", exportBundle, len(members), out, exportTarget)

	if err := recordExport(root, lf, exportTarget, artifactKey, memberDirs, out); err != nil {
		color.Warning("exported successfully, but failed to record it in %s: %v", lockfile.FileName, err)
	}
	return nil
}

// bundleDescription auto-generates a bundle's plugin.json description --
// keeps the CLI surface to one new flag (--bundle) instead of also
// requiring a description for something that isn't itself an artifact.
func bundleDescription(artifacts []*artifact.Artifact) string {
	names := make([]string, len(artifacts))
	for i, a := range artifacts {
		names[i] = a.Name
	}
	return fmt.Sprintf("Bundle of %d artifacts: %s", len(artifacts), strings.Join(names, ", "))
}

func warnIfM365PlaceholderPublisher(a *artifact.Artifact, out string) {
	if exportTarget == m365copilot.TargetID && m365copilot.UsesPlaceholderPublisher(a) {
		color.Warning("manifest.json inside %s has placeholder developer/privacy/terms URLs -- set a `publisher:` block in agentworks.yaml, or edit them directly before submitting to AppSource", out)
	}
}

// warnSecurityRisk surfaces a's LintSecurity warnings (a hook/tool command
// that will run arbitrary shell code) at export time -- a visibility fix,
// not a gate: unlike `agentworks add` writing new content from an
// untrusted remote source, exporting is always regenerable, so this never
// blocks the export.
func warnSecurityRisk(a *artifact.Artifact) {
	for _, w := range a.LintSecurity() {
		color.Warning("%s: %s", w.Dir, w.Message)
	}
}

// rootRelKey turns an artifact directory (as loadArtifactAtPath/
// project.Discover hand it back, which may be relative to the working
// directory rather than root) into the project-root-relative form used as
// a lockfile key, regardless of the relationship between the working
// directory and root.
func rootRelKey(root, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return lockfile.RelKey(root, abs)
}

// warnIfHandEdited compares an export's previously recorded output hash
// against what's actually on disk right now, before this run's exporter
// overwrites it (every exporter clears its output directory as its first
// step, so this has to happen before calling Export) -- a mismatch means
// dist was hand-edited since the last export and this run will discard
// that edit.
func warnIfHandEdited(lf *lockfile.Lockfile, target, artifactKey string) {
	entry, ok := lf.Exports[lockfile.ExportKey(target, artifactKey)]
	if !ok {
		return
	}
	currentHash, err := lockfile.HashDir(entry.Output)
	if err != nil {
		return // nothing there yet (e.g. moved/removed by hand) -- exporter will just create it
	}
	if currentHash == entry.OutputSHA256 {
		return
	}
	color.Warning("%s was hand-edited since the last export -- this run will overwrite and discard those changes", entry.Output)
}

// recordExport pins what an export just wrote: a combined hash of its
// source artifact dir(s) and a hash of the output it produced, so a future
// export can tell "source changed, needs re-export" (agentworks status)
// apart from "output was hand-edited" (warnIfHandEdited). sourceDirs are
// stored root-relative (like Imports' keys), so `agentworks status`'s path
// filter and display are stable regardless of the working directory an
// export ran from.
func recordExport(root string, lf *lockfile.Lockfile, target, artifactKey string, sourceDirs []string, out string) error {
	relDirs := make([]string, len(sourceDirs))
	for i, d := range sourceDirs {
		rel, err := rootRelKey(root, d)
		if err != nil {
			return err
		}
		relDirs[i] = rel
	}

	sourceHash, err := hashArtifactDirs(sourceDirs)
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

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportTarget, "target", "", "vendor target to export to (see 'agentworks targets')")
	exportCmd.Flags().StringVar(&exportOut, "out", "dist", "output directory")
	exportCmd.Flags().BoolVar(&exportZip, "zip", false, "also package the export as a .zip")
	exportCmd.Flags().BoolVar(&exportAll, "all", false, "export every artifact in the project")
	exportCmd.Flags().StringVar(&exportKind, "kind", "", "export every artifact of this kind (agent/skill/tool/hook/workflow)")
	exportCmd.Flags().StringVar(&exportBundle, "bundle", "", "bundle multiple artifacts into one plugin under this name")
	_ = exportCmd.MarkFlagRequired("target")
}
