package approval

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestRequestValidation(t *testing.T) {
	valid := []Request{
		{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`)},
		{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"src/generated"}`)},
		{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"go","subcommand":["test","./..."]}`)},
	}
	for _, request := range valid {
		if err := request.Validate(); err != nil {
			t.Errorf("valid request %#v: %v", request, err)
		}
	}
	invalid := []Request{
		{},
		{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"../outside"}`)},
		{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"C:/outside"}`)},
		{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":".","extra":true}`)},
		{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"/bin/go","subcommand":["test"]}`)},
		{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"go","subcommand":[]}`)},
		{Kind: "network", Scope: json.RawMessage(`{}`)},
	}
	for _, request := range invalid {
		if err := request.Validate(); err == nil {
			t.Errorf("invalid request accepted: %#v", request)
		}
	}
}

func TestBoundedGrantMatching(t *testing.T) {
	commandGrant := store.Approval{
		Kind: "command.execute", State: store.ApprovalApproved,
		Scope: json.RawMessage(`{"executable":"git","subcommand":["status"]}`),
	}
	for _, test := range []struct {
		request Request
		want    bool
	}{
		{Request{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"git","subcommand":["status"]}`)}, true},
		{Request{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"git","subcommand":["status","--short"]}`)}, true},
		{Request{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"git","subcommand":["push"]}`)}, false},
		{Request{Kind: "command.execute", Scope: json.RawMessage(`{"executable":"sh","subcommand":["status"]}`)}, false},
	} {
		if got := test.request.CoveredBy(commandGrant); got != test.want {
			t.Errorf("CoveredBy(%s) = %v, want %v", test.request.Scope, got, test.want)
		}
	}
	directoryGrant := store.Approval{
		Kind: "filesystem.write", State: store.ApprovalApproved,
		Scope: json.RawMessage(`{"directory":"src"}`),
	}
	if !(Request{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"src/generated"}`)}).CoveredBy(directoryGrant) {
		t.Fatal("child directory was not covered")
	}
	if (Request{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"test"}`)}).CoveredBy(directoryGrant) {
		t.Fatal("sibling directory was covered")
	}
}

func TestBrokerWaitsForDecisionAndReusesRunGrant(t *testing.T) {
	runtimeStore, claim := approvalFixture(t)
	broker := Broker{Store: runtimeStore, PollInterval: 5 * time.Millisecond, LeaseTTL: time.Minute}
	request := Request{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`), Summary: "write files"}
	done := make(chan struct {
		approved bool
		err      error
	}, 1)
	go func() {
		approved, err := broker.Authorize(context.Background(), claim, request)
		done <- struct {
			approved bool
			err      error
		}{approved, err}
	}()
	approval := waitForBrokerApproval(t, runtimeStore, claim.Run.ID)
	if approval.Summary != request.Summary {
		t.Fatalf("summary = %q", approval.Summary)
	}
	if _, err := runtimeStore.DecideApproval(context.Background(), approval.ID, store.ApprovalApproved, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if !result.approved || result.err != nil {
		t.Fatalf("approved=%v err=%v", result.approved, result.err)
	}
	approved, err := broker.Authorize(context.Background(), claim, request)
	if err != nil || !approved {
		t.Fatalf("reused approved=%v err=%v", approved, err)
	}
	approvals, _ := runtimeStore.ListApprovals(context.Background(), claim.Run.ID)
	if len(approvals) != 1 {
		t.Fatalf("approval count = %d", len(approvals))
	}
}

func TestBrokerCancellationCancelsPendingApproval(t *testing.T) {
	runtimeStore, claim := approvalFixture(t)
	broker := Broker{Store: runtimeStore, PollInterval: 5 * time.Millisecond, LeaseTTL: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := broker.Authorize(ctx, claim, Request{
			Kind: "command.execute", Scope: json.RawMessage(`{"executable":"go","subcommand":["test"]}`),
		})
		done <- err
	}()
	approval := waitForBrokerApproval(t, runtimeStore, claim.Run.ID)
	cancel()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancellation error = %v", err)
	}
	approval, err := runtimeStore.GetApproval(context.Background(), approval.ID)
	if err != nil || approval.State != store.ApprovalCancelled {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
}

func TestBrokerDenialAndValidationFailures(t *testing.T) {
	request := Request{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`)}
	if _, err := (Broker{}).Authorize(context.Background(), store.Claim{}, request); err == nil {
		t.Fatal("nil store accepted")
	}
	runtimeStore, claim := approvalFixture(t)
	if _, err := (Broker{Store: runtimeStore}).Authorize(context.Background(), claim, Request{}); err == nil {
		t.Fatal("invalid request accepted")
	}
	broker := Broker{Store: runtimeStore, PollInterval: 5 * time.Millisecond, LeaseTTL: time.Minute}
	done := make(chan struct {
		approved bool
		err      error
	}, 1)
	go func() {
		approved, err := broker.Authorize(context.Background(), claim, request)
		done <- struct {
			approved bool
			err      error
		}{approved, err}
	}()
	approval := waitForBrokerApproval(t, runtimeStore, claim.Run.ID)
	if _, err := runtimeStore.DecideApproval(context.Background(), approval.ID, store.ApprovalDenied, "not now", time.Now()); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.approved || result.err != nil {
		t.Fatalf("denied authorization = approved %v, err %v", result.approved, result.err)
	}
}

func approvalFixture(t *testing.T) (*store.Store, store.Claim) {
	t.Helper()
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeStore.Close() })
	now := time.Now().UTC()
	_, _, err = runtimeStore.CreateRun(context.Background(), store.Run{
		ID: "run-1", IdempotencyKey: "run:1", Agent: "agent", Workspace: "workspace", Harness: "fake",
		Permission: store.PermissionReadwrite, State: store.RunPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := runtimeStore.ClaimNext(context.Background(), "worker", now, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	if err := runtimeStore.PrepareClaim(context.Background(), claim, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return runtimeStore, claim
}

func waitForBrokerApproval(t *testing.T, runtimeStore *store.Store, runID string) store.Approval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		approvals, err := runtimeStore.ListApprovals(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if len(approvals) != 0 {
			return approvals[len(approvals)-1]
		}
		if time.Now().After(deadline) {
			t.Fatal("approval not requested")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
