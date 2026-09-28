package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRetryTimerSurvivesReopenAndCreatesNewAttempt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	runtimeStore, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, runtimeStore, "run-retry", "workspace", PermissionReadonly, now)
	claim := mustClaimAt(t, runtimeStore, "worker-1", now.Add(time.Millisecond))
	if err := runtimeStore.PrepareClaim(ctx, claim, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.StartAttempt(ctx, claim, 123, now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	due := now.Add(time.Minute)
	result := json.RawMessage(`{"exit_code":7,"retry":true}`)
	if err := runtimeStore.ScheduleClaimRetry(ctx, claim, due, "temporary failure", result, now.Add(4*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	run, err := runtimeStore.GetRun(ctx, claim.Run.ID)
	if err != nil || run.State != RunRetryScheduled || run.Conclusion != "temporary failure" {
		t.Fatalf("scheduled run = %#v err=%v", run, err)
	}
	if err := runtimeStore.Close(); err != nil {
		t.Fatal(err)
	}

	runtimeStore, err = Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	gotDue, found, err := runtimeStore.NextRetryDue(ctx)
	if err != nil || !found || !gotDue.Equal(due) {
		t.Fatalf("NextRetryDue() = %v, %v, %v; want %v, true, nil", gotDue, found, err, due)
	}
	if activated, err := runtimeStore.ActivateDueRetries(ctx, due.Add(-time.Millisecond), 10); err != nil || activated != 0 {
		t.Fatalf("early activation = %d, %v", activated, err)
	}
	if activated, err := runtimeStore.ActivateDueRetries(ctx, due, 10); err != nil || activated != 1 {
		t.Fatalf("due activation = %d, %v", activated, err)
	}
	if _, found, err := runtimeStore.NextRetryDue(ctx); err != nil || found {
		t.Fatalf("completed timer remained pending: found=%v err=%v", found, err)
	}
	second, found, err := runtimeStore.ClaimNext(ctx, "worker-2", due.Add(time.Millisecond), time.Minute)
	if err != nil || !found || second.Attempt.Number != 2 {
		t.Fatalf("second claim = %#v found=%v err=%v", second, found, err)
	}
	attempts, err := runtimeStore.ListAttempts(ctx, claim.Run.ID)
	if err != nil || len(attempts) != 2 || attempts[0].State != RunFailed || string(attempts[0].Result) != string(result) {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
}

func TestCancelScheduledRetryCancelsItsTimer(t *testing.T) {
	runtimeStore := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, runtimeStore, "run-cancel-retry", "workspace", PermissionReadonly, now)
	claim := mustClaimAt(t, runtimeStore, "worker", now.Add(time.Millisecond))
	if err := runtimeStore.PrepareClaim(ctx, claim, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.StartAttempt(ctx, claim, 123, now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	due := now.Add(time.Minute)
	if err := runtimeStore.ScheduleClaimRetry(ctx, claim, due, "retry later", nil, now.Add(4*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	cancelled, err := runtimeStore.CancelPendingRun(ctx, claim.Run.ID, now.Add(5*time.Millisecond))
	if err != nil || !cancelled {
		t.Fatalf("CancelPendingRun() = %v, %v", cancelled, err)
	}
	if activated, err := runtimeStore.ActivateDueRetries(ctx, due, 10); err != nil || activated != 0 {
		t.Fatalf("cancelled retry activation = %d, %v", activated, err)
	}
	run, err := runtimeStore.GetRun(ctx, claim.Run.ID)
	if err != nil || run.State != RunCancelled {
		t.Fatalf("cancelled run = %#v err=%v", run, err)
	}
}

func TestRetryValidationAndConflicts(t *testing.T) {
	runtimeStore := openTestStore(t)
	ctx := context.Background()
	claim := Claim{Run: Run{ID: "missing"}, Attempt: Attempt{ID: "missing"}, Lease: Lease{Owner: "worker"}}
	if err := runtimeStore.ScheduleClaimRetry(ctx, claim, time.Time{}, "", nil, time.Now()); err == nil {
		t.Fatal("zero due time accepted")
	}
	if err := runtimeStore.ScheduleClaimRetry(ctx, claim, time.Now(), "", json.RawMessage(`{`), time.Now()); err == nil {
		t.Fatal("invalid result accepted")
	}
	if err := runtimeStore.ScheduleClaimRetry(ctx, claim, time.Now(), "", nil, time.Now()); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing claim error = %v", err)
	}
	if activated, err := runtimeStore.ActivateDueRetries(ctx, time.Now(), -1); err != nil || activated != 0 {
		t.Fatalf("empty activation = %d, %v", activated, err)
	}
}
