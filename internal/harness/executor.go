package harness

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Executor makes harness probes deterministic in tests and keeps every real
// probe on the shared process supervisor.
type Executor interface {
	LookPath(string) (string, error)
	Environment() []string
	Run(context.Context, []string, string, ...string) CommandResult
}

type SystemExecutor struct{}

func (SystemExecutor) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (SystemExecutor) Environment() []string {
	return RuntimeEnvironment(os.Environ(),
		"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
		"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN",
	)
}

func (SystemExecutor) Run(ctx context.Context, environment []string, executable string, args ...string) CommandResult {
	var stdout, stderr bytes.Buffer
	result, err := awprocess.Run(ctx, awprocess.Spec{
		Executable:  executable,
		Args:        args,
		Env:         environment,
		GracePeriod: time.Second,
		Sink: func(output awprocess.Output) {
			switch output.Stream {
			case awprocess.Stdout:
				_, _ = stdout.Write(output.Data)
			case awprocess.Stderr:
				_, _ = stderr.Write(output.Data)
			}
		},
	})
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: result.ExitCode, Err: err}
}
