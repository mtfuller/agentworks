package runcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

func TestBuildIncludesSelectedAgentSkillsMemoryAndRequest(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "agents", "builder", spec.AgentFile), `---
name: builder
skills: [testing]
memory: memory/builder.md
max_permission: readwrite
---
Build carefully.`)
	write(t, filepath.Join(root, "teams", "delivery", spec.TeamFile), "name: delivery\nagents: [builder]\nmemory:\n  team: memory/team.md\n")
	write(t, filepath.Join(root, "skills", "testing", "SKILL.md"), "---\nname: testing\ndescription: Runs focused checks for changed behavior.\n---\nRun focused tests.")
	write(t, filepath.Join(root, "memory", "builder.md"), "Builder memory")
	write(t, filepath.Join(root, "memory", "team.md"), "Team memory")

	prompt, err := Build(root, worker.Request{
		Run:                store.Run{Team: "delivery", Agent: "builder", Permission: store.PermissionReadonly},
		Event:              store.Event{Data: json.RawMessage(`{"prompt":"Inspect the failure"}`)},
		TranscriptExcerpts: []string{"Prior agent said the fixture is flaky."},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"Build carefully.", "Run focused tests.", "Builder memory", "Team memory", "Inspect the failure", "Prior agent said the fixture is flaky.", `permission is "readonly"`} {
		if !strings.Contains(prompt, fragment) {
			t.Errorf("prompt does not contain %q:\n%s", fragment, prompt)
		}
	}
}

func TestBuildWithBudgetSelectsNewestHistoryAndCompactsOlderEntries(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "agents", "builder", spec.AgentFile), "---\nname: builder\n---\nBuild carefully.")
	write(t, filepath.Join(root, "teams", "delivery", spec.TeamFile), "name: delivery\nagents: [builder]\n")
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	request := worker.Request{
		Run:   store.Run{Team: "delivery", Agent: "builder", Permission: store.PermissionReadonly},
		Event: store.Event{Data: json.RawMessage(`{"prompt":"Continue the work"}`)},
		History: []store.TimelineEntry{
			{ID: "old", Kind: "run", Type: "builder", Summary: "OLD-RAW-SHOULD-NOT-FIT " + strings.Repeat("x", 1200), OccurredAt: now.Add(-time.Hour)},
			{ID: "new", Kind: "outcome", Type: "github.checks.passed", Summary: "checks passed", OccurredAt: now},
		},
	}
	first, err := BuildWithBudget(root, request, 1250)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildWithBudget(root, request, 1250)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("context selection is not deterministic")
	}
	if len(first) > 1250 {
		t.Fatalf("context length=%d", len(first))
	}
	if strings.Contains(first, "OLD-RAW-SHOULD-NOT-FIT") || !strings.Contains(first, "Older history compacted") || !strings.Contains(first, "github.checks.passed") {
		t.Fatalf("unexpected bounded context:\n%s", first)
	}
}

func TestBuildRejectsAgentOutsideTeamAndBuildsAutomatedEventPrompt(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "agents", "builder", spec.AgentFile), "---\nname: builder\n---\nInstructions")
	write(t, filepath.Join(root, "teams", "delivery", spec.TeamFile), "name: delivery\nagents: [reviewer]\n")
	request := worker.Request{Run: store.Run{Team: "delivery", Agent: "builder"}, Event: store.Event{Data: json.RawMessage(`{"prompt":"work"}`)}}
	if _, err := Build(root, request); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("Build() error = %v", err)
	}
	request.Event.Data = json.RawMessage(`{"prompt":" "}`)
	write(t, filepath.Join(root, "teams", "delivery", spec.TeamFile), "name: delivery\nagents: [builder]\n")
	request.Event.Source = "schedule"
	request.Event.Type = "nightly.check"
	request.Event.Subject = "product"
	prompt, err := Build(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "untrusted event") || !strings.Contains(prompt, "nightly.check") {
		t.Fatalf("automated prompt=%s", prompt)
	}
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
