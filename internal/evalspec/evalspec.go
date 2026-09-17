// Package evalspec is the file format and deterministic assertion checker
// behind `agentworks eval`: a directory of YAML case files describing
// prompts and what a response to them must (or must not) look like. It
// doesn't run anything itself -- see cmd/eval.go for how a case's prompt
// gets to a runner and its output gets back here for Evaluate.
package evalspec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Case is one behavior-eval case: a prompt to send to an artifact's eval
// runner, and what the runner's response must satisfy.
type Case struct {
	Name   string     `yaml:"name"`
	Prompt string     `yaml:"prompt"`
	Assert Assertions `yaml:"assert"`

	// File is the path this case was loaded from, set by LoadDir for error
	// messages and reporting -- not part of the YAML shape.
	File string `yaml:"-"`
}

// Assertions are the deterministic checks Evaluate runs against a runner's
// response. Every set field must pass for a case to pass. Contains/
// NotContains are case-insensitive substring checks; Matches/NotMatches
// are Go regular expressions.
type Assertions struct {
	Contains    []string `yaml:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty"`
	Matches     string   `yaml:"matches,omitempty"`
	NotMatches  string   `yaml:"not_matches,omitempty"`
	MaxLength   int      `yaml:"max_length,omitempty"`
	MinLength   int      `yaml:"min_length,omitempty"`
}

// caseFile is the top-level shape of one evals/*.yaml file.
type caseFile struct {
	Cases []Case `yaml:"cases"`
}

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
	for _, path := range matches {
		loaded, err := loadFile(path)
		if err != nil {
			return nil, err
		}
		cases = append(cases, loaded...)
	}
	return cases, nil
}

func loadFile(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cf caseFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i := range cf.Cases {
		cf.Cases[i].File = path
		if cf.Cases[i].Name == "" {
			return nil, fmt.Errorf("%s: case %d is missing a \"name\"", path, i+1)
		}
		if cf.Cases[i].Prompt == "" {
			return nil, fmt.Errorf("%s: case %q is missing a \"prompt\"", path, cf.Cases[i].Name)
		}
	}
	return cf.Cases, nil
}

// Evaluate checks output against assert, returning a human-readable
// failure reason per unmet condition. An empty result means output passes
// every assertion.
func Evaluate(assert Assertions, output string) []string {
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
