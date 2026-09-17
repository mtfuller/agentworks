package cmd

import (
	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Browse, create, and export artifacts in a full-screen terminal UI",
	Long: `Launch an interactive browser: drill from artifact kind, to artifact, to its
rendered frontmatter and body.

  n    scaffold a new artifact (same wizard as 'agentworks new')
  e    export the current artifact to a vendor target, with an option to zip
  enter / esc / q   drill in / step back / quit`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		return tui.Run(root)
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
