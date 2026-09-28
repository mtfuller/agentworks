package githubcopilot

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

var credentialEnvironment = []string{
	"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
	"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN",
}

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

// PrepareInvocation creates an ephemeral plugin containing only the
// permissionRequest hook for this run.
func (adapter *Adapter) PrepareInvocation(ctx context.Context, request worker.Request) (awprocess.Spec, func(), error) {
	if request.Run.Permission == store.PermissionReadonly {
		specification, err := adapter.Invocation(ctx, request)
		return specification, func() {}, err
	}
	if request.PermissionEndpoint == "" || request.PermissionToken == "" {
		return awprocess.Spec{}, func() {}, errors.New("GitHub Copilot write-capable runs require run-scoped permission credentials")
	}
	agentworks, err := adapter.agentworksPath()
	if err != nil {
		return awprocess.Spec{}, func() {}, err
	}
	temporary, err := os.MkdirTemp("", "agentworks-copilot-")
	if err != nil {
		return awprocess.Spec{}, func() {}, fmt.Errorf("create Copilot run plugin: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	manifest := map[string]any{
		"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
		"name":    "agentworks-run-permissions", "version": "1.0.0",
	}
	hooks := map[string]any{"version": 1, "hooks": map[string]any{
		"permissionRequest": []any{map[string]any{
			"type": "command", "exec": agentworks, "args": []string{"__permission-hook"}, "timeoutSec": 86400,
			"env": map[string]string{
				"AGENTWORKS_PERMISSION_ENDPOINT": request.PermissionEndpoint,
				"AGENTWORKS_PERMISSION_TOKEN":    request.PermissionToken,
			},
		}},
	}}
	if err := writeJSON(filepath.Join(temporary, "plugin.json"), manifest); err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, err
	}
	if err := writeJSON(filepath.Join(temporary, "com.github.copilot", "hooks", "hooks.json"), hooks); err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, err
	}
	executable, err := adapter.executor.LookPath("copilot")
	if err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, fmt.Errorf("locate copilot executable: %w", err)
	}
	prompt, err := runcontext.Build(adapter.projectRoot, request)
	if err != nil {
		cleanup()
		return awprocess.Spec{}, func() {}, err
	}
	args := []string{
		"-p", prompt,
		"--available-tools=view", "--available-tools=grep", "--available-tools=glob",
		"--available-tools=create", "--available-tools=edit", "--available-tools=bash",
		"--allow-tool=view", "--allow-tool=rg", "--allow-tool=glob",
		"--plugin-dir", temporary, "--output-format=json", "--stream=on", "--disable-builtin-mcps",
		"--no-custom-instructions", "--no-auto-update", "--no-remote-export", "--no-ask-user", "--no-color",
		"--secret-env-vars=AGENTWORKS_PERMISSION_TOKEN,ANTHROPIC_API_KEY,CLAUDE_CODE_OAUTH_TOKEN,COPILOT_GITHUB_TOKEN,GH_TOKEN,GITHUB_TOKEN",
	}
	environment := harness.RuntimeEnvironment(adapter.executor.Environment(), credentialEnvironment...)
	return awprocess.Spec{
		Executable: executable, Args: args, Env: environment,
		Filter: harness.RedactEnvironmentSecrets(jsonlFilter(), environment, credentialEnvironment...), GracePeriod: 8 * time.Second,
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

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Copilot run plugin: %w", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Copilot run plugin: %w", err)
	}
	return nil
}

func (adapter *Adapter) ID() harness.ID { return harness.GitHubCopilot }

func (adapter *Adapter) NormalizeLine(line []byte) (harness.Event, bool) { return NormalizeLine(line) }

func (adapter *Adapter) Probe(parent context.Context) harness.ProbeResult {
	result := harness.NewProbeResult(adapter.ID())
	executable, err := adapter.executor.LookPath("copilot")
	if err != nil {
		result.Status = harness.ProbeMissing
		result.Diagnostics = append(result.Diagnostics, "copilot executable was not found on PATH")
		return result
	}
	result.Executable = executable

	ctx, cancel := harness.ProbeContext(parent)
	defer cancel()
	environment := harness.RuntimeEnvironment(adapter.executor.Environment(), credentialEnvironment...)
	version := adapter.executor.Run(ctx, environment, executable, "--version")
	if version.Err != nil {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("copilot --version failed: %v", version.Err))
		return result
	}
	result.Version = strings.TrimSpace(version.Stdout)
	if result.Version == "" {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, "copilot --version returned an empty version")
		return result
	}
	help := adapter.executor.Run(ctx, environment, executable, "--help")
	if help.Err != nil || !strings.Contains(help.Stdout, "--prompt") || !strings.Contains(help.Stdout, "--stream") {
		result.Status = harness.ProbeIncompatible
		result.Diagnostics = append(result.Diagnostics, "Copilot CLI does not advertise its required programmatic prompt interface")
		return result
	}
	result.Capabilities = append(result.Capabilities,
		harness.CapabilityHeadless,
		harness.CapabilityStreaming,
		harness.CapabilityBoundedApproval,
	)

	if method := environmentAuthMethod(environment); method != "" {
		result.AuthMethod = method
		result.Status = harness.ProbeReady
		return result
	}
	result.Status = harness.ProbeAuthUnverified
	result.Diagnostics = append(result.Diagnostics,
		"no Copilot token environment variable is set; secure-store authentication is verified when the first run starts",
	)
	return result
}

// Invocation implements worker.Adapter for the first fail-closed production
// slice. Copilot requires automatic tool permission in prompt mode, so the
// available tool set is reduced to named read operations before that flag is
// enabled.
func (adapter *Adapter) Invocation(_ context.Context, request worker.Request) (awprocess.Spec, error) {
	if request.Run.Permission != store.PermissionReadonly {
		return awprocess.Spec{}, errors.New("GitHub Copilot write-capable runs require the AgentWorks permissionRequest hook bridge")
	}
	executable, err := adapter.executor.LookPath("copilot")
	if err != nil {
		return awprocess.Spec{}, fmt.Errorf("locate copilot executable: %w", err)
	}
	prompt, err := runcontext.Build(adapter.projectRoot, request)
	if err != nil {
		return awprocess.Spec{}, err
	}
	args := []string{
		"-p", prompt,
		"--allow-all-tools",
		"--available-tools=view",
		"--available-tools=rg",
		"--available-tools=glob",
		"--output-format=json",
		"--stream=on",
		"--disable-builtin-mcps",
		"--no-custom-instructions",
		"--no-auto-update",
		"--no-remote-export",
		"--no-ask-user",
		"--no-color",
		"--secret-env-vars=ANTHROPIC_API_KEY,CLAUDE_CODE_OAUTH_TOKEN,COPILOT_GITHUB_TOKEN,GH_TOKEN,GITHUB_TOKEN",
	}
	environment := harness.RuntimeEnvironment(adapter.executor.Environment(), credentialEnvironment...)
	return awprocess.Spec{
		Executable:  executable,
		Args:        args,
		Env:         environment,
		Filter:      harness.RedactEnvironmentSecrets(jsonlFilter(), environment, credentialEnvironment...),
		GracePeriod: 8 * time.Second,
	}, nil
}

func environmentAuthMethod(environment []string) string {
	values := map[string]string{}
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if values[name] != "" {
			return name
		}
	}
	return ""
}
