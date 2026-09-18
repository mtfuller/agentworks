package cmd

import (
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestFirstShellWord(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"python3 src/main.py", "python3"},
		{"  node   index.js  ", "node"},
		{"sh -c 'echo hi'", "sh"},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		if got := firstShellWord(tt.command); got != tt.want {
			t.Errorf("firstShellWord(%q) = %q, want %q", tt.command, got, tt.want)
		}
	}
}

func TestEnvHasValue(t *testing.T) {
	env := []string{"FOO=bar", "EMPTY=", "PATH=/usr/bin"}
	tests := []struct {
		name string
		want bool
	}{
		{"FOO", true},
		{"EMPTY", false},
		{"MISSING", false},
		{"PATH", true},
	}
	for _, tt := range tests {
		if got := envHasValue(env, tt.name); got != tt.want {
			t.Errorf("envHasValue(env, %q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDoctorChecksMissingInterpreter(t *testing.T) {
	// entrypoint is blanked out: the bare tool scaffold declares one
	// ("src/main") without creating a backing file, which would otherwise
	// add an unrelated second issue to these command-focused assertions.
	a := newValidateTestArtifact(t, artifact.KindMCP, map[string]any{
		"entrypoint": "",
		"command":    "definitely-not-a-real-binary-xyz --flag",
	})

	issues := doctorChecks(a, nil)
	if len(issues) != 1 {
		t.Fatalf("doctorChecks() = %v, want exactly 1 issue", issues)
	}
	if !issues[0].fatal {
		t.Errorf("missing interpreter should be fatal, got %+v", issues[0])
	}
}

func TestDoctorChecksInterpreterOnPath(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindMCP, map[string]any{
		"entrypoint": "",
		"command":    "sh -c true",
	})

	if issues := doctorChecks(a, nil); len(issues) != 0 {
		t.Errorf("doctorChecks() = %v, want no issues ('sh' is always on PATH)", issues)
	}
}

func TestDoctorChecksMissingEntrypoint(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindSkill, map[string]any{
		"entrypoint": "scripts/does-not-exist.py",
	})

	issues := doctorChecks(a, nil)
	if len(issues) != 1 || !issues[0].fatal {
		t.Fatalf("doctorChecks() = %v, want exactly 1 fatal issue", issues)
	}
}

func TestDoctorChecksMissingAuthIsWarningNotFatal(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindMCP, map[string]any{
		"entrypoint": "",
		"command":    "sh -c true",
		"auth":       []string{"SOME_TOKEN"},
	})

	issues := doctorChecks(a, nil)
	if len(issues) != 1 {
		t.Fatalf("doctorChecks() = %v, want exactly 1 issue", issues)
	}
	if issues[0].fatal {
		t.Errorf("missing auth variable should be a warning, not fatal: %+v", issues[0])
	}
}

func TestDoctorChecksAuthPresentInEnv(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindMCP, map[string]any{
		"entrypoint": "",
		"command":    "sh -c true",
		"auth":       []string{"SOME_TOKEN"},
	})

	if issues := doctorChecks(a, []string{"SOME_TOKEN=secret"}); len(issues) != 0 {
		t.Errorf("doctorChecks() = %v, want no issues (auth var is set)", issues)
	}
}

func TestDoctorChecksAuthIgnoredForNonTools(t *testing.T) {
	// "auth:" only means something for a tool -- see validateKindSpecific's
	// own tool-only handling of it. doctorChecks should follow the same
	// convention rather than warning about it on every kind.
	a := newValidateTestArtifact(t, artifact.KindSkill, map[string]any{
		"auth": []string{"SOME_TOKEN"},
	})

	if issues := doctorChecks(a, nil); len(issues) != 0 {
		t.Errorf("doctorChecks() = %v, want no issues (auth is ignored outside tool)", issues)
	}
}

func TestDoctorFlagsScaffoldPlaceholder(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindMCP, map[string]any{
		"command": "sh",
		"args":    []any{"-y", "REPLACE-WITH-PACKAGE-NAME"},
	})
	var found bool
	for _, issue := range doctorChecks(a, nil) {
		if issue.fatal && strings.Contains(issue.message, "placeholder") {
			found = true
		}
	}
	if !found {
		t.Error("doctorChecks() should fail an mcp artifact still holding a REPLACE-WITH- placeholder")
	}
	if smokeEligible(a) {
		t.Error("smokeEligible() should be false while a placeholder remains, so `agentworks test` doesn't try to run it")
	}
}
