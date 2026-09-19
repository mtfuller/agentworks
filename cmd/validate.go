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
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

var validateStrict bool

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate artifact frontmatter",
	Long: `Parse and validate one artifact (by path) or every artifact in the project.

Beyond structural checks (required fields,
etc.), this also lints description quality -- too long, too vague, redundant
with the name, or overlapping with another artifact's description -- and
prints those as warnings. Warnings don't fail the command unless --strict is
set. A hook or mcp server's shell command is reported as a notice, not a
warning, so a working project passes --strict; only a suspicious command shape
(a download piped into a shell, sudo, ...) counts as a warning.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toCheck []*artifact.Artifact
		var discoverErrList []error
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
			discoverErrList = errs
			toCheck = found
		}

		failed := discoverErrs
		warned := 0
		doc := validateDoc{Strict: validateStrict, Artifacts: []validateItem{}, Problems: []string{}, Warnings: []validateWarning{}}
		for _, e := range discoverErrList {
			doc.Problems = append(doc.Problems, e.Error())
		}
		for _, a := range toCheck {
			item := validateItem{Kind: string(a.Kind), Name: a.Name, Path: itemPath(a), Errors: []string{}, Warnings: []string{}, Notices: []string{}}
			if err := a.Validate(); err != nil {
				color.Error("%v", err)
				failed++
				item.Errors = append(item.Errors, err.Error())
				doc.Artifacts = append(doc.Artifacts, item)
				continue
			}
			if err := validateKindSpecific(a); err != nil {
				color.Error("%v", err)
				failed++
				item.Errors = append(item.Errors, err.Error())
				doc.Artifacts = append(doc.Artifacts, item)
				continue
			}
			if errs := append(a.RequiresSyntaxErrors(), a.BinsSyntaxErrors()...); len(errs) > 0 {
				for _, e := range errs {
					color.Error("%s: %v", a.Dir, e)
					item.Errors = append(item.Errors, e.Error())
				}
				failed++
				doc.Artifacts = append(doc.Artifacts, item)
				continue
			}
			// The "declares a command" notice is shown but never counted as a
			// warning: every working hook or mcp server has one.
			if n := a.LintSecurityNotice(); n != nil {
				color.Info("%s: %s", n.Dir, n.Message)
				item.Notices = append(item.Notices, n.Message)
			}
			warns := append(a.LintDescription(), a.LintFields()...)
			for _, w := range append(warns, a.LintSecurityRisks()...) {
				color.Warning("%s: %s", w.Dir, w.Message)
				warned++
				item.Warnings = append(item.Warnings, w.Message)
			}
			color.Success("%s (%s)", a.Name, a.Kind)
			doc.Artifacts = append(doc.Artifacts, item)
		}

		if wholeProject {
			for _, p := range artifact.CheckRequires(toCheck) {
				color.Error("%s", p)
				failed++
				doc.Problems = append(doc.Problems, p)
			}
			for _, w := range artifact.LintOverlap(toCheck) {
				color.Warning("%s: %s", w.Dir, w.Message)
				warned++
				doc.Warnings = append(doc.Warnings, validateWarning{Path: filepath.ToSlash(w.Dir), Message: w.Message})
			}
		}

		if validateStrict {
			failed += warned
		}
		var failErr error
		if failed > 0 {
			failErr = fmt.Errorf("%d artifact(s) failed validation", failed)
		}
		if jsonFlag {
			doc.envelope = newEnvelope("validate", failErr == nil)
			doc.Summary = validateSummary{Artifacts: len(toCheck), Failed: failed, Warnings: warned}
			if err := emitJSON(doc); err != nil {
				return err
			}
		}
		return failErr
	},
}

// validateDoc is validate's --json document. Problems are artifacts that
// couldn't even be discovered (unparseable frontmatter); Warnings are the
// project-wide ones (description overlap) that belong to no single artifact.
type validateDoc struct {
	envelope
	Strict    bool              `json:"strict"`
	Summary   validateSummary   `json:"summary"`
	Artifacts []validateItem    `json:"artifacts"`
	Problems  []string          `json:"discovery_errors"`
	Warnings  []validateWarning `json:"project_warnings"`
}

type validateSummary struct {
	Artifacts int `json:"artifacts"`
	Failed    int `json:"failed"`
	Warnings  int `json:"warnings"`
}

type validateItem struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	// Notices are informational and never fail --strict (e.g. "declares a
	// shell command").
	Notices []string `json:"notices"`
}

type validateWarning struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// validateKindSpecific checks the frontmatter fields specific to a kind
// that Artifact.Validate() can't (it's kind-agnostic). These catch a
// mistake at `validate` time rather than only when `export`/`test` later
// shells out to a field that was never filled in.
func validateKindSpecific(a *artifact.Artifact) error {
	if err := a.ValidateFieldTypes(); err != nil {
		return err
	}
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
	case artifact.KindHook:
		if _, err := a.HookHandlers(); err != nil {
			return err
		}
	case artifact.KindMCP:
		if err := mcpconfig.Validate(a); err != nil {
			return err
		}
	}

	// An "evals/" directory is valid for any kind (skills and agents are
	// the common case, but nothing stops an mcp server from having one too), so
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
