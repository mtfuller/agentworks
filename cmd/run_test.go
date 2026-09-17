package cmd

import (
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// TestRunRejectsNonToolKind and TestRunRejectsMissingCommand exercise
// runCmd's error paths directly (constructing the artifact the same way
// loadArtifactAtPath would return it) rather than executing the built
// binary, since a successful "run" launches a full-screen Bubble Tea
// program -- not something a unit test drives. The non-interactive-
// terminal guard and a successful connect are covered by
// tests/integration_test.go and internal/mcpclient/internal/inspector's
// own tests, respectively.
func TestRunRejectsNonToolKind(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindSkill, nil)

	if a.Kind == artifact.KindTool {
		t.Fatal("test setup error: expected a non-tool artifact")
	}
	// Mirrors runCmd.RunE's own check.
	err := checkRunnable(a)
	if err == nil {
		t.Fatal("checkRunnable() on a skill, want an error")
	}
	if !strings.Contains(err.Error(), "not a tool") {
		t.Errorf("checkRunnable() error = %v, want it to mention \"not a tool\"", err)
	}
}

func TestRunRejectsMissingCommand(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindTool, map[string]any{
		"command": "",
	})

	err := checkRunnable(a)
	if err == nil {
		t.Fatal("checkRunnable() on a tool with no command, want an error")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Errorf("checkRunnable() error = %v, want it to mention the missing command", err)
	}
}

func TestRunAcceptsToolWithCommand(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindTool, map[string]any{
		"command": "sh -c true",
	})

	if err := checkRunnable(a); err != nil {
		t.Errorf("checkRunnable() = %v, want nil", err)
	}
}
