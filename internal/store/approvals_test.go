package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestApprovalLifecycleAndRunOnlyScopeReuse(t *testing.T) {
	runtimeStore := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, runtimeStore, "run-approval", "workspace", PermissionReadwrite, now)
	claim := mustClaim(t, runtimeStore, "worker", now)
	if err := runtimeStore.PrepareClaim(ctx, claim, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	scope := json.RawMessage(`{ "directory": "." }`)
	pending, created, err := runtimeStore.RequestApproval(ctx, claim, Approval{
		ID: "approval-1", Kind: "filesystem.write", Scope: scope,
	}, now.Add(2*time.Millisecond))
	if err != nil || !created || pending.State != ApprovalPending || pending.ResumeState != RunPreparing {
		t.Fatalf("pending=%#v created=%v err=%v", pending, created, err)
	}
	run, _ := runtimeStore.GetRun(ctx, claim.Run.ID)
	if run.State != RunWaitingForApproval {
		t.Fatalf("run state = %s", run.State)
	}
	approved, err := runtimeStore.DecideApproval(ctx, pending.ID, ApprovalApproved, "approved in test", now.Add(3*time.Millisecond))
	if err != nil || approved.State != ApprovalApproved {
		t.Fatalf("approved=%#v err=%v", approved, err)
	}
	run, _ = runtimeStore.GetRun(ctx, claim.Run.ID)
	if run.State != RunPreparing {
		t.Fatalf("resumed state = %s", run.State)
	}
	reused, created, err := runtimeStore.RequestApproval(ctx, claim, Approval{
		ID: "approval-2", Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`),
	}, now.Add(4*time.Millisecond))
	if err != nil || created || reused.ID != pending.ID || reused.State != ApprovalApproved {
		t.Fatalf("reused=%#v created=%v err=%v", reused, created, err)
	}
	approvals, err := runtimeStore.ListApprovals(ctx, claim.Run.ID)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("approvals=%#v err=%v", approvals, err)
	}
}

func TestApprovalDenialConflictAndCancellation(t *testing.T) {
	runtimeStore := openTestStore(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	createLeaseRun(t, runtimeStore, "run-denied", "workspace", PermissionReadwrite, now)
	claim := mustClaim(t, runtimeStore, "worker", now)
	if err := runtimeStore.PrepareClaim(ctx, claim, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	pending, _, err := runtimeStore.RequestApproval(ctx, claim, Approval{
		ID: "approval-denied", Kind: "command.execute", Scope: json.RawMessage(`{"executable":"go","subcommand":["test"]}`),
	}, now.Add(2*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeStore.DecideApproval(ctx, pending.ID, ApprovalDenied, "not now", now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	deniedRun, err := runtimeStore.GetRun(ctx, claim.Run.ID)
	if err != nil || deniedRun.State != RunPreparing {
		t.Fatalf("denied run did not resume: %#v err=%v", deniedRun, err)
	}
	if _, err := runtimeStore.DecideApproval(ctx, pending.ID, ApprovalApproved, "changed mind", now.Add(4*time.Millisecond)); !errors.Is(err, ErrConflict) {
		t.Fatalf("second decision error = %v", err)
	}
	if err := runtimeStore.CompleteClaim(ctx, claim, RunCancelled, "approval denied", nil, now.Add(5*time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	createLeaseRun(t, runtimeStore, "run-cancel", "other", PermissionReadwrite, now.Add(time.Second))
	second := mustClaimAt(t, runtimeStore, "worker", now.Add(time.Second))
	if err := runtimeStore.PrepareClaim(ctx, second, now.Add(time.Second+time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	cancellable, _, err := runtimeStore.RequestApproval(ctx, second, Approval{
		ID: "approval-cancel", Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`),
	}, now.Add(time.Second+2*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.CancelApproval(ctx, cancellable.ID, "shutdown", now.Add(time.Second+3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	cancelled, err := runtimeStore.GetApproval(ctx, cancellable.ID)
	if err != nil || cancelled.State != ApprovalCancelled {
		t.Fatalf("cancelled=%#v err=%v", cancelled, err)
	}
}
