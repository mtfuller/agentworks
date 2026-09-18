package tui

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// NewArtifactAnswers is what RunNewArtifactWizard collects: enough to call
// scaffold.New. Pre-populate fields the caller already has (e.g. from CLI
// flags) and the wizard shows them as editable defaults instead of asking
// again.
type NewArtifactAnswers struct {
	Kind        string
	Name        string
	Description string
	// Template, if set, scaffolds from a built-in starter template (see
	// internal/scaffold.GetTemplate) instead of the kind's generic default.
	// Not its own form field -- it's chosen before the form opens (a CLI
	// flag, or the TUI's template-browse pane) and just rides along.
	Template string
}

// RunNewArtifactWizard prompts interactively (via huh) for whatever fields
// are still needed to scaffold a new artifact, so `agentworks new` and the
// TUI's "create artifact" action share one prompt flow instead of two.
func RunNewArtifactWizard(defaults NewArtifactAnswers) (NewArtifactAnswers, error) {
	form, answers := newArtifactForm(defaults)
	if err := form.Run(); err != nil {
		return NewArtifactAnswers{}, err
	}
	return *answers, nil
}

// newArtifactForm builds the create-artifact form and the answers struct
// its fields are bound to, without running it -- shared by
// RunNewArtifactWizard (which runs it as a blocking CLI prompt) and the
// TUI browser (which embeds it as a child Bubble Tea model instead).
func newArtifactForm(defaults NewArtifactAnswers) (*huh.Form, *NewArtifactAnswers) {
	a := defaults

	kindOptions := make([]huh.Option[string], 0, len(artifact.Kinds()))
	for _, k := range artifact.Kinds() {
		kindOptions = append(kindOptions, huh.NewOption(string(k), string(k)))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Kind").
				Options(kindOptions...).
				Value(&a.Kind),
			huh.NewInput().
				Title("Name").
				Description("lowercase letters, digits, and hyphens").
				Value(&a.Name).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Description").
				Value(&a.Description).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("description is required")
					}
					return nil
				}),
		),
	)
	return form, &a
}
