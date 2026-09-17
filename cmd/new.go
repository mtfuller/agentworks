package cmd

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/tui"
)

var (
	newDescription  string
	newVersion      string
	newTargetsFlag  []string
	newFromTemplate string
)

var newCmd = &cobra.Command{
	Use:   "new [kind] [name]",
	Short: "Scaffold a new agent, skill, tool, hook, or workflow",
	Long: fmt.Sprintf(`Scaffold a new artifact in the current project.

Kind is one of: agent, skill, tool, hook, workflow.

Pass --description (and kind/name as arguments) for a non-interactive run
suitable for scripts. Leave any of kind, name, or --description out in an
interactive terminal and a short wizard fills in the rest.`),
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}

		var kindStr, name string
		if len(args) > 0 {
			kindStr = args[0]
		}
		if len(args) > 1 {
			name = args[1]
		}
		description := newDescription
		targetList := newTargetsFlag

		if needsInteractiveWizard(kindStr, name, description, newFromTemplate) {
			if !isInteractive() {
				return fmt.Errorf("kind, name, and --description are required (or run this command interactively)")
			}
			answers, err := tui.RunNewArtifactWizard(tui.NewArtifactAnswers{
				Kind:        kindStr,
				Name:        name,
				Description: description,
				Targets:     targetList,
				Template:    newFromTemplate,
			})
			if err != nil {
				return fmt.Errorf("cancelled: %w", err)
			}
			kindStr, name, description, targetList = answers.Kind, answers.Name, answers.Description, answers.Targets
		}

		kind, err := artifact.ParseKind(kindStr)
		if err != nil {
			return err
		}

		description = resolveDescription(description, kind, newFromTemplate)

		if len(targetList) == 0 {
			if m, err := project.Load(root); err == nil {
				targetList = m.Targets
			}
		}

		a, err := scaffold.New(root, kind, name, scaffold.Options{
			Description: description,
			Version:     newVersion,
			Targets:     targetList,
			Template:    newFromTemplate,
		})
		if err != nil {
			return err
		}

		color.Success("Created %s %q at %s", kind, name, a.Dir)
		return nil
	},
}

// isInteractive reports whether stdin looks like a real terminal, i.e.
// whether it's safe to launch an interactive wizard instead of failing
// with a "missing flag" error.
func isInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// needsInteractiveWizard decides whether enough was given non-interactively
// to skip the wizard. A template supplies its own description, so its
// absence alone shouldn't force an interactive prompt when kind/name/
// template were all given.
func needsInteractiveWizard(kindStr, name, description, fromTemplate string) bool {
	return kindStr == "" || name == "" || (description == "" && fromTemplate == "")
}

// resolveDescription defaults description from a template's own
// Description when one was given and no description was, leaving
// description untouched otherwise (including when the template doesn't
// exist -- scaffold.New reports that error itself).
func resolveDescription(description string, kind artifact.Kind, fromTemplate string) string {
	if description != "" || fromTemplate == "" {
		return description
	}
	if t, ok := scaffold.GetTemplate(kind, fromTemplate); ok {
		return t.Description
	}
	return description
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().StringVar(&newDescription, "description", "", "one-line description of the artifact")
	newCmd.Flags().StringVar(&newVersion, "version", "", "artifact version (default: 0.1.0)")
	newCmd.Flags().StringSliceVar(&newTargetsFlag, "target", nil, "vendor target(s) this artifact supports (repeatable; default: project's default targets)")
	newCmd.Flags().StringVar(&newFromTemplate, "from-template", "", "scaffold from a built-in starter template (see 'agentworks templates')")
}
