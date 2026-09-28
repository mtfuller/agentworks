package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/harness"
	"github.com/mtfuller/agentworks/internal/harness/runcontext"
	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

type Adapter struct {
	executor             harness.Executor
	projectRoot          string
	agentworksExecutable string
}

var credentialEnvironment = []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}

func New(executor harness.Executor) *Adapter {
	if executor == nil {
		executor = harness.SystemExecutor{}
	}
	return &Adapter{executor: executor}
}

// ForProject creates an adapter for resolved agents from a format-2 project.
func ForProject(projectRoot string) *Adapter {
	return &Adapter{executor: harness.SystemExecutor{}, projectRoot: projectRoot}
}

func (adapter *Adapter) ID() harness.ID { return harness.ClaudeCode }

func (adapter *Adapter) NormalizeLine(line []byte) (harness.Event, bool) { return NormalizeLine(line) }

func (adapter *Adapter) Probe(parent context.Context) harness.ProbeResult {
	result := harness.NewProbeResult(adapter.ID())
	executable, err := adapter.executor.LookPath("claude")
	if err != nil {
		result.Status = harness.ProbeMissing
		result.Diagnostics = append(result.Diagnostics, "claude executable was not found on PATH")
		return result
	}
	result.Executable = executable

	ctx, cancel := harness.ProbeContext(parent)
	defer cancel()
	environment := harness.RuntimeEnvironment(adapter.executor.Environment(), credentialEnvironment...)
	version := adapter.executor.Run(ctx, environment, executable, "--version")
	if version.Err != nil {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, "claude --version failed: "+commandFailure(version))
		return result
	}
	result.Version = strings.TrimSpace(version.Stdout)
	if result.Version == "" {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, "claude --version returned an empty version")
		return result
	}

	help := adapter.executor.Run(ctx, environment, executable, "--help")
	if help.Err != nil || !strings.Contains(help.Stdout, "--print") || !strings.Contains(help.Stdout, "stream-json") {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, "Claude Code does not advertise the required headless stream-json interface")
		return result
	}
	result.Capabilities = append(result.Capabilities, harness.CapabilityHeadless, harness.CapabilityStreaming)
	// Anthropic documents --permission-prompt-tool for non-interactive runs.
	// Some native builds accept it without listing it in --help, so authenticated
	// fixture conformance—not help text—is the final readiness gate.
	result.Capabilities = append(result.Capabilities, harness.CapabilityBoundedApproval)
	if !strings.Contains(help.Stdout, "--permission-prompt-tool") {
		result.Diagnostics = append(result.Diagnostics, "permission broker flag is not advertised by this build; authenticated conformance is still required")
	}

	auth := adapter.executor.Run(ctx, environment, executable, "auth", "status", "--json")
	var status struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.Unmarshal([]byte(auth.Stdout), &status); err != nil {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("Claude authentication status was not valid JSON: %v", err))
		return result
	}
	result.AuthMethod = status.AuthMethod
	if !status.LoggedIn {
		result.Status = harness.ProbeUnauthenticated
		result.Diagnostics = append(result.Diagnostics, "Claude Code is installed but not authenticated")
		return result
	}
	result.Status = harness.ProbeReady
	return result
}

func (adapter *Adapter) Invocation(_ context.Context, request worker.Request) (awprocess.Spec, error) {
	if request.Run.Permission != store.PermissionReadonly {
		return awprocess.Spec{}, errors.New("Claude Code write-capable runs require the AgentWorks MCP permission bridge")
	}
	executable, err := adapter.executor.LookPath("claude")
	if err != nil {
		return awprocess.Spec{}, fmt.Errorf("locate claude executable: %w", err)
	}
	prompt, err := runcontext.Build(adapter.projectRoot, request)
	if err != nil {
		return awprocess.Spec{}, err
	}
	tools := "Read,Glob,Grep"
	environment := harness.RuntimeEnvironment(adapter.executor.Environment(), credentialEnvironment...)
	return awprocess.Spec{
		Executable: executable,
		Args: []string{
			"-p", prompt,
			"--output-format", "stream-json",
			"--verbose",
			"--include-partial-messages",
			"--permission-mode", "dontAsk",
			"--tools", tools,
			"--allowedTools", tools,
			"--strict-mcp-config",
			"--no-session-persistence",
		},
		Env: environment, Filter: harness.RedactEnvironmentSecrets(jsonlFilter(), environment, credentialEnvironment...), GracePeriod: 8 * time.Second,
	}, nil
}

// PrepareInvocation creates the run-local MCP permission server configuration
// required by write-capable Claude Code sessions.
func (adapter *Adapter) PrepareInvocation(ctx context.Context, request worker.Request) (awprocess.Spec, func(), error) {
	if request.Run.Permission == store.PermissionReadonly {
		specification, err := adapter.Invocation(ctx, request)
		return specification, func() {}, err
	}
	if request.PermissionEndpoint == "" || request.PermissionToken == "" {
		return awprocess.Spec{}, func() {}, errors.New("Claude Code write-capable runs require run-scoped permission credentials")
	}
	agentworks, err := adapter.agentworksPath()
	if err != nil {
		return awprocess.Spec{}, func() {}, err
	}
	temporary, err := os.MkdirTemp("", "agentworks-claude-")
	if err != nil {
		return awprocess.Spec{}, func() {}, fmt.Errorf("create Claude run configuration: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	config := map[string]any{"mcpServers": map[string]any{
		"agentworks_permissions": map[string]any{
			"type": "stdio", "command": agentworks, "args": []string{"__permission-mcp"},
			"env": map[string]string{
				"AGENTWORKS_PERMISSION_ENDPOINT": request.PermissionEndpoint,
				"AGENTWORKS_PERMISSION_TOKEN":    request.PermissionToken,
			},
		},
	}}
	data, _ := json.Marshal(config)
	configPath := filepath.Join(temporary, "mcp.json")
	if err := os.WriteFile(configPath, append(data, '\n'), 0o600); err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, fmt.Errorf("write Claude run configuration: %w", err)
	}
	executable, err := adapter.executor.LookPath("claude")
	if err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, fmt.Errorf("locate claude executable: %w", err)
	}
	prompt, err := runcontext.Build(adapter.projectRoot, request)
	if err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, err
	}
	tools := "Read,Glob,Grep,Write,Edit,Bash"
	environment := harness.RuntimeEnvironment(adapter.executor.Environment(), credentialEnvironment...)
	return awprocess.Spec{
		Executable: executable,
		Args: []string{
			"-p", prompt, "--output-format", "stream-json", "--verbose", "--include-partial-messages",
			"--permission-mode", "manual", "--permission-prompts", "host",
			"--permission-prompt-tool", "mcp__agentworks_permissions__approve",
			"--tools", tools, "--allowedTools", "Read,Glob,Grep",
			"--mcp-config", configPath, "--strict-mcp-config", "--restricted", "--no-session-persistence",
		},
		Env: environment, Filter: harness.RedactEnvironmentSecrets(jsonlFilter(), environment, credentialEnvironment...), GracePeriod: 8 * time.Second,
	}, cleanup, nil
}

func (adapter *Adapter) agentworksPath() (string, error) {
	if adapter.agentworksExecutable != "" {
		return adapter.agentworksExecutable, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate AgentWorks executable: %w", err)
	}
	return path, nil
}

func commandFailure(result harness.CommandResult) string {
	message := strings.TrimSpace(result.Stderr)
	if message == "" {
		message = strings.TrimSpace(result.Stdout)
	}
	if message == "" && result.Err != nil {
		message = result.Err.Error()
	}
	return message
}
