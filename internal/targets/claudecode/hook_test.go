package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func newTestHook(t *testing.T, events []string, command string) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindHook, "pre-commit-lint", scaffold.Options{Description: "Lint before commit."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra["events"] = events
	a.Extra["command"] = command
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, artifact.KindHook)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return reloaded
}

func TestExportHook(t *testing.T) {
	a := newTestHook(t, []string{"PreToolUse", "PostToolUse"}, "gofmt -l .")
	outDir := t.TempDir()

	dest, err := (exporter{}).Export(a, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if dest != filepath.Join(outDir, "pre-commit-lint") {
		t.Errorf("Export() dest = %q, want %s/pre-commit-lint", dest, outDir)
	}

	data, err := os.ReadFile(filepath.Join(dest, "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("reading hooks/hooks.json: %v", err)
	}
	var doc claudeHooksDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing hooks.json: %v", err)
	}
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		matchers, ok := doc.Hooks[event]
		if !ok || len(matchers) != 1 || len(matchers[0].Hooks) != 1 {
			t.Fatalf("hooks.json missing entry for %s, got: %+v", event, doc.Hooks)
		}
		action := matchers[0].Hooks[0]
		if action.Type != "command" || action.Command != "gofmt -l ." {
			t.Errorf("hooks.json[%s] action = %+v, want type=command command=\"gofmt -l .\"", event, action)
		}
	}

	if _, err := os.Stat(filepath.Join(dest, ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("expected .claude-plugin/plugin.json to exist: %v", err)
	}
}

func TestExportHookRequiresEventsAndCommand(t *testing.T) {
	tests := []struct {
		name    string
		events  []string
		command string
	}{
		{"no events", nil, "gofmt -l ."},
		{"no command", []string{"PreToolUse"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestHook(t, tt.events, tt.command)
			if _, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
				t.Fatalf("Export() with %s expected error, got nil", tt.name)
			}
		})
	}
}
