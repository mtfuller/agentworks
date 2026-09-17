package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/tui"
)

var (
	addName   string
	addDryRun bool
	addYes    bool
)

var addCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Import a published skill or Claude Code plugin into this project",
	Long: `Fetch a skill or Claude Code plugin from a URL and add it to this project as
one or more local artifacts.

Accepts a GitHub repo ("owner/repo", a full github.com URL, or a
github.com/.../tree/<ref>/<path> browse URL), a raw.githubusercontent.com file
URL, or a direct .zip/.tar.gz archive URL.

A bare Agent Skill (a directory with SKILL.md at its root) becomes one skill
artifact. A Claude Code plugin (.claude-plugin/plugin.json at its root) is
decomposed into one artifact per skill/agent it contains; tools and hooks
inside a fetched plugin aren't supported yet and are reported, not imported.

With no URL, in an interactive terminal, this launches the marketplace search
pane instead (the same one "agentworks tui"'s "a" key opens).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}

		if len(args) == 0 {
			if !isInteractive() {
				return fmt.Errorf("pass a URL to import (or run this in a terminal for the interactive marketplace search)")
			}
			return tui.RunMarketplace(root)
		}

		src, err := importer.ParseAddArgument(args[0])
		if err != nil {
			return err
		}

		plan, err := importer.Prepare(context.Background(), root, src, importer.Options{Name: addName})
		if err != nil {
			return err
		}
		defer plan.Close()

		if addDryRun {
			fmt.Print(plan.Describe())
			return nil
		}

		ok, err := securityGate(plan.Source.String(), plan.Artifacts, addYes)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("import aborted")
		}

		if err := plan.Apply(); err != nil {
			return err
		}
		for _, a := range plan.Artifacts {
			color.Success("Imported %s %q at %s", a.Kind, a.Name, a.Dir)
		}
		for _, u := range plan.Unsupported {
			color.Warning("%s", u)
		}

		if err := recordImports(root, plan); err != nil {
			color.Warning("imported successfully, but failed to update %s: %v", lockfile.FileName, err)
		}
		return nil
	},
}

// recordImports pins what was actually written for every artifact in plan
// into the project's agentworks.lock, so `agentworks update` has a content
// hash of the raw fetched source to compare a future fetch against (see
// internal/lockfile, internal/importer.Plan.RecordLockEntries).
func recordImports(root string, plan *importer.Plan) error {
	lf, err := lockfile.Load(root)
	if err != nil {
		return err
	}
	if err := plan.RecordLockEntries(root, lf); err != nil {
		return err
	}
	return lf.Save(root)
}

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().StringVar(&addName, "name", "", "override the derived artifact name (single-skill imports only)")
	addCmd.Flags().BoolVar(&addDryRun, "dry-run", false, "show what would be imported without writing anything")
	addCmd.Flags().BoolVar(&addYes, "yes", false, "skip the confirmation prompt when imported content declares a shell command (required in non-interactive use)")
}
