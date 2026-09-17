package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

var templatesCmd = &cobra.Command{
	Use:   "templates [kind]",
	Short: "List built-in starter templates",
	Long: `Print the built-in starter templates "agentworks new --from-template" can
scaffold from, instead of the generic blank starting point. Pass a kind to filter.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		kinds := artifact.Kinds()
		if len(args) == 1 {
			k, err := artifact.ParseKind(args[0])
			if err != nil {
				return err
			}
			kinds = []artifact.Kind{k}
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tID\tTITLE\tDESCRIPTION")
		for _, k := range kinds {
			for _, t := range scaffold.TemplatesForKind(k) {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Kind, t.ID, t.Title, t.Description)
			}
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(templatesCmd)
}
