package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/evalspec"
	"github.com/mtfuller/agentworks/internal/project"
)

var evalCaseFilter string

var evalCmd = &cobra.Command{
	Use:   "eval [path]",
	Short: "Run an artifact's behavior-eval cases",
	Long: `Behavior-test a skill or agent: for each case under its "evals/" directory,
pipe the case's "prompt" to the artifact's declared "eval_runner" command (or
the project's agentworks.yaml "eval.default_runner" if the artifact doesn't
set its own) and check the runner's stdout against the case's "assert"
rules.

AgentWorks never calls a model itself here -- eval_runner is your own shell
command (a script that calls whatever model/API you want, or "claude -p", or
anything else that reads a prompt on stdin and prints a response on
stdout). This mirrors "agentworks test": AgentWorks orchestrates cases and
assertions, it doesn't execute anything on its own.

With no path, runs every artifact in the project that has an "evals/"
directory; artifacts without one are skipped.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toRun []*artifact.Artifact

		if len(args) == 1 {
			a, err := loadArtifactAtPath(args[0])
			if err != nil {
				return err
			}
			toRun = append(toRun, a)
		} else {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			found, errs := project.Discover(root)
			for _, e := range errs {
				color.Warning("%v", e)
			}
			toRun = found
		}

		// A project-level default runner is optional, and evaluating a
		// single artifact by path (unlike whole-project mode) doesn't
		// require being inside a project at all -- so a missing manifest
		// just means "no default runner", not a hard failure.
		defaultRunner := ""
		if root, err := projectRoot(); err == nil {
			if manifest, err := project.Load(root); err == nil && manifest.Eval != nil {
				defaultRunner = manifest.Eval.DefaultRunner
			}
		}

		ran, failed := 0, 0
		results := []evalItem{}
		for _, a := range toRun {
			cases, err := evalspec.LoadDir(filepath.Join(a.Dir, "evals"))
			if err != nil {
				color.Error("%v", err)
				failed++
				results = append(results, evalItem{Artifact: a.Name, Path: itemPath(a), Status: "failed", Reasons: []string{err.Error()}})
				continue
			}
			if len(cases) == 0 {
				continue
			}

			runner := a.ExtraString("eval_runner")
			if runner == "" {
				runner = defaultRunner
			}
			if runner == "" {
				color.Warning("%s: has eval cases but no \"eval_runner\" set (and no project default) -- skipping", a.Dir)
				for _, c := range cases {
					results = append(results, evalItem{Artifact: a.Name, Path: itemPath(a), Case: c.Name, Status: "skipped", Reasons: []string{"no eval_runner set and no project default"}})
				}
				continue
			}

			for _, c := range cases {
				if evalCaseFilter != "" && c.Name != evalCaseFilter {
					continue
				}
				ran++
				item := evalItem{Artifact: a.Name, Path: itemPath(a), Case: c.Name}
				output, err := runCase(runner, a.Dir, c.Prompt)
				if err != nil {
					color.Error("%s: %q: runner failed: %v", a.Name, c.Name, err)
					failed++
					item.Status, item.Reasons = "failed", []string{"runner failed: " + err.Error()}
					results = append(results, item)
					continue
				}
				if reasons := evalspec.Evaluate(c.Assert, output); len(reasons) > 0 {
					color.Error("%s: %q failed:", a.Name, c.Name)
					for _, r := range reasons {
						color.Error("  - %s", r)
					}
					failed++
					item.Status, item.Reasons = "failed", reasons
					results = append(results, item)
					continue
				}
				color.Success("%s: %q passed", a.Name, c.Name)
				item.Status = "passed"
				results = append(results, item)
			}
		}

		if ran == 0 {
			color.Info("No eval cases ran.")
		}
		var failErr error
		if failed > 0 {
			failErr = fmt.Errorf("%d eval case(s) failed", failed)
		}
		if jsonFlag {
			if err := emitJSON(evalDoc{
				envelope: newEnvelope("eval", failErr == nil),
				Summary:  evalSummary{Ran: ran, Failed: failed},
				Cases:    results,
			}); err != nil {
				return err
			}
		}
		return failErr
	},
}

type evalDoc struct {
	envelope
	Summary evalSummary `json:"summary"`
	Cases   []evalItem  `json:"cases"`
}

type evalSummary struct {
	Ran    int `json:"ran"`
	Failed int `json:"failed"`
}

type evalItem struct {
	Artifact string `json:"artifact"`
	Path     string `json:"path"`
	// Case is empty for a failure that isn't tied to one case (e.g. an
	// unparseable evals/ file).
	Case string `json:"case,omitempty"`
	// Status is "passed", "failed", or "skipped".
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

// runCase pipes prompt to runner's stdin (run via "sh -c" from dir, the
// same execution style cmd/test.go uses for "test:") and returns its
// stdout. A non-zero exit is always a failure, regardless of assertions.
func runCase(runner, dir, prompt string) (string, error) {
	c := exec.Command("sh", "-c", runner)
	c.Dir = dir
	c.Stdin = bytes.NewBufferString(prompt)
	var stdout bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

func init() {
	rootCmd.AddCommand(evalCmd)
	evalCmd.Flags().StringVar(&evalCaseFilter, "case", "", "run only the case with this exact name")
}
