package evalspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// Runner protocols, set by an artifact's eval_protocol or the project's
// eval.protocol.
const (
	// ProtocolText: the runner prints the response text on stdout. The default.
	ProtocolText = "text"
	// ProtocolJSON: the runner prints exactly one JSON object on stdout:
	//
	//	{"text": "...",
	//	 "tool_calls": [{"name": "search", "arguments": {"query": "x"}}],
	//	 "activated": ["skill-name"],
	//	 "usage": {"input_tokens": 12, "output_tokens": 34}}
	//
	// Only "text" is required. A runner that supports the trace assertions
	// must include "tool_calls" and/or "activated" (an empty list means "none",
	// and an absent key means "not reported", which is an error for a case that
	// asserts on it).
	ProtocolJSON = "json"
)

// ValidProtocol reports whether p names a runner protocol.
func ValidProtocol(p string) bool { return p == ProtocolText || p == ProtocolJSON }

// ToolCall is one tool invocation a model made.
type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// Usage is what a runner reports about tokens, when it can.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// Response is what a runner returned for one prompt.
type Response struct {
	Text      string
	ToolCalls []ToolCall
	Activated []string
	Usage     Usage

	// HasToolCalls and HasActivated say whether the runner reported those at
	// all, as distinct from reporting none.
	HasToolCalls bool
	HasActivated bool
}

// ParseResponse turns a runner's stdout into a Response under protocol.
func ParseResponse(protocol, stdout string) (Response, error) {
	if protocol != ProtocolJSON {
		return Response{Text: stdout}, nil
	}

	trimmed := bytes.TrimSpace([]byte(stdout))
	if len(trimmed) == 0 {
		return Response{}, fmt.Errorf("the runner printed nothing, but eval_protocol is json (expected one JSON object)")
	}
	var raw struct {
		Text      *string         `json:"text"`
		ToolCalls json.RawMessage `json:"tool_calls"`
		Activated json.RawMessage `json:"activated"`
		Usage     Usage           `json:"usage"`
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(&raw); err != nil {
		return Response{}, fmt.Errorf("the runner's output is not a JSON object (eval_protocol is json): %v", err)
	}
	if dec.More() {
		return Response{}, fmt.Errorf("the runner printed more than one JSON value; under eval_protocol: json stdout must be exactly one object (send logs to stderr)")
	}
	if raw.Text == nil {
		return Response{}, fmt.Errorf("the runner's JSON has no \"text\" field")
	}

	resp := Response{Text: *raw.Text, Usage: raw.Usage}
	if len(raw.ToolCalls) > 0 && string(raw.ToolCalls) != "null" {
		if err := json.Unmarshal(raw.ToolCalls, &resp.ToolCalls); err != nil {
			return Response{}, fmt.Errorf("\"tool_calls\" must be a list of {name, arguments}: %v", err)
		}
		resp.HasToolCalls = true
	}
	if len(raw.Activated) > 0 && string(raw.Activated) != "null" {
		if err := json.Unmarshal(raw.Activated, &resp.Activated); err != nil {
			return Response{}, fmt.Errorf("\"activated\" must be a list of names: %v", err)
		}
		resp.HasActivated = true
	}
	return resp, nil
}

// Evaluate checks a response against a case's assertions and should_trigger,
// returning a human-readable reason for each one that fails; an empty result
// means every check passed. subject is the name of the artifact under test,
// which should_trigger refers to. The rubric is not checked here: it needs a
// judge (see Judge), so a case with one passes Evaluate only on its other
// assertions.
func Evaluate(c Case, resp Response, subject string) []string {
	failures := evaluateText(c.Assert, resp.Text)
	failures = append(failures, evaluateTrace(c, resp, subject)...)
	return failures
}

func evaluateText(assert Assertions, output string) []string {
	var failures []string
	lower := strings.ToLower(output)

	for _, want := range assert.Contains {
		if !strings.Contains(lower, strings.ToLower(want)) {
			failures = append(failures, fmt.Sprintf("expected output to contain %q", want))
		}
	}
	for _, unwanted := range assert.NotContains {
		if strings.Contains(lower, strings.ToLower(unwanted)) {
			failures = append(failures, fmt.Sprintf("expected output not to contain %q", unwanted))
		}
	}
	if assert.Matches != "" {
		re, err := regexp.Compile(assert.Matches)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid \"matches\" regex %q: %v", assert.Matches, err))
		} else if !re.MatchString(output) {
			failures = append(failures, fmt.Sprintf("expected output to match %q", assert.Matches))
		}
	}
	if assert.NotMatches != "" {
		re, err := regexp.Compile(assert.NotMatches)
		if err != nil {
			failures = append(failures, fmt.Sprintf("invalid \"not_matches\" regex %q: %v", assert.NotMatches, err))
		} else if re.MatchString(output) {
			failures = append(failures, fmt.Sprintf("expected output not to match %q", assert.NotMatches))
		}
	}
	if assert.MaxLength > 0 && len(output) > assert.MaxLength {
		failures = append(failures, fmt.Sprintf("expected output no longer than %d characters, got %d", assert.MaxLength, len(output)))
	}
	if assert.MinLength > 0 && len(output) < assert.MinLength {
		failures = append(failures, fmt.Sprintf("expected output at least %d characters, got %d", assert.MinLength, len(output)))
	}
	return failures
}

func evaluateTrace(c Case, resp Response, subject string) []string {
	a := c.Assert
	var failures []string

	needsCalls := len(a.ToolCalled) > 0 || len(a.ToolNotCalled) > 0 || len(a.ToolArgs) > 0
	needsActivation := len(a.Activated) > 0 || len(a.NotActivated) > 0 || c.ShouldTrigger != nil
	if needsCalls && !resp.HasToolCalls {
		failures = append(failures, "the runner didn't report \"tool_calls\", so the tool assertions can't be checked (report an empty list if none were made)")
	}
	if needsActivation && !resp.HasActivated {
		failures = append(failures, "the runner didn't report \"activated\", so the activation assertions can't be checked (report an empty list if none were)")
	}
	if len(failures) > 0 {
		return failures
	}

	called := map[string]bool{}
	for _, call := range resp.ToolCalls {
		called[call.Name] = true
	}
	for _, name := range a.ToolCalled {
		if !called[name] {
			failures = append(failures, fmt.Sprintf("expected tool %q to be called, but it wasn't (called: %s)", name, listOrNone(sortedKeys(called))))
		}
	}
	for _, name := range a.ToolNotCalled {
		if called[name] {
			failures = append(failures, fmt.Sprintf("expected tool %q not to be called, but it was", name))
		}
	}
	for _, ta := range a.ToolArgs {
		if reason := checkToolArgs(ta, resp.ToolCalls); reason != "" {
			failures = append(failures, reason)
		}
	}

	activated := map[string]bool{}
	for _, name := range resp.Activated {
		activated[name] = true
	}
	for _, name := range a.Activated {
		if !activated[name] {
			failures = append(failures, fmt.Sprintf("expected %q to be activated, but it wasn't (activated: %s)", name, listOrNone(sortedKeys(activated))))
		}
	}
	for _, name := range a.NotActivated {
		if activated[name] {
			failures = append(failures, fmt.Sprintf("expected %q not to be activated, but it was", name))
		}
	}
	if c.ShouldTrigger != nil {
		switch got := activated[subject]; {
		case *c.ShouldTrigger && !got:
			failures = append(failures, fmt.Sprintf("expected %q to trigger for this prompt, but it didn't (activated: %s) -- its description may not say when to use it", subject, listOrNone(sortedKeys(activated))))
		case !*c.ShouldTrigger && got:
			failures = append(failures, fmt.Sprintf("expected %q not to trigger for this prompt, but it did -- its description may be too broad", subject))
		}
	}
	return failures
}

// checkToolArgs passes if any call to ta.Tool satisfies every condition, and
// otherwise explains what was closest.
func checkToolArgs(ta ToolArgs, calls []ToolCall) string {
	var toTool []ToolCall
	for _, c := range calls {
		if c.Name == ta.Tool {
			toTool = append(toTool, c)
		}
	}
	if len(toTool) == 0 {
		return fmt.Sprintf("expected a call to %q with specific arguments, but it wasn't called", ta.Tool)
	}

	var why string
	for _, call := range toTool {
		reason := argMismatch(ta, call.Arguments)
		if reason == "" {
			return ""
		}
		why = reason
	}
	if len(toTool) > 1 {
		return fmt.Sprintf("none of the %d calls to %q had the expected arguments (last: %s)", len(toTool), ta.Tool, why)
	}
	return fmt.Sprintf("the call to %q had unexpected arguments: %s", ta.Tool, why)
}

func argMismatch(ta ToolArgs, args map[string]any) string {
	for _, key := range sortedAnyKeys(ta.Equals) {
		got, present := args[key]
		if !present {
			return fmt.Sprintf("%q was not passed", key)
		}
		if !jsonEqual(got, ta.Equals[key]) {
			return fmt.Sprintf("%q was %v, want %v", key, got, ta.Equals[key])
		}
	}
	for _, key := range sortedStringKeys(ta.Matches) {
		got, present := args[key]
		if !present {
			return fmt.Sprintf("%q was not passed", key)
		}
		re, err := regexp.Compile(ta.Matches[key])
		if err != nil {
			return fmt.Sprintf("invalid regex %q for %q: %v", ta.Matches[key], key, err)
		}
		if s := stringForm(got); !re.MatchString(s) {
			return fmt.Sprintf("%q was %q, which doesn't match %q", key, s, ta.Matches[key])
		}
	}
	return ""
}

// jsonEqual compares two values after normalizing both through JSON, so a YAML
// integer equals the float64 a JSON decoder produces.
func jsonEqual(a, b any) bool {
	na, err1 := normalize(a)
	nb, err2 := normalize(b)
	return err1 == nil && err2 == nil && reflect.DeepEqual(na, nb)
}

func normalize(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(data, &out)
	return out, err
}

// stringForm renders an argument value for regex matching: a string as is,
// anything else as compact JSON.
func stringForm(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
