package tui

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
)

// ExportAnswers is what exportForm collects: enough to call an Exporter.
type ExportAnswers struct {
	Target string
	Zip    bool
}

// exportForm builds a form for exporting a, restricted to targets that
// actually support a's kind (the same targets.Supports check `agentworks
// targets` uses). Returns (nil, nil) if none do, so the caller can show a
// message instead of opening a form with nothing to pick.
func exportForm(a *artifact.Artifact) (*huh.Form, *ExportAnswers) {
	var options []huh.Option[string]
	for _, t := range targets.All() {
		if targets.Supports(t.ID, a.Kind) {
			options = append(options, huh.NewOption(t.Name, t.ID))
		}
	}
	if len(options) == 0 {
		return nil, nil
	}

	answers := &ExportAnswers{}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Export %s (%s) to...", a.Name, a.Kind)).
				Options(options...).
				Value(&answers.Target),
			huh.NewConfirm().
				Title("Package as .zip?").
				Value(&answers.Zip),
		),
	)
	return form, answers
}
