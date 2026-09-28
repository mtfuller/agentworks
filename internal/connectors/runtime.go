package connectors

import (
	"context"
	"time"
)

type Runtime struct {
	service *Service
	wake    chan struct{}
	done    chan struct{}
}

func Start(ctx context.Context, service *Service) *Runtime {
	runtime := &Runtime{service: service, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go runtime.loop(ctx)
	runtime.Wake()
	return runtime
}

func (runtime *Runtime) Wake() {
	select {
	case runtime.wake <- struct{}{}:
	default:
	}
}

func (runtime *Runtime) Wait() { <-runtime.done }

func (runtime *Runtime) loop(ctx context.Context) {
	defer close(runtime.done)
	for {
		_ = runtime.service.Router.RecoverIngested(ctx)
		_, _ = runtime.service.Sync(ctx)
		due, _ := runtime.service.Store.DueSources(ctx, runtime.service.now(), 100)
		for _, source := range due {
			_, _ = runtime.service.PollNow(ctx, source.ID)
		}
		wait := 30 * time.Second
		if next, found, err := runtime.service.Store.NextSourceDue(ctx); err == nil && found {
			wait = next.Sub(runtime.service.now())
			if wait < 0 {
				wait = 0
			}
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopTimer(timer)
			return
		case <-runtime.wake:
		case <-timer.C:
		}
		stopTimer(timer)
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
