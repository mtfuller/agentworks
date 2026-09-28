// Package scheduler turns durable schedule timers into ordinary routed events.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
)

const maxCatchUpEvents = 100

type Runtime struct {
	root   string
	store  *store.Store
	events *router.Service
	wake   chan struct{}
	done   chan struct{}
}

func Start(ctx context.Context, root string, runtimeStore *store.Store, events *router.Service) *Runtime {
	runtime := &Runtime{root: root, store: runtimeStore, events: events, wake: make(chan struct{}, 1), done: make(chan struct{})}
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		_ = Sync(ctx, runtime.root, runtime.store, time.Now().UTC())
		_ = ProcessDue(ctx, runtime.root, runtime.store, runtime.events, time.Now().UTC())
		var timer *time.Timer
		var due <-chan time.Time
		if next, found, err := runtime.store.NextScheduleDue(ctx); err == nil && found {
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			due = timer.C
		}
		select {
		case <-ctx.Done():
			stop(timer)
			return
		case <-runtime.wake:
		case <-ticker.C:
		case <-due:
		}
		stop(timer)
	}
}
func stop(timer *time.Timer) {
	if timer != nil {
		timer.Stop()
	}
}

func Sync(ctx context.Context, root string, runtimeStore *store.Store, now time.Time) error {
	sources, _ := spec.DiscoverSources(root)
	active := map[string]bool{}
	for _, source := range sources {
		if source.Kind != spec.SourceScheduled || !source.IsEnabled() {
			continue
		}
		interval, err := source.Interval()
		if err != nil {
			return err
		}
		due := source.Schedule.StartAt.UTC()
		if due.IsZero() {
			due = now.Add(interval)
		}
		id := "schedule:" + source.Name
		payload, _ := json.Marshal(map[string]string{"source": source.Name})
		if err := runtimeStore.UpsertScheduleTimer(ctx, id, due, payload, now); err != nil {
			return err
		}
		active[id] = true
	}
	return runtimeStore.CancelScheduleTimersExcept(ctx, active, now)
}

func ProcessDue(ctx context.Context, root string, runtimeStore *store.Store, events *router.Service, now time.Time) error {
	timers, err := runtimeStore.DueScheduleTimers(ctx, now, 100)
	if err != nil {
		return err
	}
	sources, _ := spec.DiscoverSources(root)
	byName := map[string]spec.Source{}
	for _, source := range sources {
		byName[source.Name] = source
	}
	for _, timer := range timers {
		var payload struct {
			Source string `json:"source"`
		}
		if json.Unmarshal(timer.Payload, &payload) != nil {
			continue
		}
		source, ok := byName[payload.Source]
		if !ok || !source.IsEnabled() || source.Kind != spec.SourceScheduled {
			continue
		}
		interval, err := source.Interval()
		if err != nil {
			return err
		}
		occurrences, next := scheduleOccurrences(source.Schedule.CatchUp, timer.DueAt, now, interval)
		for _, occurred := range occurrences {
			data, _ := json.Marshal(source.Event.Data)
			_, err := events.Ingest(ctx, router.Envelope{SpecVersion: "1.0", ID: fmt.Sprintf("%s:%d", source.Name, occurred.UnixMilli()), Source: source.Name, Type: source.Event.Type, Subject: source.Event.Subject, Time: occurred, Data: data})
			if err != nil {
				return err
			}
		}
		if err := runtimeStore.AdvanceScheduleTimer(ctx, timer.ID, timer.DueAt, next, now); err != nil {
			return err
		}
	}
	return nil
}

func scheduleOccurrences(policy string, due, now time.Time, interval time.Duration) ([]time.Time, time.Time) {
	if now.Before(due) {
		return []time.Time{}, due
	}
	steps := int(now.Sub(due) / interval)
	switch policy {
	case "all":
		count := steps + 1
		if count > maxCatchUpEvents {
			count = maxCatchUpEvents
		}
		values := make([]time.Time, 0, count)
		for index := 0; index < count; index++ {
			values = append(values, due.Add(time.Duration(index)*interval))
		}
		return values, due.Add(time.Duration(count) * interval)
	case "skip":
		next := due.Add(time.Duration(steps+1) * interval)
		tolerance := interval / 10
		if tolerance > time.Minute {
			tolerance = time.Minute
		}
		if now.Sub(due) <= tolerance {
			return []time.Time{due}, due.Add(interval)
		}
		return []time.Time{}, next
	default:
		latest := due.Add(time.Duration(steps) * interval)
		return []time.Time{latest}, latest.Add(interval)
	}
}
