package evalspec

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCases(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cases.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func boolPtr(b bool) *bool { return &b }

// ---- loading --------------------------------------------------------------

func TestLoadDirRejectsATypoInsteadOfMakingAVacuousCase(t *testing.T) {
	dir := writeCases(t, `cases:
  - name: typo
    prompt: hi
    assert:
      contians: ["hello"]
`)
	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "contians") {
		t.Errorf("LoadDir() error = %v, want the unknown key named (a typo would otherwise assert nothing)", err)
	}
}

func TestLoadDirRejectsACaseThatAssertsNothing(t *testing.T) {
	dir := writeCases(t, `cases:
  - name: empty
    prompt: hi
`)
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "asserts nothing") {
		t.Errorf("LoadDir() error = %v, want it to refuse a case that passes for any response", err)
	}
}

func TestLoadDirParsesTheV2Fields(t *testing.T) {
	dir := writeCases(t, `cases:
  - name: uses-search
    prompt: find it
    runs: 5
    pass_threshold: 0.8
    timeout: 30
    assert:
      tool_called: [search]
      tool_not_called: [delete]
      tool_args:
        - tool: search
          equals: {limit: 10}
          matches: {query: "^unit"}
      activated: [researcher]
      not_activated: [other]
      rubric: "Cites at least one source."
  - name: triggers
    prompt: analyze this csv
    should_trigger: true
  - name: stays-quiet
    prompt: what is 2+2
    should_trigger: false
`)
	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("got %d cases", len(cases))
	}
	c := cases[0]
	if c.Runs != 5 || c.PassThreshold != 0.8 || c.Timeout != 30 {
		t.Errorf("runs/threshold/timeout = %d/%v/%d", c.Runs, c.PassThreshold, c.Timeout)
	}
	if len(c.Assert.ToolArgs) != 1 || c.Assert.ToolArgs[0].Equals["limit"] != 10 || c.Assert.ToolArgs[0].Matches["query"] != "^unit" {
		t.Errorf("tool_args = %+v", c.Assert.ToolArgs)
	}
	if !c.NeedsTrace() || !c.NeedsJudge() {
		t.Error("case 0 asserts on the trace and has a rubric")
	}
	if cases[1].ShouldTrigger == nil || !*cases[1].ShouldTrigger || cases[2].ShouldTrigger == nil || *cases[2].ShouldTrigger {
		t.Error("should_trigger true/false not parsed")
	}
	if !cases[1].NeedsTrace() {
		t.Error("a trigger case needs the activation trace")
	}
}

func TestLoadDirValidatesFields(t *testing.T) {
	tests := map[string]string{
		"negative runs":       "runs: -1\n    assert: {contains: [x]}",
		"threshold too big":   "pass_threshold: 1.5\n    assert: {contains: [x]}",
		"negative timeout":    "timeout: -3\n    assert: {contains: [x]}",
		"bad matches regex":   "assert: {matches: \"(\"}",
		"bad not_matches":     "assert: {not_matches: \"[\"}",
		"tool_args no tool":   "assert:\n      tool_args:\n        - equals: {a: 1}",
		"tool_args no checks": "assert:\n      tool_args:\n        - tool: t",
		"tool_args bad regex": "assert:\n      tool_args:\n        - tool: t\n          matches: {a: \"(\"}",
	}
	for name, body := range tests {
		dir := writeCases(t, "cases:\n  - name: c\n    prompt: p\n    "+body+"\n")
		if _, err := LoadDir(dir); err == nil {
			t.Errorf("%s: LoadDir() expected an error", name)
		}
	}
}

func TestLoadDirRejectsDuplicateNamesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	one := "cases:\n  - name: same\n    prompt: p\n    assert: {contains: [x]}\n"
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(one), 0o644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(one), 0o644)
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Errorf("LoadDir() error = %v, want the duplicate refused (--case would be ambiguous)", err)
	}
}

func TestValidateProtocol(t *testing.T) {
	trace := Case{Name: "t", Assert: Assertions{ToolCalled: []string{"x"}}}
	plain := Case{Name: "p", Assert: Assertions{Contains: []string{"x"}}}
	trigger := Case{Name: "g", ShouldTrigger: boolPtr(true)}

	for _, c := range []Case{trace, trigger} {
		if err := c.ValidateProtocol(ProtocolText); err == nil || !strings.Contains(err.Error(), "eval_protocol: json") {
			t.Errorf("%s under text: error = %v, want it to say to use eval_protocol: json", c.Name, err)
		}
		if err := c.ValidateProtocol(ProtocolJSON); err != nil {
			t.Errorf("%s under json: %v", c.Name, err)
		}
	}
	if err := plain.ValidateProtocol(ProtocolText); err != nil {
		t.Errorf("a plain text case works under the text protocol: %v", err)
	}
}

// ---- the response protocol ------------------------------------------------

func TestParseResponse(t *testing.T) {
	text, err := ParseResponse(ProtocolText, "hello\n")
	if err != nil || text.Text != "hello\n" || text.HasToolCalls || text.HasActivated {
		t.Errorf("text protocol = %+v, %v", text, err)
	}

	full, err := ParseResponse(ProtocolJSON, `  {"text":"hi","tool_calls":[{"name":"search","arguments":{"q":"x","n":2}}],"activated":["a","b"],"usage":{"input_tokens":3,"output_tokens":4}}  `)
	if err != nil {
		t.Fatalf("ParseResponse(json) error = %v", err)
	}
	if full.Text != "hi" || len(full.ToolCalls) != 1 || full.ToolCalls[0].Arguments["q"] != "x" || len(full.Activated) != 2 {
		t.Errorf("parsed = %+v", full)
	}
	if !full.HasToolCalls || !full.HasActivated || full.Usage.InputTokens != 3 || full.Usage.OutputTokens != 4 {
		t.Errorf("trace flags / usage = %+v", full)
	}

	// An empty list means "none"; an absent key means "not reported".
	empty, _ := ParseResponse(ProtocolJSON, `{"text":"x","tool_calls":[],"activated":[]}`)
	if !empty.HasToolCalls || !empty.HasActivated {
		t.Errorf("empty lists are still reported: %+v", empty)
	}
	absent, _ := ParseResponse(ProtocolJSON, `{"text":"x"}`)
	if absent.HasToolCalls || absent.HasActivated {
		t.Errorf("absent keys are not reported: %+v", absent)
	}
	null, _ := ParseResponse(ProtocolJSON, `{"text":"x","tool_calls":null,"activated":null}`)
	if null.HasToolCalls || null.HasActivated {
		t.Errorf("null is not reported: %+v", null)
	}
}

func TestParseResponseErrors(t *testing.T) {
	tests := map[string]string{
		"empty":            "",
		"not json":         "just some text",
		"no text field":    `{"tool_calls":[]}`,
		"two objects":      `{"text":"a"}{"text":"b"}`,
		"bad tool_calls":   `{"text":"a","tool_calls":"nope"}`,
		"bad activated":    `{"text":"a","activated":[1,2]}`,
		"log then payload": "INFO starting\n{\"text\":\"a\"}",
	}
	for name, out := range tests {
		if _, err := ParseResponse(ProtocolJSON, out); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if !ValidProtocol("text") || !ValidProtocol("json") || ValidProtocol("xml") || ValidProtocol("") {
		t.Error("ValidProtocol wrong")
	}
}

// ---- trace assertions -----------------------------------------------------

func traceResp() Response {
	return Response{
		Text:         "done",
		HasToolCalls: true, HasActivated: true,
		ToolCalls: []ToolCall{
			{Name: "search", Arguments: map[string]any{"query": "unit tests", "limit": float64(10), "tags": []any{"a", "b"}}},
			{Name: "read", Arguments: map[string]any{"path": "/tmp/x"}},
		},
		Activated: []string{"researcher"},
	}
}

func TestEvaluateToolAssertions(t *testing.T) {
	tests := []struct {
		name     string
		assert   Assertions
		wantFail string // "" = should pass
	}{
		{"called", Assertions{ToolCalled: []string{"search", "read"}}, ""},
		{"not called", Assertions{ToolNotCalled: []string{"delete"}}, ""},
		{"a required tool wasn't called", Assertions{ToolCalled: []string{"delete"}}, `expected tool "delete" to be called`},
		{"a forbidden tool was called", Assertions{ToolNotCalled: []string{"search"}}, `expected tool "search" not to be called`},
		{"args equal (int vs float)", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Equals: map[string]any{"limit": 10}}}}, ""},
		{"args equal a list", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Equals: map[string]any{"tags": []any{"a", "b"}}}}}, ""},
		{"args equal mismatch", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Equals: map[string]any{"limit": 5}}}}, `"limit" was 10, want 5`},
		{"args match", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Matches: map[string]string{"query": "^unit"}}}}, ""},
		{"args match on a non-string", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Matches: map[string]string{"tags": `\["a"`}}}}, ""},
		{"args match mismatch", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Matches: map[string]string{"query": "^integration"}}}}, "doesn't match"},
		{"arg not passed", Assertions{ToolArgs: []ToolArgs{{Tool: "search", Equals: map[string]any{"page": 1}}}}, `"page" was not passed`},
		{"args for an uncalled tool", Assertions{ToolArgs: []ToolArgs{{Tool: "delete", Equals: map[string]any{"x": 1}}}}, "wasn't called"},
		{"activated", Assertions{Activated: []string{"researcher"}}, ""},
		{"not activated", Assertions{NotActivated: []string{"other"}}, ""},
		{"a required skill wasn't activated", Assertions{Activated: []string{"other"}}, `"other" to be activated`},
		{"a forbidden skill was activated", Assertions{NotActivated: []string{"researcher"}}, `"researcher" not to be activated`},
	}
	for _, tt := range tests {
		got := Evaluate(Case{Name: tt.name, Assert: tt.assert}, traceResp(), "researcher")
		switch {
		case tt.wantFail == "" && len(got) != 0:
			t.Errorf("%s: unexpected failures %v", tt.name, got)
		case tt.wantFail != "" && !strings.Contains(strings.Join(got, "\n"), tt.wantFail):
			t.Errorf("%s: failures %v, want one mentioning %q", tt.name, got, tt.wantFail)
		}
	}
}

func TestToolArgsPassesIfAnyCallMatches(t *testing.T) {
	resp := Response{HasToolCalls: true, ToolCalls: []ToolCall{
		{Name: "search", Arguments: map[string]any{"q": "wrong"}},
		{Name: "search", Arguments: map[string]any{"q": "right"}},
	}}
	pass := Evaluate(Case{Assert: Assertions{ToolArgs: []ToolArgs{{Tool: "search", Equals: map[string]any{"q": "right"}}}}}, resp, "s")
	if len(pass) != 0 {
		t.Errorf("one matching call among several should pass: %v", pass)
	}
	fail := Evaluate(Case{Assert: Assertions{ToolArgs: []ToolArgs{{Tool: "search", Equals: map[string]any{"q": "neither"}}}}}, resp, "s")
	if len(fail) != 1 || !strings.Contains(fail[0], "none of the 2 calls") {
		t.Errorf("failures = %v, want a message counting the calls", fail)
	}
}

func TestEvaluateTriggerCases(t *testing.T) {
	yes, no := boolPtr(true), boolPtr(false)
	activated := Response{HasActivated: true, Activated: []string{"csv-analyzer"}}
	quiet := Response{HasActivated: true, Activated: []string{}}

	if f := Evaluate(Case{ShouldTrigger: yes}, activated, "csv-analyzer"); len(f) != 0 {
		t.Errorf("should_trigger true and it did: %v", f)
	}
	if f := Evaluate(Case{ShouldTrigger: no}, quiet, "csv-analyzer"); len(f) != 0 {
		t.Errorf("should_trigger false and it didn't: %v", f)
	}
	if f := Evaluate(Case{ShouldTrigger: yes}, quiet, "csv-analyzer"); len(f) != 1 || !strings.Contains(f[0], "description may not say when to use it") {
		t.Errorf("a missed trigger should point at the description: %v", f)
	}
	if f := Evaluate(Case{ShouldTrigger: no}, activated, "csv-analyzer"); len(f) != 1 || !strings.Contains(f[0], "too broad") {
		t.Errorf("a false trigger should point at the description: %v", f)
	}
}

func TestEvaluateRefusesToGuessWhenTheRunnerDidNotReportTheTrace(t *testing.T) {
	notReported := Response{Text: "x"} // json protocol, but no tool_calls / activated keys
	f := Evaluate(Case{Assert: Assertions{ToolNotCalled: []string{"delete"}}}, notReported, "s")
	if len(f) != 1 || !strings.Contains(f[0], `didn't report "tool_calls"`) {
		t.Errorf("a not-called assertion must not pass just because nothing was reported: %v", f)
	}
	f = Evaluate(Case{ShouldTrigger: boolPtr(false)}, notReported, "s")
	if len(f) != 1 || !strings.Contains(f[0], `didn't report "activated"`) {
		t.Errorf("should_trigger false must not pass just because nothing was reported: %v", f)
	}
}

func TestEvaluateCombinesTextAndTraceFailures(t *testing.T) {
	c := Case{Assert: Assertions{Contains: []string{"nope"}, ToolCalled: []string{"delete"}}}
	if f := Evaluate(c, traceResp(), "researcher"); len(f) != 2 {
		t.Errorf("both failures should be reported, got %v", f)
	}
}

// ---- judge ----------------------------------------------------------------

func TestParseVerdict(t *testing.T) {
	v, err := ParseVerdict(` {"pass": true, "reason": "cites two sources"} `)
	if err != nil || !v.Pass || v.Reason != "cites two sources" {
		t.Errorf("ParseVerdict() = %+v, %v", v, err)
	}
	v, err = ParseVerdict(`{"pass": false, "reason": "no sources"}`)
	if err != nil || v.Pass {
		t.Errorf("a false verdict = %+v, %v", v, err)
	}
	for name, out := range map[string]string{
		"empty":         "",
		"not json":      "looks good to me",
		"no pass field": `{"reason":"fine"}`,
		"two objects":   `{"pass":true}{"pass":false}`,
	} {
		if _, err := ParseVerdict(out); err == nil {
			t.Errorf("%s: expected an error (a judge that prints something else must not silently pass or fail cases)", name)
		}
	}
}

// ---- junit ----------------------------------------------------------------

func TestWriteJUnit(t *testing.T) {
	results := []Result{
		{Artifact: "researcher", Case: "cites sources", Outcome: OutcomePassed, Duration: 1500 * time.Millisecond, Runs: 3, Passes: 3},
		{Artifact: "researcher", Case: "stays brief", Outcome: OutcomeFailed, Reasons: []string{`expected output not to contain "TODO"`, "second reason"}, Duration: time.Second, Runs: 5, Passes: 2},
		{Artifact: "csv-analyzer", Case: "no runner", Outcome: OutcomeSkipped, Reasons: []string{"no eval_runner set"}},
		{Artifact: "researcher", Case: "escapes <xml> & \"quotes\"", Outcome: OutcomeFailed, Reasons: []string{"a < b & c"}},
	}
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, results); err != nil {
		t.Fatalf("WriteJUnit() error = %v", err)
	}

	var doc struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Skipped  int      `xml:"skipped,attr"`
		Suites   []struct {
			Name     string `xml:"name,attr"`
			Tests    int    `xml:"tests,attr"`
			Failures int    `xml:"failures,attr"`
			Skipped  int    `xml:"skipped,attr"`
			Time     string `xml:"time,attr"`
			Cases    []struct {
				Name    string `xml:"name,attr"`
				Failure *struct {
					Message string `xml:"message,attr"`
					Body    string `xml:",chardata"`
				} `xml:"failure"`
				Skipped   *struct{} `xml:"skipped"`
				SystemOut string    `xml:"system-out"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("the report is not well-formed XML: %v\n%s", err, buf.String())
	}
	if doc.Tests != 4 || doc.Failures != 2 || doc.Skipped != 1 || len(doc.Suites) != 2 {
		t.Errorf("totals = %d tests, %d failures, %d skipped, %d suites", doc.Tests, doc.Failures, doc.Skipped, len(doc.Suites))
	}
	researcher := doc.Suites[0]
	if researcher.Name != "researcher" || researcher.Tests != 3 || researcher.Failures != 2 || researcher.Time != "2.500" {
		t.Errorf("researcher suite = %+v", researcher)
	}
	if fc := researcher.Cases[1]; fc.Failure == nil || !strings.Contains(fc.Failure.Message, "TODO") || !strings.Contains(fc.Failure.Body, "second reason") || fc.SystemOut != "2 of 5 runs passed" {
		t.Errorf("failed case = %+v", fc)
	}
	if researcher.Cases[0].SystemOut != "3 of 3 runs passed" {
		t.Errorf("a passing multi-run case should report its pass rate: %q", researcher.Cases[0].SystemOut)
	}
	if doc.Suites[1].Cases[0].Skipped == nil || doc.Suites[1].Skipped != 1 {
		t.Errorf("skipped case = %+v", doc.Suites[1])
	}
	if !strings.Contains(researcher.Cases[2].Name, "<xml> & \"quotes\"") {
		t.Errorf("special characters must survive the XML round trip: %q", researcher.Cases[2].Name)
	}
}
