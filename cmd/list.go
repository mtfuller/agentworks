package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
)

var listCmd = &cobra.Command{
	Use:   "list [kind]",
	Short: "List artifacts in the project",
	Long:  "List discovered agents, skills, tools, hooks, and workflows. Pass a kind to filter.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}

		var kinds []artifact.Kind
		if len(args) == 1 {
			k, err := artifact.ParseKind(args[0])
			if err != nil {
				return err
			}
			kinds = []artifact.Kind{k}
		}

		found, errs := project.Discover(root, kinds...)
		for _, e := range errs {
			color.Warning("%v", e)
		}
		if len(found) == 0 {
			color.Info("No artifacts found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tNAME\tDESCRIPTION\tTARGETS")
		for _, a := range found {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Kind, a.Name, truncate(a.Description, 60), strings.Join(a.Targets, ", "))
		}
		return w.Flush()
	},
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func init() {
	rootCmd.AddCommand(listCmd)
}
