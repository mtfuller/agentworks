package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenericOutcomeCorrelatesThroughRunWorkItem(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	item, err := s.UpsertWorkItem(ctx, WorkItem{ID: "work-1", Kind: "ticket", ExternalKey: "ABC-1", Data: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	event := Event{RecordID: "event-1", Source: "fixture", ExternalID: "one", Type: "ticket.assigned", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{}`), WorkItemID: item.ID}
	run := Run{ID: "run-1", IdempotencyKey: "one", Agent: "builder", Workspace: "workspace", Harness: "fake", Permission: PermissionReadonly, State: RunPending, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.IngestAndRoute(ctx, event, run, "test"); err != nil {
		t.Fatal(err)
	}
	outcome, err := s.RecordOutcome(ctx, Outcome{ID: "out-1", Type: "github.pr.opened", RunID: run.ID, OccurredAt: now.Add(time.Second), Data: json.RawMessage(`{"number":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.WorkItemID != item.ID {
		t.Fatalf("work item=%q", outcome.WorkItemID)
	}
	timeline, err := s.WorkItemTimeline(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 {
		t.Fatalf("timeline=%#v", timeline)
	}
	if timeline[0].Kind != "event" || timeline[1].Kind != "run" || timeline[2].Kind != "outcome" {
		t.Fatalf("timeline order=%#v", timeline)
	}
}

func TestStructuredConclusionIsIdempotentButImmutable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, _, err := s.CreateRun(ctx, Run{ID: "run-1", IdempotencyKey: "one", Agent: "builder", Workspace: "workspace", Harness: "fake", State: RunSucceeded, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	value := StoredConclusion{RunID: "run-1", Conclusion: RunConclusion{Completed: []string{"done"}}, Structured: true, CreatedAt: now}
	if err := s.SaveRunConclusion(ctx, value); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRunConclusion(ctx, value); err != nil {
		t.Fatal(err)
	}
	value.Conclusion.Completed = []string{"changed"}
	if err := s.SaveRunConclusion(ctx, value); err == nil {
		t.Fatal("mutable conclusion was accepted")
	}
	stored, err := s.GetRunConclusion(ctx, "run-1")
	if err != nil || stored.Conclusion.Completed[0] != "done" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestSecondRunReceivesPriorConclusionAndOutcome(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	item, err := s.UpsertWorkItem(ctx, WorkItem{ID: "work-1", Kind: "ticket", ExternalKey: "ABC-1", Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	firstEvent := Event{RecordID: "event-1", Source: "fixture", ExternalID: "one", Type: "assigned", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{}`), WorkItemID: item.ID}
	if _, _, err := s.IngestEvent(ctx, firstEvent); err != nil {
		t.Fatal(err)
	}
	first := Run{ID: "run-1", IdempotencyKey: "one", EventRecordID: firstEvent.RecordID, Agent: "builder", Workspace: "workspace", Harness: "fake", State: RunSucceeded, CreatedAt: now, UpdatedAt: now}
	if _, _, err := s.CreateRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRunConclusion(ctx, StoredConclusion{RunID: first.ID, Conclusion: RunConclusion{Completed: []string{"opened PR"}}, Structured: true, PrivateMemory: "await checks", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordOutcome(ctx, Outcome{ID: "out-1", Type: "github.pr.opened", RunID: first.ID, OccurredAt: now.Add(time.Second), Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	secondEvent := Event{RecordID: "event-2", Source: "fixture", ExternalID: "two", Type: "commented", OccurredAt: now.Add(2 * time.Second), ReceivedAt: now.Add(2 * time.Second), Data: json.RawMessage(`{}`), WorkItemID: item.ID}
	if _, _, err := s.IngestEvent(ctx, secondEvent); err != nil {
		t.Fatal(err)
	}
	second := Run{ID: "run-2", IdempotencyKey: "two", EventRecordID: secondEvent.RecordID, Agent: "builder", Workspace: "workspace", Harness: "fake", State: RunPending, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}
	if _, _, err := s.CreateRun(ctx, second); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ContextEntries(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
	encoded, _ := json.Marshal(entries)
	if string(encoded) == "" || !containsJSON(string(encoded), "opened PR", "github.pr.opened", "await checks") {
		t.Fatalf("entries=%s", encoded)
	}
}

func TestMemoryOutcomeAndMonitorRepositories(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, _, err := s.CreateRun(ctx, Run{ID: "run-1", IdempotencyKey: "one", Agent: "builder", Workspace: "workspace", Harness: "fake", State: RunSucceeded, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	p := MemoryProposal{ID: "proposal-1", RunID: "run-1", Scope: "agent", TargetPath: "memory/agent.md", BaseSHA256: "abc", BeforeMarkdown: "old", AfterMarkdown: "new", Diff: "-old\n+new", CreatedAt: now}
	if err := s.CreateMemoryProposal(ctx, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetMemoryProposal(ctx, p.ID)
	if err != nil || loaded.State != MemoryProposalPending {
		t.Fatalf("proposal=%#v err=%v", loaded, err)
	}
	listed, err := s.ListMemoryProposals(ctx, "run-1")
	if err != nil || len(listed) != 1 {
		t.Fatalf("proposals=%#v err=%v", listed, err)
	}
	if err := s.DecideMemoryProposal(ctx, p.ID, MemoryProposalRejected, json.RawMessage(`{"reason":"test"}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideMemoryProposal(ctx, p.ID, MemoryProposalApproved, nil, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("second decision=%v", err)
	}
	if _, err := s.RecordOutcome(ctx, Outcome{ID: "out-1", Type: "github.pr.opened", OccurredAt: now, Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	outcomes, err := s.ListOutcomes(ctx, "", 10)
	if err != nil || len(outcomes) != 1 {
		t.Fatalf("outcomes=%#v err=%v", outcomes, err)
	}
	state := MonitorState{MonitorID: "test", Revision: "v1", State: "healthy", Window: json.RawMessage(`{}`), Details: json.RawMessage(`{"ok":true}`), EvaluatedAt: now}
	if err := s.PutMonitorState(ctx, state); err != nil {
		t.Fatal(err)
	}
	states, err := s.ListMonitorStates(ctx)
	if err != nil || len(states) != 1 || states[0].State != "healthy" {
		t.Fatalf("states=%#v err=%v", states, err)
	}
	metrics, err := s.ReadRuntimeMetrics(ctx, now)
	if err != nil || metrics.PendingRuns != 0 {
		t.Fatalf("metrics=%#v err=%v", metrics, err)
	}
}

func containsJSON(value string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(value, want) {
			return false
		}
	}
	return true
}
