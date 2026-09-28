package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestExecutionPlanIsImmutableAndRoundTrips(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	_, _, err := s.CreateRun(context.Background(), Run{ID: "run-1", IdempotencyKey: "one", Agent: "builder", Workspace: "workspace", Harness: "fake", Permission: PermissionReadonly, State: RunPending, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	plan := ExecutionPlan{RunID: "run-1", PlanDigest: "digest", Provider: "host", Runtime: "/bin/fake", RuntimeVersion: "1.0", Network: "host", Plan: json.RawMessage(`{"team":"engineering"}`), CreatedAt: now}
	if err := s.SaveExecutionPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveExecutionPlan(context.Background(), plan); err != nil {
		t.Fatalf("idempotent save: %v", err)
	}
	stored, err := s.GetExecutionPlan(context.Background(), "run-1")
	if err != nil || stored.Runtime != "/bin/fake" || stored.PlanDigest != "digest" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	changed := plan
	changed.Runtime = "/bin/other"
	if err := s.SaveExecutionPlan(context.Background(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("mutation error=%v", err)
	}
}
