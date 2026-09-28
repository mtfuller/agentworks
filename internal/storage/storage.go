// Package storage accounts for and prunes per-project runtime detail.
package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

type Manager struct {
	Store   *store.Store
	LogRoot string
	Limit   int64
}

type Report struct {
	BeforeBytes int64    `json:"before_bytes"`
	AfterBytes  int64    `json:"after_bytes"`
	LimitBytes  int64    `json:"limit_bytes"`
	PrunedRuns  []string `json:"pruned_runs"`
	OverLimit   bool     `json:"over_limit"`
}

func Usage(logRoot string) (int64, error) {
	if strings.TrimSpace(logRoot) == "" {
		return 0, nil
	}
	root := filepath.Dir(filepath.Clean(logRoot))
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == root {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		return nil
	})
	return bytes, err
}

func (manager Manager) Prune(ctx context.Context) (Report, error) {
	report := Report{LimitBytes: manager.Limit, PrunedRuns: []string{}}
	if manager.Store == nil || manager.Limit <= 0 {
		return report, errors.New("storage manager requires a store and positive limit")
	}
	before, err := Usage(manager.LogRoot)
	if err != nil {
		return report, err
	}
	report.BeforeBytes = before
	if before <= manager.Limit {
		report.AfterBytes = before
		return report, nil
	}
	candidates, err := manager.Store.ListPrunableRuns(ctx, 500)
	if err != nil {
		return report, err
	}
	for _, candidate := range candidates {
		if err := manager.deleteLogs(candidate); err != nil {
			return report, err
		}
		if err := manager.Store.PruneRunDetail(ctx, candidate.RunID, time.Now().UTC()); err != nil {
			return report, err
		}
		report.PrunedRuns = append(report.PrunedRuns, candidate.RunID)
		used, err := Usage(manager.LogRoot)
		if err != nil {
			return report, err
		}
		if used <= manager.Limit {
			break
		}
	}
	if len(report.PrunedRuns) > 0 {
		_ = manager.Store.Compact(ctx)
	}
	report.AfterBytes, err = Usage(manager.LogRoot)
	report.OverLimit = report.AfterBytes > manager.Limit
	return report, err
}

func (manager Manager) DeleteRunDetail(ctx context.Context, runID string) error {
	run, err := manager.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Pinned {
		return errors.New("unpin the run before deleting its raw detail")
	}
	attempts, err := manager.Store.ListAttempts(ctx, runID)
	if err != nil {
		return err
	}
	candidate := store.PrunableRun{RunID: runID, AttemptIDs: make([]string, 0, len(attempts))}
	for _, attempt := range attempts {
		candidate.AttemptIDs = append(candidate.AttemptIDs, attempt.ID)
	}
	if err := manager.deleteLogs(candidate); err != nil {
		return err
	}
	return manager.Store.PruneRunDetail(ctx, runID, time.Now().UTC())
}

func (manager Manager) deleteLogs(candidate store.PrunableRun) error {
	for _, attemptID := range candidate.AttemptIDs {
		path := worker.LogPath(manager.LogRoot, candidate.RunID, attemptID)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	runDir := filepath.Dir(worker.LogPath(manager.LogRoot, candidate.RunID, "attempt"))
	_ = os.Remove(runDir)
	return nil
}

type Runtime struct{ done chan struct{} }

func Start(ctx context.Context, manager Manager) *Runtime {
	runtime := &Runtime{done: make(chan struct{})}
	go func() {
		defer close(runtime.done)
		_, _ = manager.Prune(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = manager.Prune(ctx)
			}
		}
	}()
	return runtime
}

func (runtime *Runtime) Wait() { <-runtime.done }
