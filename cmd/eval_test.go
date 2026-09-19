package cmd

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// evalFixture builds a project with one skill whose evals/ and runner scripts
// the test controls, and returns the project directory.
type evalFixture struct {
	dir      string
	skillDir string
}

func newEvalFixture(t *testing.T, frontmatterExtra, cases string, scripts map[string]string) evalFixture {
	t.Helper()
	dir := newProject(t)
	mustRun(t, dir, "new", "skill", "greeter", "--description", "Greets people by name whenever a greeting is called for.")
	skillDir := filepath.Join(dir, "skills", "greeter")

	skill := filepath.Join(skillDir, "skill.md")
	data, _ := os.ReadFile(skill)
	if frontmatterExtra != "" {
		os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", "version: 0.1.0\n"+frontmatterExtra, 1)), 0o644)
	}
	os.WriteFile(filepath.Join(skillDir, "evals", "example.yaml"), []byte(cases), 0o644)
	for name, body := range scripts {
		os.WriteFile(filepath.Join(skillDir, name), []byte(body), 0o755)
	}
	return evalFixture{dir: dir, skillDir: skillDir}
}

func setProjectEval(t *testing.T, dir, yamlBlock string) {
	t.Helper()
	path := filepath.Join(dir, "agentworks.yaml")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, append(data, []byte("\n"+yamlBlock+"\n")...), 0o644)
}

type evalOut struct {
	OK      bool `json:"ok"`
	Summary struct{ Ran, Failed, Skipped int }
	Cases   []struct {
		Case, Status string
		Reasons      []string
		Runs, Passes int
		DurationMS   int64 `json:"duration_ms"`
		Usage        *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
}

func runEval(t *testing.T, f evalFixture, args ...string) (evalOut, result) {
	t.Helper()
	r := runCLI(t, f.dir, append([]string{"eval", "--json"}, args...)...)
	var out evalOut
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("eval --json output is not JSON: %v\n%s\nstderr: %s", err, r.stdout, r.stderr)
	}
	return out, r
}

const jsonRunner = `cat >/dev/null
printf '%s' '{"text":"Hello there","tool_calls":[{"name":"search","arguments":{"query":"unit tests","limit":10}}],"activated":["greeter"],"usage":{"input_tokens":11,"output_tokens":7}}'
`

func TestEvalJSONProtocolChecksTheTrace(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh\neval_protocol: json", `cases:
  - name: calls search with the right arguments
    prompt: find things
    assert:
      contains: ["hello"]
      tool_called: [search]
      tool_not_called: [delete]
      tool_args:
        - tool: search
          equals: {limit: 10}
          matches: {query: "^unit"}
      activated: [greeter]
  - name: wrong tool expected
    prompt: find things
    assert:
      tool_called: [delete]
`, map[string]string{"runner.sh": jsonRunner})

	out, r := runEval(t, f)
	if r.err == nil || out.OK {
		t.Fatalf("one case should fail: err=%v ok=%v", r.err, out.OK)
	}
	byName := map[string]string{}
	for _, c := range out.Cases {
		byName[c.Case] = c.Status
	}
	if byName["calls search with the right arguments"] != "passed" || byName["wrong tool expected"] != "failed" {
		t.Errorf("statuses = %v", byName)
	}
	for _, c := range out.Cases {
		if c.Case == "wrong tool expected" && (len(c.Reasons) != 1 || !strings.Contains(c.Reasons[0], `expected tool "delete" to be called`)) {
			t.Errorf("failure reasons = %v", c.Reasons)
		}
		if c.Usage == nil || c.Usage.InputTokens != 11 || c.Usage.OutputTokens != 7 {
			t.Errorf("%s usage = %+v, want the tokens the runner reported", c.Case, c.Usage)
		}
		if c.Runs != 1 {
			t.Errorf("%s runs = %d", c.Case, c.Runs)
		}
	}
}

func TestEvalTriggerCases(t *testing.T) {
	// A runner that activates the skill only when the prompt mentions "greet".
	runner := `prompt=$(cat)
case "$prompt" in
  *greet*) printf '%s' '{"text":"hi","activated":["greeter"],"tool_calls":[]}' ;;
  *)       printf '%s' '{"text":"4","activated":[],"tool_calls":[]}' ;;
esac
`
	f := newEvalFixture(t, "eval_runner: sh runner.sh\neval_protocol: json", `cases:
  - name: triggers on a greeting request
    prompt: please greet Ada
    should_trigger: true
  - name: stays quiet for arithmetic
    prompt: what is 2+2
    should_trigger: false
  - name: description misses this phrasing
    prompt: say hello to Ada
    should_trigger: true
  - name: description is too broad here
    prompt: please greet nobody, just compute 2+2
    should_trigger: false
`, map[string]string{"runner.sh": runner})

	out, _ := runEval(t, f)
	status := map[string]string{}
	reasons := map[string]string{}
	for _, c := range out.Cases {
		status[c.Case] = c.Status
		reasons[c.Case] = strings.Join(c.Reasons, " ")
	}
	if status["triggers on a greeting request"] != "passed" || status["stays quiet for arithmetic"] != "passed" {
		t.Errorf("statuses = %v", status)
	}
	if status["description misses this phrasing"] != "failed" || !strings.Contains(reasons["description misses this phrasing"], "may not say when to use it") {
		t.Errorf("a missed trigger should fail and point at the description: %v", reasons)
	}
	if status["description is too broad here"] != "failed" || !strings.Contains(reasons["description is too broad here"], "too broad") {
		t.Errorf("a false trigger should fail and point at the description: %v", reasons)
	}
}

func TestEvalTraceAssertionsNeedTheJSONProtocol(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh", `cases:
  - name: needs a trace
    prompt: hi
    assert:
      tool_called: [search]
`, map[string]string{"runner.sh": "cat >/dev/null; echo hello\n"})
	out, r := runEval(t, f)
	if r.err == nil || len(out.Cases) != 1 || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "eval_protocol: json") {
		t.Errorf("a trace assertion under the text protocol should say to use eval_protocol: json: %+v", out)
	}
}

func TestEvalJSONProtocolRejectsAMalformedRunner(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh\neval_protocol: json", `cases:
  - name: c
    prompt: hi
    assert:
      contains: [x]
`, map[string]string{"runner.sh": "cat >/dev/null; echo 'INFO starting up'; echo '{\"text\":\"x\"}'\n"})
	out, r := runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "not a JSON object") {
		t.Errorf("a runner that logs to stdout under the json protocol should be told so: %+v", out)
	}
}

func TestEvalRubricIsGradedByTheJudge(t *testing.T) {
	// The judge reads the request JSON on stdin, insists on the rubric and the
	// subject it was sent, and rejects a response that admits it has no source.
	judge := `req=$(cat)
case "$req" in
  *'"rubric":"Cites a source."'*) ;;
  *) echo "judge got an unexpected request: $req" >&2; exit 3 ;;
esac
case "$req" in
  *'"subject":"greeter"'*) ;;
  *) echo "no subject" >&2; exit 3 ;;
esac
case "$req" in
  *'"response":"No idea'*) printf '%s' '{"pass":false,"reason":"no source cited"}' ;;
  *) printf '%s' '{"pass":true,"reason":"cites one"}' ;;
esac
`
	f := newEvalFixture(t, "eval_runner: sh runner.sh\njudge_runner: sh judge.sh", `cases:
  - name: cites
    prompt: give me a source
    assert:
      rubric: "Cites a source."
`, map[string]string{
		"runner.sh": "cat >/dev/null; echo 'According to the source, yes.'\n",
		"judge.sh":  judge,
	})
	out, r := runEval(t, f)
	if r.err != nil || out.Cases[0].Status != "passed" {
		t.Fatalf("a response the judge accepts should pass: %v %+v\n%s", r.err, out, r.stderr)
	}

	os.WriteFile(filepath.Join(f.skillDir, "runner.sh"), []byte("cat >/dev/null; echo 'No idea.'\n"), 0o755)
	out, r = runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "rubric not met: no source cited") {
		t.Errorf("a response the judge rejects should fail with its reason: %+v", out)
	}
}

func TestEvalRubricNeedsAJudgeAndAUsableVerdict(t *testing.T) {
	cases := `cases:
  - name: graded
    prompt: hi
    assert:
      rubric: "Is polite."
`
	f := newEvalFixture(t, "eval_runner: sh runner.sh", cases, map[string]string{"runner.sh": "cat >/dev/null; echo hi\n"})
	out, r := runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "no judge_runner is set") {
		t.Errorf("a rubric with no judge must fail loudly, not pass unchecked: %+v", out)
	}

	f = newEvalFixture(t, "eval_runner: sh runner.sh\njudge_runner: sh judge.sh", cases, map[string]string{
		"runner.sh": "cat >/dev/null; echo hi\n",
		"judge.sh":  "cat >/dev/null; echo 'looks fine to me'\n",
	})
	out, r = runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "judge output unusable") {
		t.Errorf("a judge that doesn't print a verdict must not silently pass or fail: %+v", out)
	}

	f = newEvalFixture(t, "eval_runner: sh runner.sh\njudge_runner: sh judge.sh", cases, map[string]string{
		"runner.sh": "cat >/dev/null; echo hi\n",
		"judge.sh":  "cat >/dev/null; exit 9\n",
	})
	out, r = runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "judge failed") {
		t.Errorf("a judge that exits non-zero should fail the case: %+v", out)
	}
}

func TestEvalRunsAndPassThreshold(t *testing.T) {
	// A runner that alternates: fails the 1st, 3rd, 5th run.
	runner := `cat >/dev/null
n=$(cat count 2>/dev/null || echo 0)
n=$((n+1))
echo $n > count
if [ $((n % 2)) -eq 1 ]; then echo "flaky wrong"; else echo "right answer"; fi
`
	cases := `cases:
  - name: mostly right
    prompt: hi
    runs: 5
    pass_threshold: 0.4
    assert:
      contains: ["right answer"]
  - name: must be right every time
    prompt: hi
    runs: 4
    assert:
      contains: ["right answer"]
`
	f := newEvalFixture(t, "eval_runner: sh runner.sh", cases, map[string]string{"runner.sh": runner})
	out, r := runEval(t, f)
	if r.err == nil {
		t.Fatal("the strict case should fail")
	}
	got := map[string]struct {
		status       string
		runs, passes int
		reason       string
	}{}
	for _, c := range out.Cases {
		got[c.Case] = struct {
			status       string
			runs, passes int
			reason       string
		}{c.Status, c.Runs, c.Passes, strings.Join(c.Reasons, " | ")}
	}
	// Runs 1..5 pass on the even ones: 2 of 5 = 0.4, which meets the threshold.
	if m := got["mostly right"]; m.status != "passed" || m.runs != 5 || m.passes != 2 {
		t.Errorf("mostly right = %+v, want 2 of 5 to meet a 0.4 threshold", m)
	}
	// The counter continues across cases: runs 6..9 -> pass on 6, 8 = 2 of 4, but all were required.
	if s := got["must be right every time"]; s.status != "failed" || s.runs != 4 || s.passes != 2 || !strings.Contains(s.reason, "2 of 4 runs passed") || !strings.Contains(s.reason, "run 2:") {
		t.Errorf("must be right every time = %+v", s)
	}
}

func TestEvalTimeoutStopsAHungRunner(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh", `cases:
  - name: hangs
    prompt: hi
    timeout: 1
    assert:
      contains: [x]
`, map[string]string{"runner.sh": "cat >/dev/null; sleep 30\n"})
	out, r := runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "timed out after 1s") {
		t.Errorf("a runner that hangs should time out: %+v", out)
	}
	if out.Cases[0].DurationMS > 10_000 {
		t.Errorf("the case took %dms; the timeout should have stopped it", out.Cases[0].DurationMS)
	}
}

func TestEvalRunnerSeesTheArtifactContext(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh", `cases:
  - name: env
    prompt: hi
    assert:
      contains: ["name=greeter", "kind=skill", "case=env", "role=runner", "protocol=text"]
`, map[string]string{"runner.sh": `cat >/dev/null
echo "name=$AGENTWORKS_ARTIFACT_NAME kind=$AGENTWORKS_ARTIFACT_KIND case=$AGENTWORKS_EVAL_CASE role=$AGENTWORKS_EVAL_ROLE protocol=$AGENTWORKS_EVAL_PROTOCOL"
test -d "$AGENTWORKS_ARTIFACT_DIR" && echo dir-exists
`})
	out, r := runEval(t, f)
	if r.err != nil || out.Cases[0].Status != "passed" {
		t.Errorf("the runner should see the artifact's context in its environment: %v %+v", r.err, out)
	}
}

func TestEvalProjectDefaultsApplyAndArtifactsOverrideThem(t *testing.T) {
	cases := `cases:
  - name: traced
    prompt: hi
    assert:
      tool_called: [search]
`
	// The project sets the runner and the json protocol; the artifact sets neither.
	f := newEvalFixture(t, "", cases, map[string]string{"runner.sh": jsonRunner})
	setProjectEval(t, f.dir, "eval:\n  default_runner: sh runner.sh\n  protocol: json\n  runs: 2")
	out, r := runEval(t, f)
	if r.err != nil || out.Cases[0].Status != "passed" || out.Cases[0].Runs != 2 {
		t.Errorf("project defaults should apply (json protocol, 2 runs): %v %+v", r.err, out)
	}

	// An artifact's own eval_protocol wins over the project's.
	skill := filepath.Join(f.skillDir, "skill.md")
	data, _ := os.ReadFile(skill)
	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", "version: 0.1.0\neval_protocol: text", 1)), 0o644)
	out, r = runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), "eval_protocol: json") {
		t.Errorf("the artifact's protocol should override the project's: %+v", out)
	}
}

func TestEvalRejectsABadProtocolAndSkipsWithoutARunner(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: echo hi\neval_protocol: xml", "cases:\n  - name: c\n    prompt: p\n    assert: {contains: [x]}\n", nil)
	// The registry rejects the value at validate time too, but eval must not run with it.
	out, r := runEval(t, f)
	if r.err == nil || !strings.Contains(strings.Join(out.Cases[0].Reasons, " "), `must be "text" or "json"`) {
		t.Errorf("a bad eval_protocol should fail clearly: %+v", out)
	}

	g := newEvalFixture(t, "", "cases:\n  - name: c\n    prompt: p\n    assert: {contains: [x]}\n", nil)
	out, r = runEval(t, g)
	if r.err != nil || out.Summary.Skipped != 1 || out.Cases[0].Status != "skipped" {
		t.Errorf("a case with no runner is skipped, not failed: %v %+v", r.err, out)
	}
}

func TestEvalWritesAJUnitReport(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh", `cases:
  - name: passes
    prompt: hi
    assert: {contains: [hello]}
  - name: fails
    prompt: hi
    assert: {contains: [nope]}
`, map[string]string{"runner.sh": "cat >/dev/null; echo hello\n"})
	report := filepath.Join(t.TempDir(), "eval.xml")
	r := runCLI(t, f.dir, "eval", "--junit", report)
	if r.err == nil {
		t.Fatal("one case fails, so eval should fail")
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("the JUnit report should be written even when cases fail: %v", err)
	}
	var doc struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Suites   []struct {
			Name  string `xml:"name,attr"`
			Cases []struct {
				Name string `xml:"name,attr"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the report is not XML: %v\n%s", err, data)
	}
	if doc.Tests != 2 || doc.Failures != 1 || len(doc.Suites) != 1 || doc.Suites[0].Name != "greeter" {
		t.Errorf("report = %+v", doc)
	}
	if r := runCLI(t, f.dir, "eval", "--junit", filepath.Join(t.TempDir(), "no-such-dir", "x.xml")); r.err == nil || !strings.Contains(r.err.Error(), "JUnit") {
		t.Errorf("an unwritable report path should be reported: %v", r.err)
	}
}

func TestEvalHumanOutputForMultipleRuns(t *testing.T) {
	f := newEvalFixture(t, "eval_runner: sh runner.sh", `cases:
  - name: steady
    prompt: hi
    runs: 3
    assert: {contains: [hello]}
`, map[string]string{"runner.sh": "cat >/dev/null; echo hello\n"})
	r := runCLI(t, f.dir, "eval")
	if r.err != nil || !strings.Contains(r.combined(), "passed (3 of 3 runs)") {
		t.Errorf("a multi-run case should report its pass rate: %v\n%s", r.err, r.combined())
	}
}
