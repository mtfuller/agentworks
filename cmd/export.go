package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
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

		switch {
		case exportAll || exportKind != "":
			return runBulkExport(exporter)
		case len(args) > 1 || exportBundle != "":
			return runBundleExport(exporter, args)
		default:
			return runSingleExport(exporter, args[0])
		}
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

func runSingleExport(exporter targets.Exporter, path string) error {
	a, err := loadArtifactAtPath(path)
	if err != nil {
		return err
	}
	out, err := exporter.Export(a, exportOut, targets.ExportOptions{Zip: exportZip})
	if err != nil {
		return err
	}
	color.Success("Exported %s (%s) to %s for %s", a.Name, a.Kind, out, exportTarget)
	warnIfM365PlaceholderPublisher(a, out)
	return nil
}

// runBulkExport exports every artifact matching --all/--kind individually.
// A kind the target can't consume is skipped with a warning rather than
// aborting the whole run -- a project mixing skills and tools shouldn't
// make `--all --target chatgpt` fail outright.
func runBulkExport(exporter targets.Exporter) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
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
		out, err := exporter.Export(a, exportOut, targets.ExportOptions{Zip: exportZip})
		if err != nil {
			color.Error("%s (%s): %v", a.Name, a.Kind, err)
			failed++
			continue
		}
		color.Success("Exported %s (%s) to %s", a.Name, a.Kind, out)
		warnIfM365PlaceholderPublisher(a, out)
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
func runBundleExport(exporter targets.Exporter, paths []string) error {
	bundler, ok := exporter.(targets.BundleExporter)
	if !ok {
		return fmt.Errorf("%s doesn't support bundling several artifacts into one plugin -- export them individually instead", exportTarget)
	}
	if exportBundle == "" {
		return fmt.Errorf("bundling multiple artifacts needs a name: pass --bundle <name>")
	}

	members := make([]*artifact.Artifact, 0, len(paths))
	for _, p := range paths {
		a, err := loadArtifactAtPath(p)
		if err != nil {
			return err
		}
		members = append(members, a)
	}

	out, err := bundler.ExportBundle(exportBundle, bundleDescription(members), members, exportOut, targets.ExportOptions{Zip: exportZip})
	if err != nil {
		return err
	}
	color.Success("Exported bundle %s (%d artifacts) to %s for %s", exportBundle, len(members), out, exportTarget)
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
