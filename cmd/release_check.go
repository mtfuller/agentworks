package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/releasegate"
)

var releaseCheckCmd = &cobra.Command{
	Use:    "release-check [evidence.yaml]",
	Short:  "Validate the external evidence required for a runtime release",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "docs/operations/m9-release-evidence.yaml"
		if len(args) == 1 {
			path = args[0]
		}
		evidence, err := releasegate.Load(path)
		if err != nil {
			return err
		}
		expectedCommit, err := cmd.Flags().GetString("commit")
		if err != nil {
			return err
		}
		if strings.TrimSpace(expectedCommit) != "" {
			if err := evidence.ValidateForCommit(expectedCommit); err != nil {
				return err
			}
		}
		fmt.Printf("Runtime release evidence approved for tested commit %s\n", evidence.TestedCommit)
		return nil
	},
}

func init() {
	releaseCheckCmd.Flags().String("commit", "", "require evidence for this exact full Git commit SHA")
	rootCmd.AddCommand(releaseCheckCmd)
}
