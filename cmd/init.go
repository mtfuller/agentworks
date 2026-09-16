package cmd

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
)

var (
	initName    string
	initTargets []string
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Scaffold a new AgentWorks project",
	Long: `Create agentworks.yaml and the agents/, skills/, tools/, hooks/, and
workflows/ directories for a new project at path (default: current directory).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return err
		}

		name := initName
		if name == "" {
			name = filepath.Base(absDir)
		}

		m, err := project.Init(dir, name, initTargets)
		if err != nil {
			return err
		}

		color.Success("Initialized AgentWorks project %q in %s", m.Name, absDir)
		color.Info("Next: agentworks new skill <name> --description \"...\"")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initName, "name", "", "project name (default: the directory name)")
	initCmd.Flags().StringSliceVar(&initTargets, "target", nil, "default vendor target(s) for new artifacts (repeatable)")
}
