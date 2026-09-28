package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/store"
)

func TestScheduleCatchUpIsDurableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	root := scheduleFixture(t)
	runtimeStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	events := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	if err := Sync(ctx, root, runtimeStore, now); err != nil {
		t.Fatal(err)
	}
	if err := ProcessDue(ctx, root, runtimeStore, events, now); err != nil {
		t.Fatal(err)
	}
	runs, err := runtimeStore.ListRuns(ctx, 100)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
	inbox, err := runtimeStore.ListEvents(ctx, 100)
	if err != nil || len(inbox) != 1 || inbox[0].Status != store.EventRouted {
		t.Fatalf("events=%#v err=%v", inbox, err)
	}
	// A restart sync preserves the advanced due time. Reprocessing at the same
	// clock cannot create another event or run.
	if err := Sync(ctx, root, runtimeStore, now); err != nil {
		t.Fatal(err)
	}
	if err := ProcessDue(ctx, root, runtimeStore, events, now); err != nil {
		t.Fatal(err)
	}
	runs, _ = runtimeStore.ListRuns(ctx, 100)
	inbox, _ = runtimeStore.ListEvents(ctx, 100)
	if len(runs) != 1 || len(inbox) != 1 {
		t.Fatalf("restart duplicated work: runs=%d events=%d", len(runs), len(inbox))
	}
}

func TestCatchUpPolicies(t *testing.T) {
	due := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	now := due.Add(3*time.Hour + time.Minute)
	all, next := scheduleOccurrences("all", due, now, time.Hour)
	if len(all) != 4 || !next.Equal(due.Add(4*time.Hour)) {
		t.Fatalf("all=%v next=%v", all, next)
	}
	latest, next := scheduleOccurrences("latest", due, now, time.Hour)
	if len(latest) != 1 || !latest[0].Equal(due.Add(3*time.Hour)) || !next.Equal(due.Add(4*time.Hour)) {
		t.Fatalf("latest=%v next=%v", latest, next)
	}
	skipped, next := scheduleOccurrences("skip", due, now, time.Hour)
	if len(skipped) != 0 || !next.Equal(due.Add(4*time.Hour)) {
		t.Fatalf("skip=%v next=%v", skipped, next)
	}
}

func scheduleFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{workspace, filepath.Join(root, "teams", "engineering"), filepath.Join(root, "agents", "implementer")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"agentworks.yaml":       "format: 2\nname: schedules\nteams: [engineering]\nworkspaces: [product]\n",
		"agentworks.local.yaml": "workspaces:\n  product:\n    path: " + workspace + "\n",
		filepath.Join("teams", "engineering", "team.yaml"): "name: engineering\nagents: [implementer]\ndefault_agent: implementer\n",
		filepath.Join("agents", "implementer", "AGENT.md"): "---\nname: implementer\nmax_permission: readonly\n---\nInspect.\n",
		filepath.Join("routes", "nightly", "route.yaml"):   "name: nightly\npriority: 10\nwhen:\n  source: nightly\n  type: maintenance.requested\ninvoke:\n  team: engineering\n  workspace: product\n  harness: fake\n  permission: readonly\n",
		filepath.Join("sources", "nightly", "source.yaml"): "name: nightly\nkind: schedule\nschedule:\n  every: 1h\n  start_at: 2026-09-26T09:00:00Z\n  catch_up: latest\nevent:\n  type: maintenance.requested\n  subject: product\n  data:\n    task: inspect\n",
	}
	for name, value := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
