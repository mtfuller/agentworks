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

func TestConcurrentWorkersClaimOneRunOnce(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	if _, _, err := store.CreateRun(ctx, testRun("run-1", now)); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	claims := make(chan Claim, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			claim, found, err := store.ClaimNext(ctx, fmt.Sprintf("worker-%d", index), now, time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if found {
				claims <- claim
			}
		}(i)
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var got []Claim
	for claim := range claims {
		got = append(got, claim)
	}
	if len(got) != 1 || got[0].Run.ID != "run-1" || got[0].Attempt.Number != 1 {
		t.Fatalf("claims = %#v", got)
	}
}

func TestClaimNextForHarnessesLeavesUnsupportedRunsPending(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	unsupported := testRun("claude", now)
	unsupported.Harness = "claude-code"
	if _, _, err := store.CreateRun(context.Background(), unsupported); err != nil {
		t.Fatal(err)
	}
	supported := testRun("fake", now.Add(time.Millisecond))
	supported.Harness = "fake"
	if _, _, err := store.CreateRun(context.Background(), supported); err != nil {
		t.Fatal(err)
	}
	claim, found, err := store.ClaimNextForHarnesses(context.Background(), "worker", []string{"fake"}, now, time.Minute)
	if err != nil || !found || claim.Run.ID != "fake" {
		t.Fatalf("claim = %#v found=%v err=%v", claim, found, err)
	}
	run, err := store.GetRun(context.Background(), "claude")
	if err != nil || run.State != RunPending {
		t.Fatalf("unsupported run = %#v err=%v", run, err)
	}
}

func TestCancelPendingRunAndListAttempts(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, store, "pending", "one", PermissionReadonly, now)
	cancelled, err := store.CancelPendingRun(context.Background(), "pending", now.Add(time.Second))
	if err != nil || !cancelled {
		t.Fatalf("cancelled=%v err=%v", cancelled, err)
	}
	run, _ := store.GetRun(context.Background(), "pending")
	if run.State != RunCancelled {
		t.Fatalf("state = %s", run.State)
	}
	if attempts, err := store.ListAttempts(context.Background(), "pending"); err != nil || len(attempts) != 0 {
		t.Fatalf("attempts=%#v err=%v", attempts, err)
	}
}

func TestReadonlyClaimsShareWorkspaceAndBlockWriter(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, store, "read-1", "shared", PermissionReadonly, now)
	createLeaseRun(t, store, "read-2", "shared", PermissionReadonly, now.Add(time.Millisecond))
	createLeaseRun(t, store, "write", "shared", PermissionReadwrite, now.Add(2*time.Millisecond))

	first := mustClaim(t, store, "worker-1", now)
	second := mustClaim(t, store, "worker-2", now)
	if first.Lease.Mode != LeaseReadonly || second.Lease.Mode != LeaseReadonly {
		t.Fatalf("lease modes = %s, %s", first.Lease.Mode, second.Lease.Mode)
	}
	if _, found, err := store.ClaimNext(ctx, "worker-3", now, time.Minute); err != nil || found {
		t.Fatalf("writer claim while readers active: found=%v err=%v", found, err)
	}
	if err := store.PrepareClaim(ctx, first, now.Add(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteClaim(ctx, first, RunCancelled, "released in test", json.RawMessage(`{"ok":true}`), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimNext(ctx, "worker-3", now.Add(time.Second), time.Minute); err != nil || found {
		t.Fatalf("writer claim with one reader active: found=%v err=%v", found, err)
	}
	if err := store.PrepareClaim(ctx, second, now.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteClaim(ctx, second, RunCancelled, "released in test", nil, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	writer := mustClaimAt(t, store, "worker-3", now.Add(2*time.Second))
	if writer.Run.ID != "write" || writer.Lease.Mode != LeaseWrite {
		t.Fatalf("writer claim = %#v", writer)
	}
}

func TestWriterBlocksReadonlyAndOtherWorkspacesRemainRunnable(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, store, "writer", "shared", PermissionReadwrite, now)
	createLeaseRun(t, store, "reader", "shared", PermissionReadonly, now.Add(time.Millisecond))
	createLeaseRun(t, store, "other", "other", PermissionReadwrite, now.Add(2*time.Millisecond))
	writer := mustClaim(t, store, "worker-1", now)
	if writer.Run.ID != "writer" {
		t.Fatalf("first run = %s", writer.Run.ID)
	}
	other := mustClaim(t, store, "worker-2", now)
	if other.Run.ID != "other" {
		t.Fatalf("claim should skip blocked reader, got %s", other.Run.ID)
	}
	if _, found, err := store.ClaimNext(ctx, "worker-3", now, time.Minute); err != nil || found {
		t.Fatalf("reader claimed behind writer: found=%v err=%v", found, err)
	}
}

func TestAttemptLifecycleAndHeartbeat(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, store, "run-1", "shared", PermissionReadwrite, now)
	claim := mustClaim(t, store, "worker-1", now)
	if err := store.HeartbeatLease(ctx, claim, now.Add(10*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareClaim(ctx, claim, now.Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.StartAttempt(ctx, claim, 1234, now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteClaim(ctx, claim, RunSucceeded, "finished", json.RawMessage(`{"exit_code":0}`), now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != RunSucceeded || run.Conclusion != "finished" {
		t.Fatalf("run = %#v", run)
	}
	var attemptState RunState
	var processID int
	if err := store.db.QueryRow(`SELECT state, process_id FROM run_attempts WHERE id = ?`, claim.Attempt.ID).Scan(&attemptState, &processID); err != nil {
		t.Fatal(err)
	}
	if attemptState != RunSucceeded || processID != 1234 {
		t.Fatalf("attempt state=%s process=%d", attemptState, processID)
	}
	var leaseCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM run_leases WHERE run_id = ?`, run.ID).Scan(&leaseCount); err != nil {
		t.Fatal(err)
	}
	if leaseCount != 0 {
		t.Fatalf("lease count = %d", leaseCount)
	}
}

func TestExpiredLeaseRecovery(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, store, "not-started", "one", PermissionReadwrite, now)
	createLeaseRun(t, store, "started", "two", PermissionReadwrite, now.Add(time.Millisecond))
	first := mustClaim(t, store, "worker-1", now)
	second := mustClaim(t, store, "worker-2", now)
	if err := store.PrepareClaim(ctx, second, now.Add(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := store.StartAttempt(ctx, second, 4321, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverExpiredLeases(ctx, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Fatalf("recovered = %d, want 2", recovered)
	}
	firstRun, _ := store.GetRun(ctx, first.Run.ID)
	secondRun, _ := store.GetRun(ctx, second.Run.ID)
	if firstRun.State != RunPending || secondRun.State != RunInterrupted {
		t.Fatalf("recovered states = %s, %s", firstRun.State, secondRun.State)
	}
	retry := mustClaimAt(t, store, "worker-3", now.Add(2*time.Minute))
	if retry.Run.ID != first.Run.ID || retry.Attempt.Number != 2 {
		t.Fatalf("retry claim = %#v", retry)
	}
}

func TestLeaseValidation(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if _, _, err := store.ClaimNext(ctx, "", time.Now(), time.Minute); err == nil {
		t.Fatal("empty owner accepted")
	}
	if _, _, err := store.ClaimNext(ctx, "worker", time.Now(), 0); err == nil {
		t.Fatal("zero TTL accepted")
	}
	claim := Claim{Run: Run{ID: "missing"}, Lease: Lease{Owner: "worker"}}
	if err := store.StartAttempt(ctx, claim, 0, time.Now()); err == nil {
		t.Fatal("zero PID accepted")
	}
	if err := store.HeartbeatLease(ctx, claim, time.Now(), 0); err == nil {
		t.Fatal("zero heartbeat TTL accepted")
	}
	if err := store.CompleteClaim(ctx, claim, RunRunning, "", nil, time.Now()); err == nil {
		t.Fatal("nonterminal completion accepted")
	}
	if err := store.CompleteClaim(ctx, claim, RunFailed, "", json.RawMessage(`{`), time.Now()); err == nil {
		t.Fatal("invalid result JSON accepted")
	}
	if err := store.HeartbeatLease(ctx, claim, time.Now(), time.Minute); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing heartbeat error = %v", err)
	}
}

func createLeaseRun(t *testing.T, store *Store, id, workspace string, permission Permission, now time.Time) {
	t.Helper()
	run := testRun(id, now)
	run.Workspace = workspace
	run.Permission = permission
	if _, _, err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func mustClaim(t *testing.T, store *Store, owner string, now time.Time) Claim {
	t.Helper()
	return mustClaimAt(t, store, owner, now)
}

func mustClaimAt(t *testing.T, store *Store, owner string, now time.Time) Claim {
	t.Helper()
	claim, found, err := store.ClaimNext(context.Background(), owner, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("no run claimed")
	}
	return claim
}
