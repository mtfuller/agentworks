package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/evalspec"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets/agentcaps"
	"github.com/mtfuller/agentworks/internal/targets/workflowsteps"
)

var validateStrict bool

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate artifact frontmatter",
	Long: `Parse and validate one artifact (by path) or every artifact in the project.

Beyond structural checks (required fields, a workflow's "steps:" resolving,
etc.), this also lints description quality -- too long, too vague, redundant
with the name, or overlapping with another artifact's description -- and
prints those as warnings. Warnings don't fail the command unless --strict is
set.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toCheck []*artifact.Artifact
		discoverErrs := 0
		wholeProject := len(args) == 0

		if !wholeProject {
			a, err := loadArtifactAtPath(args[0])
			if err != nil {
				return err
			}
			toCheck = append(toCheck, a)
		} else {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			found, errs := project.Discover(root)
			for _, e := range errs {
				color.Error("%v", e)
			}
			discoverErrs = len(errs)
			toCheck = found
		}

		failed := discoverErrs
		warned := 0
		for _, a := range toCheck {
			if err := a.Validate(); err != nil {
				color.Error("%v", err)
				failed++
				continue
			}
			if err := validateKindSpecific(a); err != nil {
				color.Error("%v", err)
				failed++
				continue
			}
			for _, w := range a.LintDescription() {
				color.Warning("%s: %s", w.Dir, w.Message)
				warned++
			}
			color.Success("%s (%s)", a.Name, a.Kind)
		}

		if wholeProject {
			for _, w := range artifact.LintOverlap(toCheck) {
				color.Warning("%s: %s", w.Dir, w.Message)
				warned++
			}
		}

		if validateStrict {
			failed += warned
		}
		if failed > 0 {
			return fmt.Errorf("%d artifact(s) failed validation", failed)
		}
		return nil
	},
}

// validateKindSpecific checks the frontmatter fields specific to a kind
// that Artifact.Validate() can't (it's kind-agnostic). These catch a
// mistake at `validate` time rather than only when `export`/`test` later
// shells out to a field that was never filled in.
func validateKindSpecific(a *artifact.Artifact) error {
	switch a.Kind {
	case artifact.KindAgent:
		for _, tool := range a.ExtraStringSlice("tools") {
			if !agentcaps.IsValidTool(tool) {
				return fmt.Errorf("%s: unknown tool %q (want one of: %s)", a.Dir, tool, strings.Join(agentcaps.ValidTools(), ", "))
			}
		}
		if model := a.ExtraString("model"); model != "" && !agentcaps.IsValidModel(model) {
			return fmt.Errorf("%s: unknown model %q (want one of: %s)", a.Dir, model, strings.Join(agentcaps.ValidModels(), ", "))
		}
	case artifact.KindWorkflow:
		if _, err := workflowsteps.Resolve(a); err != nil {
			return err
		}
	case artifact.KindHook:
		events := a.ExtraStringSlice("events")
		command := a.ExtraString("command")
		if (len(events) > 0) != (command != "") {
			return fmt.Errorf("%s: \"events\" and \"command\" must be set together (a hook needs both to do anything)", a.Dir)
		}
	case artifact.KindTool:
		auth := a.ExtraStringSlice("auth")
		command := a.ExtraString("command")
		if len(auth) > 0 && command == "" {
			return fmt.Errorf("%s: declares \"auth\" but no \"command\" -- nothing will use those environment variables", a.Dir)
		}
	}

	// An "evals/" directory is valid for any kind (skills and agents are
	// the common case, but nothing stops a tool from having one too), so
	// this isn't inside the switch above -- catches a malformed eval file
	// at validate time rather than only when `agentworks eval` runs it.
	if _, err := evalspec.LoadDir(filepath.Join(a.Dir, "evals")); err != nil {
		return err
	}
	return nil
}

func init() {
	rootCmd.AddCommand(validateCmd)
	validateCmd.Flags().BoolVar(&validateStrict, "strict", false, "treat description-quality warnings as failures")
}
