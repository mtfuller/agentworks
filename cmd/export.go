package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/export"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/project"
)

var (
	exportTargets    []string
	exportOut        string
	exportZip        bool
	exportFormat     string
	exportNamespaces []string
	exportName       string
	exportNoBuild    bool
)

var exportCmd = &cobra.Command{
	Use:   "export [path...]",
	Short: "Export the project as plugins, or its skills as .zip/.skill files",
	Long: `Export bundles the project's artifacts into a plugin for each target under
--out (default: ./dist/<target>/). Targets come from the "targets:" list in
agentworks.yaml; pass --target (repeatable) to override it for one run.
"agentworks targets" shows what each vendor supports.

  agentworks export
      Everything in one plugin, named after the project.

  agentworks export --namespace obra --namespace .
      One plugin per listed namespace -- "obra" is what "agentworks add
      obra/superpowers" files its skills under, and "." is your own
      un-namespaced artifacts. Other namespaces are left out.

  agentworks export --format skills.zip
      Every skill in one .zip (a folder per skill).

  agentworks export --format skill
      Every skill as its own .skill file, under <out>/skills/.

The skill formats are vendor-neutral and need no target. Pass paths to
export only those artifacts rather than the whole project. Vendors with no
plugin format (chatgpt, cursor, gemini-cli) get each artifact
exported on its own instead, and workflows always become their own plugin.

Before exporting, every artifact being exported (the whole project unless
paths are given) that declares a "build:" command is built, so bundled output
like dist/main.js is fresh. If any build fails, nothing is exported. Pass
--no-build to skip this step.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format := export.Format(exportFormat)
		switch format {
		case export.FormatPlugin, export.FormatSkillsZip, export.FormatSkillFiles:
		default:
			return fmt.Errorf("unknown --format %q (want plugin, skills.zip, or skill)", exportFormat)
		}
		if format != export.FormatPlugin && (len(exportNamespaces) > 0 || exportZip) {
			return fmt.Errorf("--namespace and --zip only apply to --format plugin")
		}

		root, err := projectRoot()
		if err != nil {
			return err
		}
		m, err := project.Load(root)
		if err != nil {
			return err
		}

		req := export.Request{
			Root:        root,
			ProjectName: m.Name,
			OutDir:      exportOut,
			Format:      format,
			Namespaces:  splitCSV(exportNamespaces),
			Name:        exportName,
			Zip:         exportZip,
		}
		if format == export.FormatPlugin {
			if req.Targets, err = export.ResolveTargets(root, exportTargets); err != nil {
				return err
			}
		}
		for _, p := range args {
			a, err := loadArtifactAtPath(p)
			if err != nil {
				return err
			}
			req.Artifacts = append(req.Artifacts, a)
		}

		if !exportNoBuild {
			toBuild := req.Artifacts
			if len(toBuild) == 0 {
				found, errs := project.Discover(root)
				for _, e := range errs {
					color.Warning("%v", e)
				}
				toBuild = found
			}
			if _, failed := runBuilds(toBuild); failed > 0 {
				return fmt.Errorf("%d artifact(s) failed to build; nothing was exported (fix the build or pass --no-build)", failed)
			}
		}

		lf, err := lockfile.Load(root)
		if err != nil {
			return err
		}
		res, runErr := export.Run(req, lf)
		if err := lf.Save(root); err != nil {
			color.Warning("failed to update %s: %v", lockfile.FileName, err)
		}

		for _, w := range res.Warnings {
			color.Warning("%s", w)
		}
		for _, o := range res.Outputs {
			label := o.Name
			if o.Target != "" {
				label = fmt.Sprintf("%s (%s)", o.Name, o.Target)
			}
			color.Success("Exported %s -- %d artifact(s) -- to %s", label, o.Members, o.Path)
		}
		return runErr
	},
}

// splitCSV flattens repeated and comma-separated flag values
// ("--namespace a,b --namespace c") into one list.
func splitCSV(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// The helpers below are thin wrappers over internal/export for the other
// commands that record or check exports (marketplace, status).

func warnSecurityRisk(a *artifact.Artifact) {
	for _, w := range a.LintSecurity() {
		color.Warning("%s: %s", w.Dir, w.Message)
	}
}

func rootRelKey(root, dir string) (string, error) { return export.RootRelKey(root, dir) }

func warnIfHandEdited(lf *lockfile.Lockfile, target, artifactKey string) {
	for _, w := range export.HandEditedWarning(lf, target, artifactKey) {
		color.Warning("%s", w)
	}
}

func recordExport(root string, lf *lockfile.Lockfile, target, artifactKey string, sourceDirs []string, out string) error {
	return export.Record(root, lf, target, artifactKey, sourceDirs, out)
}

func bundleDescription(arts []*artifact.Artifact) string { return export.BundleDescription(arts) }

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringSliceVar(&exportTargets, "target", nil, "vendor target(s) to export for (default: the project's targets)")
	exportCmd.Flags().StringVar(&exportOut, "out", "dist", "output directory")
	exportCmd.Flags().BoolVar(&exportZip, "zip", false, "also package each plugin as a .zip")
	exportCmd.Flags().StringVar(&exportFormat, "format", "plugin", "plugin, skills.zip (all skills in one zip), or skill (one .skill file per skill)")
	exportCmd.Flags().StringSliceVar(&exportNamespaces, "namespace", nil, `bundle one plugin per namespace ("." for your own un-namespaced artifacts; repeatable)`)
	exportCmd.Flags().BoolVar(&exportNoBuild, "no-build", false, "skip running artifacts' build: commands before exporting")
	exportCmd.Flags().StringVar(&exportName, "name", "", "plugin/archive name (default: the project name)")
}
