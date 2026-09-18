package mcpclient

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/mtfuller/agentworks/internal/version"
)

// killGrace is how long Process.Close waits for the server to exit on its
// own (via stdin EOF) before force-killing it.
const killGrace = 3 * time.Second

// Process is a Client bound to a real child process -- a tool artifact's
// declared "command", run the same way cmd/test.go and
// internal/targets/mcpconfig.ServerFor already do (via "sh -c"), except
// here it's actually started and spoken to rather than just described in
// generated config.
type Process struct {
	*Client
	cmd *exec.Cmd
}

// StartProcess launches "sh -c command" in dir with env as its process
// environment (nil inherits the caller's, matching os/exec.Cmd.Env), wires
// its stdin/stdout into a new Client and its stderr into Client.OnStderr,
// and starts it. The command isn't running an MCP server until the
// caller calls Initialize on the result.
func StartProcess(command, dir string, env []string) (*Process, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: wiring stdin for %q: %w", command, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: wiring stdout for %q: %w", command, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: wiring stderr for %q: %w", command, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: starting %q: %w", command, err)
	}

	p := &Process{Client: New(stdin, stdout), cmd: cmd}
	go p.pipeStderr(stderr)
	return p, nil
}

func (p *Process) pipeStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if p.OnStderr != nil {
			p.OnStderr(scanner.Text())
		}
	}
}

// Probe starts command as an MCP server, performs the initialize handshake,
// lists its tools, and shuts it down -- the smallest end-to-end proof that a
// server actually speaks MCP, used by `agentworks test` for mcp artifacts
// that don't declare their own `test:`. Stderr is forwarded to onStderr if
// non-nil so a failure isn't opaque.
func Probe(ctx context.Context, command, dir string, env []string, onStderr func(string)) (InitializeResult, []Tool, error) {
	p, err := StartProcess(command, dir, env)
	if err != nil {
		return InitializeResult{}, nil, err
	}
	p.OnStderr = onStderr
	defer p.Close()

	info, err := p.Initialize(ctx, "agentworks", version.GetShortVersion())
	if err != nil {
		return InitializeResult{}, nil, fmt.Errorf("initialize: %w", err)
	}
	tools, err := p.ListTools(ctx)
	if err != nil {
		return info, nil, fmt.Errorf("tools/list: %w", err)
	}
	return info, tools, nil
}

// Close closes the client (see Client.Close -- this closes stdin, the
// stdio transport's own shutdown signal) and waits up to killGrace for the
// process to exit, killing it if it hasn't by then.
func (p *Process) Close() error {
	clientErr := p.Client.Close()

	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case waitErr := <-done:
		if clientErr != nil {
			return clientErr
		}
		return waitErr
	case <-time.After(killGrace):
		_ = p.cmd.Process.Kill()
		<-done
		if clientErr != nil {
			return clientErr
		}
		return fmt.Errorf("mcp: %q did not exit within %s after stdin closed; killed it", p.cmd.Path, killGrace)
	}
}
