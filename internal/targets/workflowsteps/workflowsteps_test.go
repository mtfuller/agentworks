package workflowsteps

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

// newTestWorkflow builds a project with a researcher agent, a jira-fetch
// tool, and a workflow whose steps (given as raw YAML-shaped data, the way
// they'd come back from parsing frontmatter) reference them.
func newTestWorkflow(t *testing.T, steps []any) *artifact.Artifact {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	if _, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "x"}); err != nil {
		t.Fatalf("scaffold.New(agent) error = %v", err)
	}
	if _, err := scaffold.New(root, artifact.KindTool, "jira-fetch", scaffold.Options{Description: "x"}); err != nil {
		t.Fatalf("scaffold.New(tool) error = %v", err)
	}

	wf, err := scaffold.New(root, artifact.KindWorkflow, "software-factory", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New(workflow) error = %v", err)
	}
	wf.Extra["steps"] = steps
	if err := wf.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := artifact.Load(wf.Dir, artifact.KindWorkflow)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return reloaded
}

func TestResolveAgentAndToolSteps(t *testing.T) {
	a := newTestWorkflow(t, []any{
		map[string]any{"agent": "researcher"},
		map[string]any{"tool": "jira-fetch"},
	})

	steps, err := Resolve(a)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("Resolve() returned %d steps, want 2", len(steps))
	}
	if steps[0].Kind != artifact.KindAgent || steps[0].Name != "researcher" {
		t.Errorf("step 0 = %+v, want agent researcher", steps[0])
	}
	if steps[0].Artifact == nil || steps[0].Artifact.Name != "researcher" {
		t.Errorf("step 0 Artifact not loaded correctly: %+v", steps[0].Artifact)
	}
	if steps[1].Kind != artifact.KindTool || steps[1].Name != "jira-fetch" {
		t.Errorf("step 1 = %+v, want tool jira-fetch", steps[1])
	}
}

func TestResolveEmptySteps(t *testing.T) {
	a := newTestWorkflow(t, nil)
	steps, err := Resolve(a)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("Resolve() = %v, want empty", steps)
	}
}

func TestResolveMissingArtifact(t *testing.T) {
	a := newTestWorkflow(t, []any{map[string]any{"agent": "does-not-exist"}})
	if _, err := Resolve(a); err == nil {
		t.Fatal("Resolve() with a missing referenced agent expected error, got nil")
	}
}

func TestResolveMalformedStep(t *testing.T) {
	tests := []struct {
		name string
		step any
	}{
		{"not a mapping", "just a string"},
		{"missing agent/tool key", map[string]any{"skill": "csv-analyzer"}},
		{"empty agent name", map[string]any{"agent": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestWorkflow(t, []any{tt.step})
			if _, err := Resolve(a); err == nil {
				t.Fatalf("Resolve() with %s expected error, got nil", tt.name)
			}
		})
	}
}
