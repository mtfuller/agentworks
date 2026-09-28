package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

func TestPruneDeletesOldUnpinnedDetailAndPreservesPinned(t *testing.T) {
	root := t.TempDir()
	runtimeStore, err := store.Open(context.Background(), filepath.Join(root, "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	logRoot := filepath.Join(root, "logs")
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	oldClaim := completedRun(t, runtimeStore, "old", now)
	pinnedClaim := completedRun(t, runtimeStore, "pinned", now.Add(time.Minute))
	if err := runtimeStore.SetRunPinned(context.Background(), pinnedClaim.Run.ID, true, now); err != nil {
		t.Fatal(err)
	}
	oldLog := worker.LogPath(logRoot, oldClaim.Run.ID, oldClaim.Attempt.ID)
	pinnedLog := worker.LogPath(logRoot, pinnedClaim.Run.ID, pinnedClaim.Attempt.ID)
	for _, path := range []string{oldLog, pinnedLog} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := Usage(logRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager := Manager{Store: runtimeStore, LogRoot: logRoot, Limit: usage - 2048}
	report, err := manager.Prune(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.PrunedRuns) != 1 || report.PrunedRuns[0] != oldClaim.Run.ID || report.OverLimit {
		t.Fatalf("report=%#v", report)
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Fatalf("old log remains: %v", err)
	}
	if _, err := os.Stat(pinnedLog); err != nil {
		t.Fatalf("pinned log removed: %v", err)
	}
	if err := manager.DeleteRunDetail(context.Background(), pinnedClaim.Run.ID); err == nil {
		t.Fatal("manual deletion accepted a pinned run")
	}
}

func completedRun(t *testing.T, runtimeStore *store.Store, id string, now time.Time) store.Claim {
	t.Helper()
	run := store.Run{ID: id, IdempotencyKey: id, Agent: "builder", Workspace: id, Harness: "fake", Permission: store.PermissionReadonly, State: store.RunPending, CreatedAt: now, UpdatedAt: now}
	if _, _, err := runtimeStore.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	claim, found, err := runtimeStore.ClaimNext(context.Background(), "worker", now, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	if err := runtimeStore.PrepareClaim(context.Background(), claim, now); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.StartAttempt(context.Background(), claim, 123, now); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(map[string]any{"log": map[string]any{"path": "secret", "bytes": 4096}})
	if err := runtimeStore.CompleteClaim(context.Background(), claim, store.RunSucceeded, "done", result, now); err != nil {
		t.Fatal(err)
	}
	return claim
}
