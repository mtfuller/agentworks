package cmd

import (
	"context"
	"testing"

	"github.com/mtfuller/agentworks/internal/harness"
)

func TestProbeHarnessesIsConcurrentAndDeterministic(t *testing.T) {
	results := probeHarnesses(context.Background(), []harness.Adapter{
		fakeHarnessAdapter{id: harness.GitHubCopilot, status: harness.ProbeMissing},
		fakeHarnessAdapter{id: harness.ClaudeCode, status: harness.ProbeReady},
	})
	if len(results) != 2 || results[0].Harness != harness.ClaudeCode || results[1].Harness != harness.GitHubCopilot {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Status != harness.ProbeReady || results[1].Status != harness.ProbeMissing {
		t.Fatalf("results = %#v", results)
	}
}

func TestPrintHarnesses(t *testing.T) {
	result := runCLIRaw(t, "harnesses", "--json")
	if result.err != nil {
		t.Fatalf("harnesses --json failed: %v\n%s", result.err, result.combined())
	}
	if result.stdout == "" {
		t.Fatal("harnesses --json produced no output")
	}
}

type fakeHarnessAdapter struct {
	id     harness.ID
	status harness.ProbeStatus
}

func (adapter fakeHarnessAdapter) ID() harness.ID { return adapter.id }
func (adapter fakeHarnessAdapter) Probe(context.Context) harness.ProbeResult {
	result := harness.NewProbeResult(adapter.id)
	result.Status = adapter.status
	return result
}
