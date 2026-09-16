package cmd

import (
	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Browse the project in a full-screen terminal UI",
	Long:  "Launch an interactive browser: drill from artifact kind, to artifact, to its rendered frontmatter and body.",
	Args:  cobra.NoArgs,
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
