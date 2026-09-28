// Package memory creates and applies reviewable Markdown memory proposals.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func SHA256(data []byte) string { digest := sha256.Sum256(data); return hex.EncodeToString(digest[:]) }

func AppendProposal(id, runID, scope, target string, before []byte, suggestions []string, at time.Time) store.MemoryProposal {
	after := strings.TrimRight(string(before), "\n")
	for _, suggestion := range suggestions {
		suggestion = strings.TrimSpace(suggestion)
		if suggestion != "" {
			after += "\n- " + suggestion
		}
	}
	after += "\n"
	diff := fmt.Sprintf("--- a/%s\n+++ b/%s\n@@ memory @@\n-%s\n+%s", target, target, strings.ReplaceAll(string(before), "\n", "\n-"), strings.ReplaceAll(after, "\n", "\n+"))
	return store.MemoryProposal{ID: id, RunID: runID, Scope: scope, TargetPath: filepath.ToSlash(target), BaseSHA256: SHA256(before), BeforeMarkdown: string(before), AfterMarkdown: after, Diff: diff, State: store.MemoryProposalPending, CreatedAt: at}
}

// Apply verifies both path confinement and the proposal's base digest before
// atomically replacing the file and recording the decision.
func Apply(ctx context.Context, runtimeStore *store.Store, root, id string) error {
	p, err := runtimeStore.GetMemoryProposal(ctx, id)
	if err != nil {
		return err
	}
	clean := filepath.Clean(filepath.FromSlash(p.TargetPath))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("memory target escapes project root")
	}
	target := filepath.Join(root, clean)
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedTarget, targetErr := filepath.EvalSymlinks(target)
	if rootErr != nil || targetErr != nil {
		return errors.New("memory target cannot be resolved safely")
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("memory target escapes project root")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if SHA256(data) != p.BaseSHA256 {
		_ = runtimeStore.DecideMemoryProposal(ctx, id, store.MemoryProposalConflict, json.RawMessage(`{"reason":"file changed on disk"}`), time.Now().UTC())
		return fmt.Errorf("%w: memory file changed on disk", store.ErrConflict)
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("memory target is not a regular file")
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".agentworks-memory-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(p.AfterMarkdown); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	latest, err := os.ReadFile(target)
	if err != nil || SHA256(latest) != p.BaseSHA256 {
		_ = runtimeStore.DecideMemoryProposal(ctx, id, store.MemoryProposalConflict, json.RawMessage(`{"reason":"file changed on disk"}`), time.Now().UTC())
		return fmt.Errorf("%w: memory file changed on disk", store.ErrConflict)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return err
	}
	return runtimeStore.DecideMemoryProposal(ctx, id, store.MemoryProposalApproved, nil, time.Now().UTC())
}

func Reject(ctx context.Context, runtimeStore *store.Store, id string) error {
	return runtimeStore.DecideMemoryProposal(ctx, id, store.MemoryProposalRejected, nil, time.Now().UTC())
}
