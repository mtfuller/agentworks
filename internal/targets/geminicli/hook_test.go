package geminicli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
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
	a := newTestHook(t, []string{"BeforeTool", "AfterTool"}, "gofmt -l .")
	outDir := t.TempDir()

	dest, err := exportHook(a, outDir)
	if err != nil {
		t.Fatalf("exportHook() error = %v", err)
	}
	wantPath := filepath.Join(outDir, ".gemini", "settings.json")
	if dest != wantPath {
		t.Errorf("exportHook() dest = %q, want %q", dest, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	var frag settingsFragment
	if err := json.Unmarshal(data, &frag); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	for _, event := range []string{"BeforeTool", "AfterTool"} {
		groups, ok := frag.Hooks[event]
		if !ok || len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("settings.json missing entry for %s, got: %+v", event, frag.Hooks)
		}
		action := groups[0].Hooks[0]
		if action.Type != "command" || action.Command != "gofmt -l ." {
			t.Errorf("settings.json[%s] = %+v, want type=command command=\"gofmt -l .\"", event, action)
		}
	}
}

func TestExportHookRequiresEventsAndCommand(t *testing.T) {
	tests := []struct {
		name    string
		events  []string
		command string
	}{
		{"no events", nil, "gofmt -l ."},
		{"no command", []string{"BeforeTool"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestHook(t, tt.events, tt.command)
			if _, err := exportHook(a, t.TempDir()); err == nil {
				t.Fatalf("exportHook() with %s expected error, got nil", tt.name)
			}
		})
	}
}

func TestExportHookMergesIntoOneFile(t *testing.T) {
	outDir := t.TempDir()
	mk := func(name, command string, events ...string) *artifact.Artifact {
		a, err := scaffold.New(t.TempDir(), artifact.KindHook, name, scaffold.Options{Description: "x"})
		if err != nil {
			t.Fatalf("scaffold.New() error = %v", err)
		}
		list := make([]any, len(events))
		for i, e := range events {
			list[i] = e
		}
		a.Extra["events"] = list
		a.Extra["command"] = command
		return a
	}
	first := mk("one", "lint.sh", "BeforeTool")
	second := mk("two", "fmt.sh", "BeforeTool", "SessionStart")
	for _, h := range []*artifact.Artifact{first, second, first} {
		if _, err := exportHook(h, outDir); err != nil {
			t.Fatalf("exportHook() error = %v", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(outDir, ".gemini", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var frag settingsFragment
	if err := json.Unmarshal(data, &frag); err != nil {
		t.Fatal(err)
	}
	if got := len(frag.Hooks["BeforeTool"]); got != 2 {
		t.Errorf("BeforeTool has %d groups, want 2 (no duplicate of the re-exported hook): %s", got, data)
	}
	if got := len(frag.Hooks["SessionStart"]); got != 1 {
		t.Errorf("SessionStart has %d groups, want 1: %s", got, data)
	}
}

func TestExportHookHandlersConvertTimeoutToMilliseconds(t *testing.T) {
	a, err := scaffold.New(t.TempDir(), artifact.KindHook, "guard", scaffold.Options{Description: "Guard file access."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	a.Extra = map[string]any{"handlers": []map[string]any{
		{"event": "BeforeTool", "matcher": "read_file", "command": "check.js", "timeout": 5},
		{"event": "SessionStart", "command": "hello.js"},
	}}
	outDir := t.TempDir()
	if _, err := exportHook(a, outDir); err != nil {
		t.Fatalf("exportHook() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, ".gemini", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var frag settingsFragment
	if err := json.Unmarshal(data, &frag); err != nil {
		t.Fatal(err)
	}
	before := frag.Hooks["BeforeTool"]
	if len(before) != 1 || before[0].Matcher != "read_file" || before[0].Hooks[0].Timeout != 5000 {
		t.Errorf("BeforeTool = %+v, want matcher read_file and a 5000 ms timeout (Gemini CLI's unit is milliseconds)", before)
	}
	start := frag.Hooks["SessionStart"]
	if len(start) != 1 || start[0].Hooks[0].Timeout != 0 {
		t.Errorf("SessionStart = %+v, want no timeout so Gemini's own default applies", start)
	}
}
