package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/harness"
	"github.com/mtfuller/agentworks/internal/harness/claudecode"
	"github.com/mtfuller/agentworks/internal/harness/githubcopilot"
)

// This test is deliberately opt-in. It verifies that both production CLIs are
// installed, compatible, and authenticated without submitting a model prompt.
func TestLiveHarnessAuthentication(t *testing.T) {
	if os.Getenv("AGENTWORKS_RUNTIME_CONFORMANCE") != "1" {
		t.Skip("set AGENTWORKS_RUNTIME_CONFORMANCE=1 for live harness authentication checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, adapter := range []harness.Adapter{claudecode.New(nil), githubcopilot.New(nil)} {
		result := adapter.Probe(ctx)
		if result.Status != harness.ProbeReady {
			t.Errorf("%s readiness=%s version=%q diagnostics=%v", result.Harness, result.Status, result.Version, result.Diagnostics)
		}
	}
}
