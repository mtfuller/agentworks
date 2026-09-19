// Package evalspec is the file format, response model, and assertion checker
// behind `agentworks eval`: a directory of YAML case files describing prompts
// and what a response to them must (or must not) look like. It doesn't run
// anything itself -- see cmd/eval.go for how a case's prompt gets to a runner
// and its output gets back here.
//
// A runner speaks one of two protocols (see ParseResponse): plain text on
// stdout, or one JSON document that also reports the tool calls the model made
// and which skills it activated. The text protocol supports the string
// assertions; the JSON protocol adds assertions on the trace.
package evalspec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Case is one behavior-eval case: a prompt to send to an artifact's eval
// runner, and what the response must satisfy.
type Case struct {
	Name   string     `yaml:"name"`
	Prompt string     `yaml:"prompt"`
	Assert Assertions `yaml:"assert"`

	// ShouldTrigger makes this a trigger case: true asserts the artifact under
	// test was activated for the prompt, false asserts it was not. It tests
	// the artifact's description -- the text a model reads to decide whether to
	// use it -- which is the commonest way a skill fails. It needs a runner
	// that reports activations (protocol json).
	ShouldTrigger *bool `yaml:"should_trigger,omitempty"`

	// Runs repeats the case, for a nondeterministic runner; the case passes if
	// at least PassThreshold of the runs do. Zero means "use the project
	// default", and ultimately one run.
	Runs int `yaml:"runs,omitempty"`
	// PassThreshold is the fraction of runs (greater than 0, at most 1) that
	// must pass. Zero means all of them.
	PassThreshold float64 `yaml:"pass_threshold,omitempty"`
	// Timeout is seconds to wait for the runner per run. Zero means the
	// project default.
	Timeout int `yaml:"timeout,omitempty"`

	// File is the path this case was loaded from, set by LoadDir for error
	// messages and reporting -- not part of the YAML shape.
	File string `yaml:"-"`
}

// Assertions are the checks Evaluate runs against a response. Every set field
// must pass for a case to pass. Contains/NotContains are case-insensitive
// substring checks on the response text; Matches/NotMatches are Go regular
// expressions on it. The tool_* and activated fields check the trace a json
// runner reports; Rubric is graded by a judge command (see judge.go), which
// AgentWorks runs but which is the project's own.
type Assertions struct {
	Contains    []string `yaml:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty"`
	Matches     string   `yaml:"matches,omitempty"`
	NotMatches  string   `yaml:"not_matches,omitempty"`
	MaxLength   int      `yaml:"max_length,omitempty"`
	MinLength   int      `yaml:"min_length,omitempty"`

	// ToolCalled lists tools that must have been called at least once;
	// ToolNotCalled tools that must not have been.
	ToolCalled    []string `yaml:"tool_called,omitempty"`
	ToolNotCalled []string `yaml:"tool_not_called,omitempty"`
	// ToolArgs asserts on a tool call's arguments: at least one call to the
	// named tool must satisfy every condition.
	ToolArgs []ToolArgs `yaml:"tool_args,omitempty"`

	// Activated lists skills that must have been activated; NotActivated
	// skills that must not have been.
	Activated    []string `yaml:"activated,omitempty"`
	NotActivated []string `yaml:"not_activated,omitempty"`

	// Rubric is criteria a judge grades the response against.
	Rubric string `yaml:"rubric,omitempty"`
}

// ToolArgs is one condition on a tool call's arguments. Equals compares
// argument values exactly (after JSON normalization); Matches applies a Go
// regular expression to the argument's string form.
type ToolArgs struct {
	Tool    string            `yaml:"tool"`
	Equals  map[string]any    `yaml:"equals,omitempty"`
	Matches map[string]string `yaml:"matches,omitempty"`
}

// caseFile is the top-level shape of one evals/*.yaml file.
type caseFile struct {
	Cases []Case `yaml:"cases"`
}

// NeedsTrace reports whether the case asserts on the tool-call trace or on
// activation, which only a json-protocol runner can report.
func (c Case) NeedsTrace() bool {
	a := c.Assert
	return len(a.ToolCalled) > 0 || len(a.ToolNotCalled) > 0 || len(a.ToolArgs) > 0 ||
		len(a.Activated) > 0 || len(a.NotActivated) > 0 || c.ShouldTrigger != nil
}

// NeedsJudge reports whether the case has a rubric to grade.
func (c Case) NeedsJudge() bool { return strings.TrimSpace(c.Assert.Rubric) != "" }

// LoadDir loads every *.yaml/*.yml file directly inside dir (no recursion
// into subdirectories) and returns their cases concatenated, in filename
// order. It returns (nil, nil) if dir doesn't exist or has no eval files --
// having no evals is not an error, since most artifacts won't have any.
func LoadDir(dir string) ([]Case, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	ymlMatches, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	matches = append(matches, ymlMatches...)
	sort.Strings(matches)

	var cases []Case
	seen := map[string]string{}
	for _, path := range matches {
		loaded, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		for _, c := range loaded {
			if prev, dup := seen[c.Name]; dup {
				return nil, fmt.Errorf("%s: case %q is already defined in %s (names identify cases for --case, so they must be unique)", path, c.Name, prev)
			}
			seen[c.Name] = path
		}
		cases = append(cases, loaded...)
	}
	return cases, nil
}

// loadFile parses one case file strictly: an unknown key is an error, because
// a typo such as "contians:" would otherwise leave a case that asserts nothing
// and passes for any response.
func loadFile(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cf caseFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cf); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i := range cf.Cases {
		cf.Cases[i].File = path
		if err := cf.Cases[i].validate(i); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return cf.Cases, nil
}

func (c Case) validate(index int) error {
	if c.Name == "" {
		return fmt.Errorf("case %d is missing a \"name\"", index+1)
	}
	if c.Prompt == "" {
		return fmt.Errorf("case %q is missing a \"prompt\"", c.Name)
	}
	if c.Runs < 0 {
		return fmt.Errorf("case %q: \"runs\" can't be negative", c.Name)
	}
	if c.PassThreshold < 0 || c.PassThreshold > 1 {
		return fmt.Errorf("case %q: \"pass_threshold\" must be between 0 and 1 (a fraction of runs), got %v", c.Name, c.PassThreshold)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("case %q: \"timeout\" can't be negative", c.Name)
	}
	a := c.Assert
	for field, pattern := range map[string]string{"matches": a.Matches, "not_matches": a.NotMatches} {
		if pattern == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("case %q: invalid %q regex %q: %v", c.Name, field, pattern, err)
		}
	}
	for i, ta := range a.ToolArgs {
		switch {
		case ta.Tool == "":
			return fmt.Errorf("case %q: tool_args[%d] has no \"tool\"", c.Name, i)
		case len(ta.Equals) == 0 && len(ta.Matches) == 0:
			return fmt.Errorf("case %q: tool_args[%d] (%s) sets neither \"equals\" nor \"matches\"", c.Name, i, ta.Tool)
		}
		for arg, pattern := range ta.Matches {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("case %q: tool_args[%d] (%s): invalid regex for %q: %v", c.Name, i, ta.Tool, arg, err)
			}
		}
	}
	if !c.assertsSomething() {
		return fmt.Errorf("case %q asserts nothing, so it would pass for any response -- add an assertion (or should_trigger)", c.Name)
	}
	return nil
}

func (c Case) assertsSomething() bool {
	a := c.Assert
	return len(a.Contains) > 0 || len(a.NotContains) > 0 || a.Matches != "" || a.NotMatches != "" ||
		a.MaxLength > 0 || a.MinLength > 0 || c.NeedsTrace() || c.NeedsJudge()
}

// ValidateProtocol reports an error when the case asserts on the trace but the
// runner speaks the text protocol, which can't report one.
func (c Case) ValidateProtocol(protocol string) error {
	if c.NeedsTrace() && protocol != ProtocolJSON {
		return fmt.Errorf("case %q asserts on the tool-call trace or on activation, which needs a runner that reports it: set eval_protocol: json", c.Name)
	}
	return nil
}
