package harness

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecutorHelper(t *testing.T) {
	if os.Getenv("CI") != "agentworks-executor-test" {
		return
	}
	fmt.Fprint(os.Stdout, "probe output")
	fmt.Fprint(os.Stderr, "probe diagnostic")
	os.Exit(4)
}

func TestSystemExecutorCapturesCommandResult(t *testing.T) {
	t.Setenv("CI", "agentworks-executor-test")
	executor := SystemExecutor{}
	result := executor.Run(context.Background(), RuntimeEnvironment(os.Environ()), os.Args[0], "-test.run=TestExecutorHelper")
	if result.ExitCode != 4 || result.Err == nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Stdout != "probe output" || !strings.HasPrefix(result.Stderr, "probe diagnostic") {
		t.Errorf("result streams = %#v", result)
	}
	if len(executor.Environment()) == 0 {
		t.Error("Environment() returned no host environment")
	}
}

func TestSystemExecutorLookPath(t *testing.T) {
	path, err := (SystemExecutor{}).LookPath("go")
	if err != nil || !strings.Contains(path, "go") {
		t.Fatalf("LookPath(go) = %q, %v", path, err)
	}
}

func TestProbeHelpers(t *testing.T) {
	result := NewProbeResult(ClaudeCode)
	if result.Harness != ClaudeCode || result.Capabilities == nil || result.Diagnostics == nil {
		t.Fatalf("NewProbeResult() = %#v", result)
	}
	ctx, cancel := ProbeContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > ProbeTimeout {
		t.Errorf("ProbeContext deadline = %v, %v", deadline, ok)
	}
}
