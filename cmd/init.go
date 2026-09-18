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
	initCI      bool
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Scaffold a new AgentWorks project",
	Long: `Create agentworks.yaml and the agents/, skills/, mcp/, and hooks/
directories for a new project at path (default: current directory).

With --ci, also write .github/workflows/agentworks.yml, a GitHub Actions
workflow that runs the project's checks (validate --strict, doctor, and the
committed-marketplace freshness check) on every pull request.`,
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

		if initCI {
			path, err := project.WriteCIWorkflow(dir)
			if err != nil {
				return err
			}
			color.Success("Wrote %s", path)
		}

		color.Success("Initialized AgentWorks project %q in %s", m.Name, absDir)
		color.Info("Next: agentworks new skill <name> --description \"...\"")
		return nil
	},
}

// promptProjectTargets asks (via the same huh-based prompting style as
// `agentworks new`'s wizard) which vendor targets this project exports to.
// Targets are project-level, not per-artifact, so setting them once here
// means a bare `agentworks export` (CLI or TUI) just works afterward.
func promptProjectTargets() ([]string, error) {
	options := make([]huh.Option[string], 0, len(targets.All()))
	for _, t := range targets.All() {
		options = append(options, huh.NewOption(t.Name, t.ID))
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Export targets").
				Description("which vendors will this project export to? (leave empty to pass --target to each export)").
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
	initCmd.Flags().BoolVar(&initCI, "ci", false, "also write a GitHub Actions workflow that runs the project's checks on pull requests")
	initCmd.Flags().StringSliceVar(&initTargets, "target", nil, "vendor target(s) this project exports to (repeatable)")
}
