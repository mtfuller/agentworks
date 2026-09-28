package monitors

import (
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestSoftwareDeliveryMonitorFollowsLatestGenericOutcome(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	template := SoftwareDelivery[1]
	values := []store.Outcome{{ID: "a", Type: "github.checks.failed", OccurredAt: now.Add(-time.Hour)}}
	if got := Evaluate(template, values, now); got.State != "unhealthy" {
		t.Fatalf("state=%s", got.State)
	}
	values = append(values, store.Outcome{ID: "b", Type: "github.checks.passed", OccurredAt: now})
	if got := Evaluate(template, values, now); got.State != "healthy" {
		t.Fatalf("state=%s", got.State)
	}
	if got := Evaluate(template, values, now); string(got.Details) != string(Evaluate(template, values, now).Details) {
		t.Fatal("evaluation is not deterministic")
	}
}

func TestCorrectionRate(t *testing.T) {
	now := time.Now().UTC()
	values := []store.Outcome{{Type: "agent.run.completed", OccurredAt: now}, {Type: "human.correction", OccurredAt: now}}
	if got := EvaluateCorrectionRate(values, now); got.State != "unhealthy" {
		t.Fatalf("state=%s", got.State)
	}
}

func TestOperationalMonitorThresholds(t *testing.T) {
	now := time.Now().UTC()
	states := EvaluateOperational(store.RuntimeMetrics{PendingRuns: 2, OldestQueueDelay: 31 * time.Minute, Attempts: 4, RetryAttempts: 2, CompletedRuns: 1, AverageDuration: 20 * time.Minute, ConfiguredSources: 1, StaleSources: 1}, now)
	want := map[string]string{"queue-delay": "unhealthy", "retry-rate": "unhealthy", "run-duration": "warning", "source-freshness": "unhealthy"}
	for _, state := range states {
		if state.State != want[state.MonitorID] {
			t.Errorf("%s=%s", state.MonitorID, state.State)
		}
	}
}
