package evalspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDirNoFiles(t *testing.T) {
	dir := t.TempDir()
	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir on empty dir: %v", err)
	}
	if len(cases) != 0 {
		t.Fatalf("LoadDir on empty dir = %v, want empty", cases)
	}
}

func TestLoadDirParsesCasesInFilenameOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.yaml", `
cases:
  - name: second
    prompt: "hello"
    assert:
      contains: ["hi"]
`)
	writeFile(t, dir, "a.yaml", `
cases:
  - name: first
    prompt: "hello"
    assert:
      max_length: 10
`)

	cases, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("LoadDir returned %d cases, want 2", len(cases))
	}
	if cases[0].Name != "first" || cases[1].Name != "second" {
		t.Fatalf("LoadDir order = [%s, %s], want [first, second]", cases[0].Name, cases[1].Name)
	}
}

func TestLoadDirRejectsMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"missing name", `cases:
  - prompt: "hello"
    assert:
      contains: ["hi"]
`},
		{"missing prompt", `cases:
  - name: no-prompt
    assert:
      contains: ["hi"]
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "cases.yaml", tt.content)
			if _, err := LoadDir(dir); err == nil {
				t.Fatal("LoadDir with a malformed case = nil error, want an error")
			}
		})
	}
}

func TestLoadDirRejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "broken.yaml", "cases: [this is not valid yaml :::")
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("LoadDir with invalid YAML = nil error, want an error")
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name    string
		assert  Assertions
		output  string
		wantErr bool
	}{
		{"contains passes case-insensitively", Assertions{Contains: []string{"Outlier"}}, "found an outlier row", false},
		{"contains fails when absent", Assertions{Contains: []string{"outlier"}}, "nothing unusual here", true},
		{"not_contains passes when absent", Assertions{NotContains: []string{"error"}}, "all good", false},
		{"not_contains fails when present", Assertions{NotContains: []string{"error"}}, "an ERROR occurred", true},
		{"matches passes on regex hit", Assertions{Matches: `\d+ rows`}, "flagged 3 rows", false},
		{"matches fails on regex miss", Assertions{Matches: `\d+ rows`}, "flagged some rows", true},
		{"not_matches passes on regex miss", Assertions{NotMatches: `\d+ rows`}, "flagged some rows", false},
		{"not_matches fails on regex hit", Assertions{NotMatches: `\d+ rows`}, "flagged 3 rows", true},
		{"max_length passes under limit", Assertions{MaxLength: 20}, "short", false},
		{"max_length fails over limit", Assertions{MaxLength: 5}, "this is too long", true},
		{"min_length passes over limit", Assertions{MinLength: 3}, "long enough", false},
		{"min_length fails under limit", Assertions{MinLength: 100}, "short", true},
		{"invalid regex reported as a failure", Assertions{Matches: "("}, "anything", true},
		{"empty assertions always pass", Assertions{}, "anything at all", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := Evaluate(Case{Assert: tt.assert}, Response{Text: tt.output}, "subject")
			if tt.wantErr && len(failures) == 0 {
				t.Errorf("Evaluate(%+v, %q) = no failures, want at least one", tt.assert, tt.output)
			}
			if !tt.wantErr && len(failures) != 0 {
				t.Errorf("Evaluate(%+v, %q) = %v, want none", tt.assert, tt.output, failures)
			}
		})
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
