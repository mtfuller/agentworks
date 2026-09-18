package cmd

import (
	"os"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/logger"
	"github.com/spf13/cobra"
)

var (
	verbose     bool
	logLevel    string
	projectFlag string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "agentworks",
	Short: "A local-first, vendor-agnostic tool for building agent contexts, skills, tools, and hooks",
	Long: color.Bold("agentworks") + ` is a local-first, vendor-agnostic platform for authoring agents,
skills, tools, and hooks once and exporting them to the harnesses you actually use
(Claude Code, ChatGPT, GitHub Copilot, Cursor, Gemini CLI, and others).

Author artifacts as plain files in a project directory, test and validate them
locally, then export versioned, target-specific bundles (skill zips, plugins,
or vendor-native formats) without hand-maintaining a copy per vendor.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Configure logger based on flags
		if verbose {
			logger.SetLevel(logger.DEBUG)
		} else {
			logger.SetLevel(logger.ParseLogLevel(logLevel))
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		color.Error("Error: %v", err)
		os.Exit(1)
	}
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output (debug level)")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "info", "set log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVarP(&projectFlag, "project", "p", ".", "path inside the AgentWorks project to operate on")
}
