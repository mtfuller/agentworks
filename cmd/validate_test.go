package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func newValidateTestArtifact(t *testing.T, kind artifact.Kind, extra map[string]any) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	a, err := scaffold.New(root, kind, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	for k, v := range extra {
		a.Extra[k] = v
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(a.Dir, kind)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return reloaded
}

func TestValidateKindSpecificHook(t *testing.T) {
	tests := []struct {
		name    string
		events  []string
		command string
		wantErr bool
	}{
		{"fresh scaffold, both empty", nil, "", false},
		{"both set", []string{"PreToolUse"}, "gofmt -l .", false},
		{"events without command", []string{"PreToolUse"}, "", true},
		{"command without events", nil, "gofmt -l .", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newValidateTestArtifact(t, artifact.KindHook, map[string]any{
				"events":  tt.events,
				"command": tt.command,
			})
			err := validateKindSpecific(a)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateKindSpecific() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateKindSpecificTool(t *testing.T) {
	tests := []struct {
		name    string
		auth    []string
		command string
		wantErr bool
	}{
		{"fresh scaffold, both empty", nil, "", false},
		{"command only, no auth needed", nil, "python3 src/main.py", false},
		{"auth without command", []string{"API_TOKEN"}, "", true},
		{"auth with command", []string{"API_TOKEN"}, "python3 src/main.py", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newValidateTestArtifact(t, artifact.KindTool, map[string]any{
				"auth":    tt.auth,
				"command": tt.command,
			})
			err := validateKindSpecific(a)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateKindSpecific() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateKindSpecificWorkflowMissingStep(t *testing.T) {
	a := newValidateTestArtifact(t, artifact.KindWorkflow, map[string]any{
		"steps": []any{map[string]any{"agent": "does-not-exist"}},
	})
	if err := validateKindSpecific(a); err == nil {
		t.Fatal("validateKindSpecific() with a missing referenced agent expected error, got nil")
	}
}

func TestValidateKindSpecificSkillAndAgentAreNoOps(t *testing.T) {
	for _, kind := range []artifact.Kind{artifact.KindSkill, artifact.KindAgent} {
		a := newValidateTestArtifact(t, kind, nil)
		if err := validateKindSpecific(a); err != nil {
			t.Errorf("validateKindSpecific(%s) error = %v, want nil", kind, err)
		}
	}
}

func TestValidateKindSpecificAgent(t *testing.T) {
	tests := []struct {
		name    string
		tools   []string
		model   string
		wantErr bool
	}{
		{"fresh scaffold, both unset", nil, "", false},
		{"recognized tools and model", []string{"read-files", "web-search"}, "balanced", false},
		{"unknown tool", []string{"delete-everything"}, "", true},
		{"unknown model", nil, "gpt-5", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newValidateTestArtifact(t, artifact.KindAgent, map[string]any{
				"tools": tt.tools,
				"model": tt.model,
			})
			err := validateKindSpecific(a)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateKindSpecific() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateKindSpecificEvalsDir(t *testing.T) {
	// A freshly scaffolded agent already has a valid evals/example.yaml
	// (see internal/scaffold), so this must still pass as-is.
	a := newValidateTestArtifact(t, artifact.KindAgent, nil)
	if err := validateKindSpecific(a); err != nil {
		t.Fatalf("validateKindSpecific() with the scaffolded evals/ dir error = %v, want nil", err)
	}

	if err := os.WriteFile(filepath.Join(a.Dir, "evals", "broken.yaml"), []byte("cases: [not valid :::"), 0o644); err != nil {
		t.Fatalf("writing broken eval file: %v", err)
	}
	if err := validateKindSpecific(a); err == nil {
		t.Fatal("validateKindSpecific() with a malformed evals/ file expected error, got nil")
	}
}
