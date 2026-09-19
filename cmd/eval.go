package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/evalspec"
	"github.com/mtfuller/agentworks/internal/project"
)

var (
	evalCaseFilter string
	evalJUnitPath  string
)

// defaultEvalTimeout bounds a runner or judge call that sets no timeout.
const defaultEvalTimeout = 2 * time.Minute

var evalCmd = &cobra.Command{
	Use:   "eval [path]",
	Short: "Run an artifact's behavior-eval cases",
	Long: `Behavior-test a skill or agent: for each case under its "evals/" directory,
pipe the case's "prompt" to the artifact's declared "eval_runner" command (or
the project's agentworks.yaml "eval.default_runner" if the artifact doesn't
set its own) and check what comes back against the case's assertions.

AgentWorks never calls a model itself here -- eval_runner is your own shell
command (a script that calls whatever model/API you want, or "claude -p", or
anything else that reads a prompt on stdin and prints a response on
stdout). This mirrors "agentworks test": AgentWorks orchestrates cases and
assertions, it doesn't execute anything on its own.

A runner speaks one of two protocols, set by "eval_protocol" (or the project's
eval.protocol). "text" (the default): stdout is the response. "json": stdout is
one JSON object {"text", "tool_calls", "activated", "usage"}, which also lets a
case assert on the tools the model called (tool_called, tool_not_called,
tool_args), on which skills it activated (activated, not_activated), and test
that the artifact triggers for the right prompts (should_trigger).

A "rubric" assertion is graded by a judge command ("judge_runner", or the
project's eval.judge_runner) that reads {"subject","prompt","response","rubric"}
as JSON on stdin and prints {"pass": true|false, "reason": "..."}.

A case can set "runs" and "pass_threshold" to repeat a nondeterministic case
and pass if enough runs do, and "timeout" (seconds) to bound each run. The
runner and judge see AGENTWORKS_ARTIFACT_NAME, _KIND, and _DIR, and
AGENTWORKS_EVAL_CASE, in their environment.

With --junit <file>, a JUnit XML report is written for CI test reporting.

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

		settings := loadEvalSettings()

		ran, failed, skipped := 0, 0, 0
		var items []evalItem
		var results []evalspec.Result
		record := func(r evalspec.Result, item evalItem) {
			results = append(results, r)
			items = append(items, item)
			switch r.Outcome {
			case evalspec.OutcomeFailed:
				failed++
			case evalspec.OutcomeSkipped:
				skipped++
			}
		}

		for _, a := range toRun {
			cases, err := evalspec.LoadDir(filepath.Join(a.Dir, "evals"))
			if err != nil {
				color.Error("%v", err)
				record(
					evalspec.Result{Artifact: a.Name, Outcome: evalspec.OutcomeFailed, Reasons: []string{err.Error()}},
					evalItem{Artifact: a.Name, Path: itemPath(a), Status: "failed", Reasons: []string{err.Error()}},
				)
				continue
			}
			if len(cases) == 0 {
				continue
			}

			cfg, err := settings.forArtifact(a)
			if err != nil {
				color.Error("%s: %v", a.Dir, err)
				record(
					evalspec.Result{Artifact: a.Name, Outcome: evalspec.OutcomeFailed, Reasons: []string{err.Error()}},
					evalItem{Artifact: a.Name, Path: itemPath(a), Status: "failed", Reasons: []string{err.Error()}},
				)
				continue
			}

			for _, c := range cases {
				if evalCaseFilter != "" && c.Name != evalCaseFilter {
					continue
				}

				if cfg.runner == "" {
					reason := "no eval_runner set and no project default"
					color.Warning("%s: %q has eval cases but no \"eval_runner\" set (and no project default) -- skipping", a.Dir, c.Name)
					record(
						evalspec.Result{Artifact: a.Name, Case: c.Name, Outcome: evalspec.OutcomeSkipped, Reasons: []string{reason}},
						evalItem{Artifact: a.Name, Path: itemPath(a), Case: c.Name, Status: "skipped", Reasons: []string{reason}},
					)
					continue
				}

				res := runEvalCaseRepeatedly(a, c, cfg)
				ran++
				report(a, c, res)
				record(res.result(a.Name, c.Name), res.item(a, c.Name, itemPath(a)))
			}
		}

		if ran == 0 {
			color.Info("No eval cases ran.")
		}

		if evalJUnitPath != "" {
			if err := writeJUnitFile(evalJUnitPath, results); err != nil {
				return err
			}
		}

		var failErr error
		if failed > 0 {
			failErr = fmt.Errorf("%d eval case(s) failed", failed)
		}
		if jsonFlag {
			if err := emitJSON(evalDoc{
				envelope: newEnvelope("eval", failErr == nil),
				Summary:  evalSummary{Ran: ran, Failed: failed, Skipped: skipped},
				Cases:    items,
			}); err != nil {
				return err
			}
		}
		return failErr
	},
}

// ---- settings -------------------------------------------------------------

// evalSettings are the project-level eval defaults; an artifact's own
// frontmatter overrides the runner, protocol, and judge.
type evalSettings struct {
	runner   string
	protocol string
	judge    string
	runs     int
	timeout  time.Duration
}

// loadEvalSettings reads agentworks.yaml's eval block. Evaluating a single
// artifact by path (unlike whole-project mode) doesn't require being inside a
// project at all, so a missing manifest just means "no project defaults".
func loadEvalSettings() evalSettings {
	var s evalSettings
	if root, err := projectRoot(); err == nil {
		if manifest, err := project.Load(root); err == nil && manifest.Eval != nil {
			e := manifest.Eval
			s = evalSettings{runner: e.DefaultRunner, protocol: e.Protocol, judge: e.JudgeRunner, runs: e.Runs, timeout: time.Duration(e.Timeout) * time.Second}
		}
	}
	return s
}

// artifactEval is the eval configuration in force for one artifact.
type artifactEval struct {
	runner   string
	protocol string
	judge    string
	runs     int
	timeout  time.Duration
}

func (s evalSettings) forArtifact(a *artifact.Artifact) (artifactEval, error) {
	cfg := artifactEval{runner: s.runner, protocol: s.protocol, judge: s.judge, runs: s.runs, timeout: s.timeout}
	if r := a.ExtraString("eval_runner"); r != "" {
		cfg.runner = r
	}
	if j := a.ExtraString("judge_runner"); j != "" {
		cfg.judge = j
	}
	if p := a.ExtraString("eval_protocol"); p != "" {
		cfg.protocol = p
	}
	if cfg.protocol == "" {
		cfg.protocol = evalspec.ProtocolText
	}
	if !evalspec.ValidProtocol(cfg.protocol) {
		return cfg, fmt.Errorf("eval_protocol must be \"text\" or \"json\", got %q", cfg.protocol)
	}
	if cfg.runs == 0 {
		cfg.runs = 1
	}
	if cfg.timeout == 0 {
		cfg.timeout = defaultEvalTimeout
	}
	return cfg, nil
}

// ---- running a case -------------------------------------------------------

// runOutcome is one run of a case.
type runOutcome struct {
	passed   bool
	reasons  []string
	duration time.Duration
	usage    evalspec.Usage
}

// caseResult aggregates a case's runs.
type caseResult struct {
	runs      []runOutcome
	threshold float64
}

func (r caseResult) passes() int {
	n := 0
	for _, o := range r.runs {
		if o.passed {
			n++
		}
	}
	return n
}

func (r caseResult) passed() bool {
	if len(r.runs) == 0 {
		return false
	}
	// A small tolerance so 4 of 5 passes a 0.8 threshold despite float rounding.
	return float64(r.passes())/float64(len(r.runs)) >= r.threshold-1e-9
}

func (r caseResult) duration() time.Duration {
	var d time.Duration
	for _, o := range r.runs {
		d += o.duration
	}
	return d
}

// reasons explains a failed case: with one run, that run's reasons; with
// several, each failing run's, prefixed by its number, after a pass-rate line.
func (r caseResult) reasons() []string {
	if r.passed() {
		return nil
	}
	if len(r.runs) == 1 {
		return r.runs[0].reasons
	}
	out := []string{fmt.Sprintf("%d of %d runs passed, below the %.0f%% required", r.passes(), len(r.runs), r.threshold*100)}
	for i, o := range r.runs {
		if o.passed {
			continue
		}
		for _, reason := range o.reasons {
			out = append(out, fmt.Sprintf("run %d: %s", i+1, reason))
		}
	}
	return out
}

func (r caseResult) usage() (total evalspec.Usage) {
	for _, o := range r.runs {
		total.InputTokens += o.usage.InputTokens
		total.OutputTokens += o.usage.OutputTokens
	}
	return total
}

func (r caseResult) result(artifactName, caseName string) evalspec.Result {
	outcome := evalspec.OutcomePassed
	if !r.passed() {
		outcome = evalspec.OutcomeFailed
	}
	return evalspec.Result{
		Artifact: artifactName, Case: caseName, Outcome: outcome, Reasons: r.reasons(),
		Runs: len(r.runs), Passes: r.passes(), Duration: r.duration(),
	}
}

func (r caseResult) item(a *artifact.Artifact, caseName, path string) evalItem {
	status := "passed"
	if !r.passed() {
		status = "failed"
	}
	item := evalItem{
		Artifact: a.Name, Path: path, Case: caseName, Status: status, Reasons: r.reasons(),
		Runs: len(r.runs), Passes: r.passes(), DurationMS: r.duration().Milliseconds(),
	}
	if u := r.usage(); u.InputTokens > 0 || u.OutputTokens > 0 {
		item.Usage = &usageDoc{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
	}
	return item
}

// runEvalCaseRepeatedly runs a case as many times as it asks for (or the
// project default) and aggregates the runs.
func runEvalCaseRepeatedly(a *artifact.Artifact, c evalspec.Case, cfg artifactEval) caseResult {
	runs := c.Runs
	if runs == 0 {
		runs = cfg.runs
	}
	threshold := c.PassThreshold
	if threshold == 0 {
		threshold = 1
	}
	timeout := cfg.timeout
	if c.Timeout > 0 {
		timeout = time.Duration(c.Timeout) * time.Second
	}

	res := caseResult{threshold: threshold}
	for i := 0; i < runs; i++ {
		res.runs = append(res.runs, runEvalCaseOnce(a, c, cfg, timeout))
	}
	return res
}

// runEvalCaseOnce sends the prompt to the runner once and checks the response:
// the protocol, the deterministic assertions, and then the rubric via the judge.
func runEvalCaseOnce(a *artifact.Artifact, c evalspec.Case, cfg artifactEval, timeout time.Duration) runOutcome {
	start := time.Now()
	fail := func(reasons ...string) runOutcome {
		return runOutcome{reasons: reasons, duration: time.Since(start)}
	}

	if err := c.ValidateProtocol(cfg.protocol); err != nil {
		return fail(err.Error())
	}
	if c.NeedsJudge() && cfg.judge == "" {
		return fail("this case has a rubric, but no judge_runner is set (in the artifact's frontmatter or the project's eval.judge_runner) to grade it")
	}

	output, err := runShellCommand(cfg.runner, a, c, cfg.protocol, "runner", c.Prompt, timeout)
	if err != nil {
		return fail("runner failed: " + err.Error())
	}
	resp, err := evalspec.ParseResponse(cfg.protocol, output)
	if err != nil {
		return fail(err.Error())
	}

	reasons := evalspec.Evaluate(c, resp, a.Name)

	if c.NeedsJudge() {
		req, _ := json.Marshal(evalspec.JudgeRequest{Subject: a.Name, Prompt: c.Prompt, Response: resp.Text, Rubric: c.Assert.Rubric})
		verdictOut, err := runShellCommand(cfg.judge, a, c, cfg.protocol, "judge", string(req), timeout)
		switch {
		case err != nil:
			reasons = append(reasons, "judge failed: "+err.Error())
		default:
			verdict, err := evalspec.ParseVerdict(verdictOut)
			switch {
			case err != nil:
				reasons = append(reasons, "judge output unusable: "+err.Error())
			case !verdict.Pass:
				reason := "rubric not met"
				if verdict.Reason != "" {
					reason += ": " + verdict.Reason
				}
				reasons = append(reasons, reason)
			}
		}
	}

	return runOutcome{passed: len(reasons) == 0, reasons: reasons, duration: time.Since(start), usage: resp.Usage}
}

// runShellCommand pipes stdin to command (run via "sh -c" from the artifact's
// directory, the same execution style cmd/test.go uses for "test:") and returns
// its stdout, giving up after timeout. A non-zero exit is always a failure,
// regardless of assertions. The runner's stderr passes through to ours, where
// its logs belong.
func runShellCommand(command string, a *artifact.Artifact, c evalspec.Case, protocol, role, stdin string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	abs, err := filepath.Abs(a.Dir)
	if err != nil {
		abs = a.Dir
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = a.Dir
	killWithChildren(cmd)
	cmd.Stdin = bytes.NewBufferString(stdin)
	cmd.Env = append(os.Environ(),
		"AGENTWORKS_ARTIFACT_NAME="+a.Name,
		"AGENTWORKS_ARTIFACT_KIND="+string(a.Kind),
		"AGENTWORKS_ARTIFACT_DIR="+abs,
		"AGENTWORKS_EVAL_CASE="+c.Name,
		"AGENTWORKS_EVAL_PROTOCOL="+protocol,
		"AGENTWORKS_EVAL_ROLE="+role,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	// sh -c may leave grandchildren holding the pipes open after a kill.
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("timed out after %s", timeout)
		}
		return "", err
	}
	return stdout.String(), nil
}

// report prints a case's outcome in the human format.
func report(a *artifact.Artifact, c evalspec.Case, res caseResult) {
	multi := len(res.runs) > 1
	if res.passed() {
		if multi {
			color.Success("%s: %q passed (%d of %d runs)", a.Name, c.Name, res.passes(), len(res.runs))
		} else {
			color.Success("%s: %q passed", a.Name, c.Name)
		}
		return
	}
	if !multi && len(res.runs[0].reasons) == 1 && strings.HasPrefix(res.runs[0].reasons[0], "runner failed: ") {
		color.Error("%s: %q: %s", a.Name, c.Name, res.runs[0].reasons[0])
		return
	}
	color.Error("%s: %q failed:", a.Name, c.Name)
	for _, r := range res.reasons() {
		color.Error("  - %s", r)
	}
}

func writeJUnitFile(path string, results []evalspec.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("writing the JUnit report: %w", err)
	}
	defer f.Close()
	if err := evalspec.WriteJUnit(f, results); err != nil {
		return fmt.Errorf("writing the JUnit report: %w", err)
	}
	return nil
}

// ---- --json ---------------------------------------------------------------

type evalDoc struct {
	envelope
	Summary evalSummary `json:"summary"`
	Cases   []evalItem  `json:"cases"`
}

type evalSummary struct {
	Ran     int `json:"ran"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
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
	// Runs is how many times the case ran, and Passes how many of those passed;
	// both are zero for a case that didn't run.
	Runs   int `json:"runs"`
	Passes int `json:"passes"`
	// DurationMS is the total time spent across the runs.
	DurationMS int64 `json:"duration_ms"`
	// Usage sums the tokens a json-protocol runner reported, if it did.
	Usage *usageDoc `json:"usage,omitempty"`
}

type usageDoc struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func init() {
	rootCmd.AddCommand(evalCmd)
	evalCmd.Flags().StringVar(&evalCaseFilter, "case", "", "run only the case with this exact name")
	evalCmd.Flags().StringVar(&evalJUnitPath, "junit", "", "also write a JUnit XML report to this file, for CI test reporting")
}
