package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestProposalRequiresExplicitApplyAndDetectsExternalEdit(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runtimeStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	now := time.Now().UTC()
	_, _, err = runtimeStore.CreateRun(ctx, store.Run{ID: "run-1", IdempotencyKey: "run-1", Agent: "agent", Workspace: "workspace", Harness: "fake", Permission: store.PermissionReadonly, State: store.RunSucceeded, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	target := "memory/agent.md"
	absolute := filepath.Join(root, target)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte("# Memory\n")
	if err := os.WriteFile(absolute, before, 0o600); err != nil {
		t.Fatal(err)
	}
	proposal := AppendProposal("proposal-1", "run-1", "agent", target, before, []string{"Use focused tests"}, now)
	if err := runtimeStore.CreateMemoryProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(absolute)
	if string(data) != string(before) {
		t.Fatal("proposal changed memory before approval")
	}
	if err := Apply(ctx, runtimeStore, root, proposal.ID); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(absolute)
	if string(data) == string(before) {
		t.Fatal("approved proposal did not change memory")
	}

	proposal = AppendProposal("proposal-2", "run-1", "agent", target, data, []string{"Another fact"}, now)
	if err := runtimeStore.CreateMemoryProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte("external edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, runtimeStore, root, proposal.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Apply() error=%v", err)
	}
	stored, _ := runtimeStore.GetMemoryProposal(ctx, proposal.ID)
	if stored.State != store.MemoryProposalConflict {
		t.Fatalf("state=%s", stored.State)
	}
	escape := AppendProposal("proposal-3", "run-1", "agent", "../outside.md", before, []string{"escape"}, now)
	if err := runtimeStore.CreateMemoryProposal(ctx, escape); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, runtimeStore, root, escape.ID); err == nil {
		t.Fatal("escaping memory target was accepted")
	}
}
