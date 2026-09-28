package worker

import (
	"context"
	"time"
)

// Runtime keeps a worker alive while Studio is open. Wake is edge-triggered
// and non-blocking; each wake drains all currently runnable work.
type Runtime struct {
	worker *Worker
	wake   chan struct{}
	done   chan struct{}
}

func StartRuntime(ctx context.Context, worker *Worker) *Runtime {
	runtime := &Runtime{worker: worker, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go runtime.loop(ctx)
	runtime.Wake() // startup recovery and pending-work drain
	return runtime
}

func (runtime *Runtime) Wake() {
	select {
	case runtime.wake <- struct{}{}:
	default:
	}
}

func (runtime *Runtime) Cancel(runID string) bool { return runtime.worker.Cancel(runID) }

func (runtime *Runtime) Wait() { <-runtime.done }

func (runtime *Runtime) loop(ctx context.Context) {
	defer close(runtime.done)
	// The ticker is a safety net for runs persisted by future event sources that
	// do not share Studio's in-process wake callback.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		var retryTimer *time.Timer
		var retryDue <-chan time.Time
		if due, found, err := runtime.worker.config.Store.NextRetryDue(ctx); err == nil && found {
			delay := time.Until(due)
			if delay < 0 {
				delay = 0
			}
			retryTimer = time.NewTimer(delay)
			retryDue = retryTimer.C
		}
		select {
		case <-ctx.Done():
			stopTimer(retryTimer)
			return
		case <-runtime.wake:
		case <-ticker.C:
		case <-retryDue:
		}
		stopTimer(retryTimer)
		for ctx.Err() == nil {
			found, _ := runtime.worker.RunOnce(ctx)
			if !found {
				break
			}
		}
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil || !timer.Stop() {
		return
	}
}
