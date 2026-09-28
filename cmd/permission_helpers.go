package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/permissionbridge"
)

var permissionHookCmd = &cobra.Command{
	Use:    "__permission-hook",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return permissionbridge.RunHook(cmd.Context(), os.Stdin, os.Stdout)
	},
}

var permissionMCPCmd = &cobra.Command{
	Use:    "__permission-mcp",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return permissionbridge.RunMCP(cmd.Context(), os.Stdin, os.Stdout)
	},
}

func init() {
	rootCmd.AddCommand(permissionHookCmd, permissionMCPCmd)
}
