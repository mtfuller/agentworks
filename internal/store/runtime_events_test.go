package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeEventJournalSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	runtimeStore, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	published, err := runtimeStore.AppendRuntimeEvent(ctx, RuntimeEvent{
		Topic: "run.state", EntityType: "run", EntityID: "run-1", Data: json.RawMessage(`{"state":"running"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	events, err := reopened.ListRuntimeEventsAfter(ctx, 0, 10)
	if err != nil || len(events) != 1 || events[0].Sequence != published.Sequence {
		t.Fatalf("events after reopen = %#v err=%v", events, err)
	}
}

func TestRuntimeEventJournalOrdersAndReplays(t *testing.T) {
	runtimeStore := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	first, err := runtimeStore.AppendRuntimeEvent(ctx, RuntimeEvent{
		Topic: "run.output", EntityType: "run", EntityID: "run-1",
		Data: json.RawMessage(`{"attempt_id":"attempt-1","output_sequence":1}`), CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeStore.AppendRuntimeEvent(ctx, RuntimeEvent{
		Topic: "run.output", EntityType: "run", EntityID: "run-1",
		Data: json.RawMessage(`{"attempt_id":"attempt-1","output_sequence":2}`), CreatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence <= 0 || second.Sequence != first.Sequence+1 {
		t.Fatalf("sequences = %d, %d", first.Sequence, second.Sequence)
	}
	replayed, err := runtimeStore.ListRuntimeEventsAfter(ctx, first.Sequence, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0].Sequence != second.Sequence || replayed[0].CreatedAt != second.CreatedAt {
		t.Fatalf("replayed = %#v", replayed)
	}
}

func TestRuntimeEventValidation(t *testing.T) {
	runtimeStore := openTestStore(t)
	for _, event := range []RuntimeEvent{
		{},
		{Topic: "run.state", EntityType: "run", EntityID: "run-1", Data: json.RawMessage(`{`)},
	} {
		if _, err := runtimeStore.AppendRuntimeEvent(context.Background(), event); err == nil {
			t.Fatalf("invalid event accepted: %#v", event)
		}
	}
	if _, err := runtimeStore.ListRuntimeEventsAfter(context.Background(), -1, 1); err == nil {
		t.Fatal("negative sequence accepted")
	}
}

func TestRunLifecyclePublishesStateEvents(t *testing.T) {
	runtimeStore := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	event := Event{RecordID: "event-1", Source: "manual", ExternalID: "manual-1", Type: "manual", OccurredAt: now}
	run := testRun("run-1", now)
	if _, created, err := runtimeStore.IngestAndRoute(ctx, event, run, "test"); err != nil || !created {
		t.Fatalf("create run: created=%v err=%v", created, err)
	}
	claim, found, err := runtimeStore.ClaimNext(ctx, "worker", now.Add(time.Millisecond), time.Minute)
	if err != nil || !found {
		t.Fatalf("claim: found=%v err=%v", found, err)
	}
	if err := runtimeStore.PrepareClaim(ctx, claim, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.StartAttempt(ctx, claim, 123, now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.CompleteClaim(ctx, claim, RunSucceeded, "done", nil, now.Add(4*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	events, err := runtimeStore.ListRuntimeEventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 || events[0].Topic != "event.routing" {
		t.Fatalf("events = %#v", events)
	}
	want := []RunState{RunPending, RunLeased, RunPreparing, RunRunning, RunSucceeded}
	for index, state := range want {
		var data struct {
			State RunState `json:"state"`
		}
		if err := json.Unmarshal(events[index+1].Data, &data); err != nil || data.State != state {
			t.Fatalf("event %d = %#v data=%#v err=%v", index, events[index+1], data, err)
		}
	}
}
