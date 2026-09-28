package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestEventIngestionDeduplicatesBySourceAndExternalID(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	event := Event{
		RecordID: "event-1", Source: "jira", ExternalID: "ABC-123:17",
		Type: "issue.assigned", Subject: "ABC-123", OccurredAt: now,
		Data: json.RawMessage(`{"assignee":"me"}`),
	}
	stored, created, err := store.IngestEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if !created || stored.RecordID != "event-1" || stored.Status != EventIngested {
		t.Fatalf("first ingest = %#v created=%v", stored, created)
	}
	duplicate := event
	duplicate.RecordID = "different-local-id"
	duplicate.Data = json.RawMessage(`{"assignee":"someone-else"}`)
	stored, created, err = store.IngestEvent(ctx, duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if created || stored.RecordID != "event-1" || string(stored.Data) != `{"assignee":"me"}` {
		t.Fatalf("duplicate ingest = %#v created=%v", stored, created)
	}
}

func TestRouteEventIsAtomicAndIdempotent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	_, _, err := store.IngestEvent(ctx, Event{
		RecordID: "event-1", Source: "manual", ExternalID: "request-1",
		Type: "agent.requested", OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := testRun("run-1", now)
	stored, created, err := store.RouteEvent(ctx, "event-1", run, "route:builder")
	if err != nil {
		t.Fatal(err)
	}
	if !created || stored.EventRecordID != "event-1" {
		t.Fatalf("routed run = %#v created=%v", stored, created)
	}
	stored, created, err = store.RouteEvent(ctx, "event-1", run, "route:builder")
	if err != nil {
		t.Fatal(err)
	}
	if created || stored.ID != "run-1" {
		t.Fatalf("duplicate route = %#v created=%v", stored, created)
	}
	conflict := testRun("run-2", now)
	if _, _, err := store.RouteEvent(ctx, "event-1", conflict, "route:other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting route error = %v, want ErrConflict", err)
	}
	var status EventStatus
	var routedRunID string
	if err := store.db.QueryRow(`SELECT status, routed_run_id FROM events WHERE record_id = 'event-1'`).Scan(&status, &routedRunID); err != nil {
		t.Fatal(err)
	}
	if status != EventRouted || routedRunID != "run-1" {
		t.Fatalf("event status=%s run=%s", status, routedRunID)
	}
}

func TestIngestAndRouteIsAtomicAndIdempotent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	event := Event{
		RecordID: "event-1", Source: "manual", ExternalID: "request-1",
		Type: "agentworks.manual-run.requested", OccurredAt: now,
		Data: json.RawMessage(`{"prompt":"ship it"}`),
	}
	run := testRun("run-1", now)
	run.IdempotencyKey = "manual:request-1"
	stored, created, err := store.IngestAndRoute(ctx, event, run, "manual request")
	if err != nil {
		t.Fatal(err)
	}
	if !created || stored.EventRecordID != "event-1" {
		t.Fatalf("stored = %#v created=%v", stored, created)
	}

	duplicateEvent := event
	duplicateEvent.RecordID = "event-2"
	duplicateRun := run
	duplicateRun.ID = "run-2"
	stored, created, err = store.IngestAndRoute(ctx, duplicateEvent, duplicateRun, "manual request")
	if err != nil {
		t.Fatal(err)
	}
	if created || stored.ID != "run-1" || stored.EventRecordID != "event-1" {
		t.Fatalf("duplicate = %#v created=%v", stored, created)
	}

	conflicting := duplicateRun
	conflicting.Agent = "other"
	if _, _, err := store.IngestAndRoute(ctx, duplicateEvent, conflicting, "manual request"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error = %v, want ErrConflict", err)
	}
	var eventCount, runCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || runCount != 1 {
		t.Fatalf("event count=%d run count=%d, want 1/1", eventCount, runCount)
	}
}

func TestIngestAndRouteRollsBackInvalidRun(t *testing.T) {
	store := openTestStore(t)
	now := time.Now().UTC()
	event := Event{
		RecordID: "event-1", Source: "manual", ExternalID: "request-1",
		Type: "agentworks.manual-run.requested", OccurredAt: now,
	}
	if _, _, err := store.IngestAndRoute(context.Background(), event, Run{}, "manual request"); err == nil {
		t.Fatal("invalid run accepted")
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("event count = %d, want rollback", count)
	}
}

func TestRouteMissingEventDoesNotCreateRun(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, _, err := store.RouteEvent(ctx, "missing", testRun("run-1", time.Now().UTC()), "route"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	runs, err := store.ListRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %d, want 0", len(runs))
	}
}

func TestConcurrentRoutesSelectOnlyOneRun(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, _, err := store.IngestEvent(ctx, Event{
		RecordID: "event-1", Source: "jira", ExternalID: "ABC-123:19",
		Type: "issue.changed", OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	const candidates = 12
	var wg sync.WaitGroup
	results := make(chan error, candidates)
	for i := 0; i < candidates; i++ {
		wg.Add(1)
		go func(candidate int) {
			defer wg.Done()
			run := testRun(fmt.Sprintf("run-%d", candidate), now)
			_, _, err := store.RouteEvent(ctx, "event-1", run, fmt.Sprintf("route-%d", candidate))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrConflict):
			conflicted++
		default:
			t.Fatalf("unexpected route error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != candidates-1 {
		t.Fatalf("succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	runs, err := store.ListRuns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runs))
	}
}

func TestRunIdempotencyConflict(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	run := testRun("run-1", time.Now().UTC())
	if _, _, err := store.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	other := run
	other.ID = "run-2"
	other.Agent = "reviewer"
	if _, _, err := store.CreateRun(ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
}

func TestRunTransitionsAreOptimistic(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	run := testRun("run-1", time.Now().UTC())
	if _, _, err := store.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := store.TransitionRun(ctx, run.ID, RunPending, RunLeased, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.TransitionRun(ctx, run.ID, RunPending, RunCancelled, time.Now().UTC()); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale transition error = %v, want ErrConflict", err)
	}
	if err := store.TransitionRun(ctx, run.ID, RunLeased, RunSucceeded, time.Now().UTC()); err == nil {
		t.Fatal("invalid transition succeeded")
	}
	if err := store.TransitionRun(ctx, "missing", RunPending, RunLeased, time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing transition error = %v, want ErrNotFound", err)
	}
}

func TestMarkEventUnrouted(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, _, err := store.IngestEvent(ctx, Event{
		RecordID: "event-1", Source: "github", ExternalID: "comment-1",
		Type: "pr.comment", OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkEventUnrouted(ctx, "event-1", "highest priority tied", now); err != nil {
		t.Fatal(err)
	}
	event, err := store.eventBySourceID(ctx, "github", "comment-1")
	if err != nil {
		t.Fatal(err)
	}
	if event.Status != EventUnrouted || event.RoutingReason != "highest priority tied" {
		t.Fatalf("event = %#v", event)
	}
	if err := store.MarkEventUnrouted(ctx, "missing", "no route", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error = %v, want ErrNotFound", err)
	}
}

func TestRepositoryInputValidation(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, _, err := store.IngestEvent(ctx, Event{}); err == nil {
		t.Fatal("empty event accepted")
	}
	if _, _, err := store.IngestEvent(ctx, Event{
		RecordID: "e", Source: "s", ExternalID: "x", Type: "t",
		OccurredAt: time.Now(), Data: json.RawMessage(`{`),
	}); err == nil {
		t.Fatal("invalid event JSON accepted")
	}
	if _, _, err := store.CreateRun(ctx, Run{}); err == nil {
		t.Fatal("empty run accepted")
	}
}
