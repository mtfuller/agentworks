package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPollCommitIsAtomicAndDeduplicated(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	if err := s.SyncSource(ctx, SourceStatus{ID: "jira", Kind: "jira", ConfigRevision: "one", DefinitionEnabled: true, CreatedAt: now, UpdatedAt: now, NextPollAt: now}); err != nil {
		t.Fatal(err)
	}
	event := Event{RecordID: "event-1", Source: "jira", ExternalID: "delivery-1", Type: "jira.issue.assigned", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{}`)}
	bad := PollCommit{SourceID: "jira", Cursor: "cursor-1", Events: []Event{event}, Outcomes: []Outcome{{ID: "bad", Type: "github.pr.opened", WorkItemID: "missing", OccurredAt: now, Data: json.RawMessage(`{}`)}}, PolledAt: now, NextPollAt: now.Add(time.Minute)}
	if _, err := s.CommitPoll(ctx, bad); err == nil {
		t.Fatal("invalid batch committed")
	}
	status, _ := s.Source(ctx, "jira")
	events, _ := s.ListEvents(ctx, 10)
	if status.Cursor != "" || len(events) != 0 {
		t.Fatalf("partial commit status=%#v events=%#v", status, events)
	}
	good := PollCommit{SourceID: "jira", Cursor: "cursor-1", Events: []Event{event}, PolledAt: now, NextPollAt: now.Add(time.Minute)}
	first, err := s.CommitPoll(ctx, good)
	if err != nil || first.EventsInserted != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	good.ExpectedCursor = "cursor-1"
	second, err := s.CommitPoll(ctx, good)
	if err != nil || second.EventsInserted != 0 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	status, _ = s.Source(ctx, "jira")
	if status.Cursor != "cursor-1" || status.State != "healthy" {
		t.Fatalf("status=%#v", status)
	}
	stale := PollCommit{
		SourceID:       "jira",
		ExpectedCursor: "",
		Cursor:         "cursor-2",
		Events: []Event{{
			RecordID: "event-2", Source: "jira", ExternalID: "delivery-2",
			Type: "jira.issue.updated", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{}`),
		}},
		PolledAt: now, NextPollAt: now.Add(time.Minute),
	}
	if _, err := s.CommitPoll(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale cursor err=%v", err)
	}
	status, _ = s.Source(ctx, "jira")
	events, _ = s.ListEvents(ctx, 10)
	if status.Cursor != "cursor-1" || len(events) != 1 {
		t.Fatalf("stale poll changed durable state: status=%#v events=%#v", status, events)
	}
}

func TestSourcePauseFailureAndDueState(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	if err := s.SyncSource(ctx, SourceStatus{ID: "github", Kind: "github", ConfigRevision: "one", DefinitionEnabled: true, NextPollAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	due, err := s.DueSources(ctx, now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due=%#v err=%v", due, err)
	}
	if err := s.SetSourcePaused(ctx, "github", true, now); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueSources(ctx, now, 10)
	if len(due) != 0 {
		t.Fatalf("paused due=%#v", due)
	}
	if err := s.SetSourcePaused(ctx, "missing", true, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing pause=%v", err)
	}
	if err := s.SetSourcePaused(ctx, "github", false, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSourceFailure(ctx, "github", "safe error", now.Add(time.Minute), time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	status, err := s.Source(ctx, "github")
	if err != nil || status.State != "degraded" || status.FailureCount != 1 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	values, err := s.ListSources(ctx)
	if err != nil || len(values) != 1 {
		t.Fatalf("sources=%#v err=%v", values, err)
	}
}

func TestSourcePollAttemptsRetainSuccessFailureAndCorrelation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if err := s.SyncSource(ctx, SourceStatus{ID: "jira", Kind: "jira", ConfigRevision: "revision", DefinitionEnabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSourcePoll(ctx, SourcePollAttempt{ID: "poll-success", SourceID: "jira", CursorBefore: "before", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteSourcePoll(ctx, SourcePollAttempt{ID: "poll-success", RoutedRuns: 1, EventRecordIDs: []string{"event-2", "event-1", "event-1"}, RunIDs: []string{"run-1"}, FinishedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSourcePoll(ctx, SourcePollAttempt{ID: "poll-failure", SourceID: "jira", StartedAt: now.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	failed, err := s.FailSourcePoll(ctx, SourcePollAttempt{ID: "poll-failure", SourceID: "jira", ErrorCode: "rate_limited", ErrorMessage: "connector rate limit reached", FinishedAt: now.Add(3 * time.Second), NextPollAt: now.Add(time.Minute), RateLimitResetAt: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != "failed" || failed.ErrorCode != "rate_limited" || !failed.RateLimitResetAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("failed attempt = %#v", failed)
	}
	attempts, err := s.ListSourcePollAttempts(ctx, "jira", 10)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
	var success SourcePollAttempt
	for _, attempt := range attempts {
		if attempt.ID == "poll-success" {
			success = attempt
		}
	}
	if success.State != "succeeded" || success.RoutedRuns != 1 || !reflect.DeepEqual(success.EventRecordIDs, []string{"event-1", "event-2"}) || !reflect.DeepEqual(success.RunIDs, []string{"run-1"}) {
		t.Fatalf("success attempt = %#v", success)
	}
}
