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
		for _, a := range toRun {
			cases, err := evalspec.LoadDir(filepath.Join(a.Dir, "evals"))
			if err != nil {
				color.Error("%v", err)
				failed++
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
				continue
			}

			for _, c := range cases {
				if evalCaseFilter != "" && c.Name != evalCaseFilter {
					continue
				}
				ran++
				output, err := runCase(runner, a.Dir, c.Prompt)
				if err != nil {
					color.Error("%s: %q: runner failed: %v", a.Name, c.Name, err)
					failed++
					continue
				}
				if reasons := evalspec.Evaluate(c.Assert, output); len(reasons) > 0 {
					color.Error("%s: %q failed:", a.Name, c.Name)
					for _, r := range reasons {
						color.Error("  - %s", r)
					}
					failed++
					continue
				}
				color.Success("%s: %q passed", a.Name, c.Name)
			}
		}

		if ran == 0 {
			color.Info("No eval cases ran.")
		}
		if failed > 0 {
			return fmt.Errorf("%d eval case(s) failed", failed)
		}
		return nil
	},
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
