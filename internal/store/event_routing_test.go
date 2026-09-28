package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEventInboxLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	event := Event{RecordID: "event-a", Source: "manual", ExternalID: "delivery-a", Type: "work.requested", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{"value":1}`), RawData: json.RawMessage(`{"value":1}`)}
	stored, created, err := s.IngestEvent(ctx, event)
	if err != nil || !created || stored.Status != EventIngested {
		t.Fatalf("ingest=%#v created=%v err=%v", stored, created, err)
	}
	duplicate, created, err := s.IngestEvent(ctx, event)
	if err != nil || created || duplicate.RecordID != event.RecordID {
		t.Fatalf("duplicate=%#v created=%v err=%v", duplicate, created, err)
	}
	loaded, err := s.GetEvent(ctx, event.RecordID)
	if err != nil || loaded.ExternalID != event.ExternalID {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	listed, err := s.ListEvents(ctx, 9999)
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	if err := s.MarkEventUnrouted(ctx, event.RecordID, "no match", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenEvent(ctx, event.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenEvent(ctx, event.RecordID); !errors.Is(err, ErrConflict) {
		t.Fatalf("second reopen error=%v", err)
	}
	if _, err := s.GetEvent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get error=%v", err)
	}
	if err := s.ReopenEvent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing reopen error=%v", err)
	}
	if err := s.MarkEventUnrouted(ctx, "", "reason", now); err == nil {
		t.Fatal("empty event ID accepted")
	}
}

func TestSubscriptionsAndScheduleTimers(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	enabled, err := s.SubscriptionEnabled(ctx, "route-a", true)
	if err != nil || !enabled {
		t.Fatalf("default enabled=%v err=%v", enabled, err)
	}
	if err := s.SyncSubscription(ctx, Subscription{ID: "route-a", Revision: "one", Enabled: true, Priority: 10, LoadedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSubscriptionEnabled(ctx, "route-a", false, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSubscriptionEnabled(ctx, "missing", false, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing subscription error=%v", err)
	}
	subscriptions, err := s.ListSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0].Enabled {
		t.Fatalf("subscriptions=%#v err=%v", subscriptions, err)
	}

	due := now.Add(time.Hour)
	if err := s.UpsertScheduleTimer(ctx, "schedule:a", due, json.RawMessage(`{"source":"a"}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertScheduleTimer(ctx, "bad", due, json.RawMessage(`{`), now); err == nil {
		t.Fatal("invalid timer payload accepted")
	}
	next, found, err := s.NextScheduleDue(ctx)
	if err != nil || !found || !next.Equal(due) {
		t.Fatalf("next=%v found=%v err=%v", next, found, err)
	}
	values, err := s.DueScheduleTimers(ctx, due, 0)
	if err != nil || len(values) != 1 || values[0].ID != "schedule:a" {
		t.Fatalf("due=%#v err=%v", values, err)
	}
	advanced := due.Add(time.Hour)
	if err := s.AdvanceScheduleTimer(ctx, "schedule:a", due, advanced, now); err != nil {
		t.Fatal(err)
	}
	if err := s.AdvanceScheduleTimer(ctx, "schedule:a", due, advanced, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale advance error=%v", err)
	}
	if err := s.CancelScheduleTimersExcept(ctx, map[string]bool{}, now); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.NextScheduleDue(ctx); err != nil || found {
		t.Fatalf("cancelled timer found=%v err=%v", found, err)
	}
	// Re-syncing a disabled source reactivates its timer without losing the
	// already-advanced due time.
	if err := s.UpsertScheduleTimer(ctx, "schedule:a", due, nil, now); err != nil {
		t.Fatal(err)
	}
	next, found, err = s.NextScheduleDue(ctx)
	if err != nil || !found || !next.Equal(advanced) {
		t.Fatalf("reactivated next=%v found=%v err=%v", next, found, err)
	}
}
