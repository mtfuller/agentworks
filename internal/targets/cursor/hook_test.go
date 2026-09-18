package cursor

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
	a := newTestHook(t, []string{"beforeShellExecution", "afterFileEdit"}, "gofmt -l .")
	outDir := t.TempDir()

	dest, err := exportHook(a, outDir)
	if err != nil {
		t.Fatalf("exportHook() error = %v", err)
	}
	wantPath := filepath.Join(outDir, ".cursor", "hooks.json")
	if dest != wantPath {
		t.Errorf("exportHook() dest = %q, want %q", dest, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	var doc hooksDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing hooks.json: %v", err)
	}
	if doc.Version != 1 {
		t.Errorf("Version = %d, want 1", doc.Version)
	}
	for _, event := range []string{"beforeShellExecution", "afterFileEdit"} {
		actions, ok := doc.Hooks[event]
		if !ok || len(actions) != 1 {
			t.Fatalf("hooks.json missing entry for %s, got: %+v", event, doc.Hooks)
		}
		if actions[0].Type != "command" || actions[0].Command != "gofmt -l ." {
			t.Errorf("hooks.json[%s] = %+v, want type=command command=\"gofmt -l .\"", event, actions[0])
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
		{"no command", []string{"preToolUse"}, ""},
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
	first := newTestHook(t, []string{"beforeShellExecution"}, "lint.sh")
	second := newTestHook(t, []string{"beforeShellExecution", "afterFileEdit"}, "fmt.sh")
	for _, h := range []*artifact.Artifact{first, second, first} { // first again: must not duplicate
		if _, err := exportHook(h, outDir); err != nil {
			t.Fatalf("exportHook() error = %v", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(outDir, ".cursor", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc hooksDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if got := len(doc.Hooks["beforeShellExecution"]); got != 2 {
		t.Errorf("beforeShellExecution has %d actions, want 2 (lint + fmt, no duplicate): %s", got, data)
	}
	if got := len(doc.Hooks["afterFileEdit"]); got != 1 {
		t.Errorf("afterFileEdit has %d actions, want 1: %s", got, data)
	}
}
