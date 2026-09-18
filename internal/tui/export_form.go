package tui

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/export"
)

var errNoNamespace = errors.New("pick at least one namespace")

// formatNamespaces is the form's own value for "one plugin per namespace",
// which export.Request expresses as FormatPlugin plus a Namespaces list.
const formatNamespaces = "namespaces"

// ExportAnswers is what exportForm collects.
type ExportAnswers struct {
	Format     string // an export.Format, or formatNamespaces
	Namespaces []string
	Zip        bool
}

// request turns the answers into an export.Request for the project at
// root. Targets are left for the caller to resolve from the project.
func (a *ExportAnswers) request(root, projectName string) export.Request {
	req := export.Request{
		Root:        root,
		ProjectName: projectName,
		OutDir:      filepath.Join(root, "dist"),
		Format:      export.Format(a.Format),
	}
	switch a.Format {
	case formatNamespaces:
		req.Format = export.FormatPlugin
		req.Namespaces = a.Namespaces
		req.Zip = a.Zip
	case string(export.FormatPlugin):
		req.Zip = a.Zip
	}
	return req
}

// exportForm builds the project-level export form. projectTargets are only
// shown, never asked for: with none configured, plugin formats are left out
// (the skill archives need no target) rather than offered and then failing.
// namespaces are export.Namespaces(arts); per-namespace export is only
// offered when there's more than one to choose between.
func exportForm(projectTargets, namespaces []string, arts []*artifact.Artifact) (*huh.Form, *ExportAnswers) {
	hasSkills := false
	for _, a := range arts {
		hasSkills = hasSkills || a.Kind == artifact.KindSkill
	}

	var options []huh.Option[string]
	if len(projectTargets) > 0 {
		options = append(options, huh.NewOption("Everything in one plugin", string(export.FormatPlugin)))
		if len(namespaces) > 1 {
			options = append(options, huh.NewOption("One plugin per namespace...", formatNamespaces))
		}
	}
	if hasSkills {
		options = append(options,
			huh.NewOption("All skills as one .zip", string(export.FormatSkillsZip)),
			huh.NewOption("Each skill as a .skill file", string(export.FormatSkillFiles)),
		)
	}

	if len(options) == 0 {
		return nil, nil
	}

	desc := "No targets in agentworks.yaml -- add a `targets:` list to export plugins."
	if len(projectTargets) > 0 {
		desc = "Targets (from agentworks.yaml): " + strings.Join(projectTargets, ", ")
	}

	answers := &ExportAnswers{Format: options[0].Value}

	nsOptions := make([]huh.Option[string], 0, len(namespaces))
	for _, ns := range namespaces {
		label := "@" + ns
		if ns == export.UnnamespacedToken {
			label = "(yours -- no namespace)"
		}
		nsOptions = append(nsOptions, huh.NewOption(label, ns))
	}
	pluginish := func() bool {
		return answers.Format == string(export.FormatPlugin) || answers.Format == formatNamespaces
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Export project as...").
				Description(desc).
				Options(options...).
				Value(&answers.Format),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Namespaces to bundle").
				Description("one plugin is written for each").
				Options(nsOptions...).
				Value(&answers.Namespaces).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return errNoNamespace
					}
					return nil
				}),
		).WithHideFunc(func() bool { return answers.Format != formatNamespaces }),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Also package each plugin as a .zip?").
				Value(&answers.Zip),
		).WithHideFunc(func() bool { return !pluginish() }),
	)
	return form, answers
}
