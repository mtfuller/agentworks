package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets"
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

		projectTargets := initTargets
		if len(projectTargets) == 0 && isInteractive() {
			projectTargets, err = promptProjectTargets()
			if err != nil {
				return fmt.Errorf("cancelled: %w", err)
			}
		}

		m, err := project.Init(dir, name, projectTargets)
		if err != nil {
			return err
		}

		color.Success("Initialized AgentWorks project %q in %s", m.Name, absDir)
		color.Info("Next: agentworks new skill <name> --description \"...\"")
		return nil
	},
}

// promptProjectTargets asks (via the same huh-based prompting style as
// `agentworks new`'s wizard) which vendor targets new artifacts in this
// project should default to. Setting it once here, instead of leaving it
// unset, means every later `agentworks new` -- CLI or TUI -- opens with
// this project's targets already pre-selected rather than asking again
// from scratch for every single artifact.
func promptProjectTargets() ([]string, error) {
	options := make([]huh.Option[string], 0, len(targets.All()))
	for _, t := range targets.All() {
		options = append(options, huh.NewOption(t.Name, t.ID))
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Default targets").
				Description("which vendors should new artifacts support by default? (leave empty to decide per artifact)").
				Options(options...).
				Value(&selected),
		),
	)
	if err := form.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initName, "name", "", "project name (default: the directory name)")
	initCmd.Flags().StringSliceVar(&initTargets, "target", nil, "default vendor target(s) for new artifacts (repeatable)")
}
