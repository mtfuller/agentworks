package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPruneRunDetailPreservesRetainedRecordsAndHonorsPin(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	event := Event{RecordID: "event-1", Source: "manual", ExternalID: "one", Type: "request", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{"prompt":"secret detail"}`), RawData: json.RawMessage(`{"token":"secret"}`)}
	if _, _, err := s.IngestEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "run-1", IdempotencyKey: "run-1", EventRecordID: event.RecordID, Agent: "builder", Workspace: "workspace", Harness: "fake", State: RunSucceeded, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	conclusion := RunConclusion{Completed: []string{"done"}}
	conclusion.Normalize()
	if err := s.SaveRunConclusion(ctx, StoredConclusion{RunID: run.ID, Conclusion: conclusion, Structured: true, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunPinned(ctx, run.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneRunDetail(ctx, run.ID, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("pinned prune error=%v", err)
	}
	if err := s.SetRunPinned(ctx, run.ID, false, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneRunDetail(ctx, run.ID, now); err != nil {
		t.Fatal(err)
	}
	storedEvent, err := s.GetEvent(ctx, event.RecordID)
	if err != nil || string(storedEvent.Data) != `{"pruned":true}` || string(storedEvent.RawData) != `{}` {
		t.Fatalf("event=%#v err=%v", storedEvent, err)
	}
	storedConclusion, err := s.GetRunConclusion(ctx, run.ID)
	if err != nil || len(storedConclusion.Conclusion.Completed) != 1 {
		t.Fatalf("conclusion=%#v err=%v", storedConclusion, err)
	}
}

func TestExecutionPlanValidation(t *testing.T) {
	s := openTestStore(t)
	if err := s.SaveExecutionPlan(context.Background(), ExecutionPlan{}); err == nil {
		t.Fatal("invalid plan accepted")
	}
}
