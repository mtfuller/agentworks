package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestRuntimeDrainsPendingWorkOnStartup(t *testing.T) {
	fixture := newWorkerFixture(t, "success")
	ctx, cancel := context.WithCancel(context.Background())
	runtime := StartRuntime(ctx, fixture.worker)
	waitForRunState(t, fixture.store, fixture.runID, store.RunSucceeded)
	cancel()
	runtime.Wait()
}

func TestRuntimeWakesForDurableRetryTimer(t *testing.T) {
	fixture := newWorkerFixture(t, "retry")
	ctx, cancel := context.WithCancel(context.Background())
	runtime := StartRuntime(ctx, fixture.worker)
	waitForRunState(t, fixture.store, fixture.runID, store.RunSucceeded)
	attempts, err := fixture.store.ListAttempts(context.Background(), fixture.runID)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
	cancel()
	runtime.Wait()
}

func TestRuntimeCancelsActiveRun(t *testing.T) {
	fixture := newWorkerFixtureWithTiming(t, "hang", time.Second, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := StartRuntime(ctx, fixture.worker)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(fixture.workspace, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake harness did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !runtime.Cancel(fixture.runID) {
		t.Fatal("active run was not owned by runtime")
	}
	waitForRunState(t, fixture.store, fixture.runID, store.RunCancelled)
	cancel()
	runtime.Wait()
}

func waitForRunState(t *testing.T, runtimeStore *store.Store, runID string, wanted store.RunState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := runtimeStore.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.State == wanted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run state = %s, want %s", run.State, wanted)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
