// Package monitors evaluates portable outcome facts into deterministic health.
package monitors

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

type Template struct {
	ID          string
	SuccessType string
	FailureType string
	Window      time.Duration
}

var SoftwareDelivery = []Template{
	{ID: "pr-opened", SuccessType: "github.pr.opened", FailureType: "github.pr.open.failed", Window: 14 * 24 * time.Hour},
	{ID: "checks-passed", SuccessType: "github.checks.passed", FailureType: "github.checks.failed", Window: 7 * 24 * time.Hour},
	{ID: "pr-merged", SuccessType: "github.pr.merged", FailureType: "github.pr.merge.failed", Window: 30 * 24 * time.Hour},
	{ID: "jira-transitioned", SuccessType: "jira.issue.transitioned", FailureType: "jira.issue.transition.failed", Window: 30 * 24 * time.Hour},
}

func Evaluate(template Template, outcomes []store.Outcome, now time.Time) store.MonitorState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-template.Window)
	relevant := make([]store.Outcome, 0)
	for _, outcome := range outcomes {
		if !outcome.OccurredAt.Before(cutoff) && (outcome.Type == template.SuccessType || outcome.Type == template.FailureType) {
			relevant = append(relevant, outcome)
		}
	}
	sort.SliceStable(relevant, func(i, j int) bool {
		if relevant[i].OccurredAt.Equal(relevant[j].OccurredAt) {
			return relevant[i].ID < relevant[j].ID
		}
		return relevant[i].OccurredAt.Before(relevant[j].OccurredAt)
	})
	state := "unknown"
	latest := ""
	if len(relevant) > 0 {
		latest = relevant[len(relevant)-1].Type
		if latest == template.SuccessType {
			state = "healthy"
		} else {
			state = "unhealthy"
		}
	}
	window, _ := json.Marshal(map[string]any{"since": cutoff, "until": now, "duration": template.Window.String()})
	details, _ := json.Marshal(map[string]any{"success_type": template.SuccessType, "failure_type": template.FailureType, "matching_outcomes": len(relevant), "latest_type": latest})
	return store.MonitorState{MonitorID: template.ID, Revision: "software-delivery-v1", State: state, Window: window, Details: details, EvaluatedAt: now}
}

func EvaluateCorrectionRate(outcomes []store.Outcome, now time.Time) store.MonitorState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-30 * 24 * time.Hour)
	completed, corrections := 0, 0
	for _, o := range outcomes {
		if o.OccurredAt.Before(cutoff) {
			continue
		}
		if o.Type == "agent.run.completed" {
			completed++
		}
		if o.Type == "human.correction" {
			corrections++
		}
	}
	state := "unknown"
	rate := 0.0
	if completed > 0 {
		rate = float64(corrections) / float64(completed)
		state = "healthy"
		if rate > 0.2 {
			state = "unhealthy"
		}
	}
	window, _ := json.Marshal(map[string]any{"since": cutoff, "until": now, "duration": "720h0m0s"})
	details, _ := json.Marshal(map[string]any{"completed_runs": completed, "human_corrections": corrections, "rate": rate, "maximum_healthy_rate": 0.2})
	return store.MonitorState{MonitorID: "low-human-correction-rate", Revision: "software-delivery-v1", State: state, Window: window, Details: details, EvaluatedAt: now}
}

func EvaluateSoftwareDelivery(outcomes []store.Outcome, now time.Time) []store.MonitorState {
	states := make([]store.MonitorState, 0, len(SoftwareDelivery)+1)
	for _, template := range SoftwareDelivery {
		states = append(states, Evaluate(template, outcomes, now))
	}
	return append(states, EvaluateCorrectionRate(outcomes, now))
}

func EvaluateOperational(metrics store.RuntimeMetrics, now time.Time) []store.MonitorState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	makeState := func(id, state string, details map[string]any) store.MonitorState {
		window, _ := json.Marshal(map[string]any{"at": now})
		encoded, _ := json.Marshal(details)
		return store.MonitorState{MonitorID: id, Revision: "runtime-v1", State: state, Window: window, Details: encoded, EvaluatedAt: now}
	}
	queueState := "healthy"
	if metrics.OldestQueueDelay > 30*time.Minute {
		queueState = "unhealthy"
	} else if metrics.OldestQueueDelay > 5*time.Minute {
		queueState = "warning"
	}
	retryState, retryRate := "unknown", 0.0
	if metrics.Attempts > 0 {
		retryRate = float64(metrics.RetryAttempts) / float64(metrics.Attempts)
		retryState = "healthy"
		if retryRate > 0.25 {
			retryState = "unhealthy"
		} else if retryRate > 0.10 {
			retryState = "warning"
		}
	}
	durationState := "unknown"
	if metrics.CompletedRuns > 0 {
		durationState = "healthy"
		if metrics.AverageDuration > 30*time.Minute {
			durationState = "unhealthy"
		} else if metrics.AverageDuration > 15*time.Minute {
			durationState = "warning"
		}
	}
	sourceState := "unknown"
	if metrics.ConfiguredSources > 0 {
		sourceState = "healthy"
		if metrics.StaleSources > 0 {
			sourceState = "unhealthy"
		}
	}
	return []store.MonitorState{
		makeState("queue-delay", queueState, map[string]any{"pending_runs": metrics.PendingRuns, "oldest_delay_ms": metrics.OldestQueueDelay.Milliseconds()}),
		makeState("retry-rate", retryState, map[string]any{"attempts": metrics.Attempts, "retry_attempts": metrics.RetryAttempts, "rate": retryRate}),
		makeState("run-duration", durationState, map[string]any{"completed_runs": metrics.CompletedRuns, "average_duration_ms": metrics.AverageDuration.Milliseconds()}),
		makeState("source-freshness", sourceState, map[string]any{"configured_sources": metrics.ConfiguredSources, "stale_sources": metrics.StaleSources}),
	}
}

func EvaluateAndStore(ctx context.Context, runtimeStore *store.Store, now time.Time) error {
	outcomes, err := runtimeStore.ListOutcomes(ctx, "", 500)
	if err != nil {
		return err
	}
	states := EvaluateSoftwareDelivery(outcomes, now)
	metrics, err := runtimeStore.ReadRuntimeMetrics(ctx, now)
	if err != nil {
		return err
	}
	states = append(states, EvaluateOperational(metrics, now)...)
	for _, state := range states {
		if err := runtimeStore.PutMonitorState(ctx, state); err != nil {
			return err
		}
	}
	return nil
}
