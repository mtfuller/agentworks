package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/harness"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

func TestProbeStates(t *testing.T) {
	base := fakeExecutor{
		path: "/usr/local/bin/claude",
		results: map[string]harness.CommandResult{
			"--version":          {Stdout: "2.1.220 (Claude Code)\n", ExitCode: 0},
			"--help":             {Stdout: "--print --output-format stream-json", ExitCode: 0},
			"auth status --json": {Stdout: `{"loggedIn":true,"authMethod":"oauth"}`, ExitCode: 0},
		},
	}
	tests := []struct {
		name       string
		executor   fakeExecutor
		wantStatus harness.ProbeStatus
		wantDiag   string
	}{
		{name: "ready", executor: base, wantStatus: harness.ProbeReady, wantDiag: "permission broker flag"},
		{name: "missing", executor: fakeExecutor{lookErr: errors.New("not found")}, wantStatus: harness.ProbeMissing, wantDiag: "not found"},
		{name: "unauthenticated", executor: base.with("auth status --json", harness.CommandResult{Stdout: `{"loggedIn":false,"authMethod":"none"}`, ExitCode: 1, Err: errors.New("exit 1")}), wantStatus: harness.ProbeUnauthenticated, wantDiag: "not authenticated"},
		{name: "old interface", executor: base.with("--help", harness.CommandResult{Stdout: "--print text only"}), wantStatus: harness.ProbeIncompatible, wantDiag: "stream-json"},
		{name: "empty version", executor: base.with("--version", harness.CommandResult{}), wantStatus: harness.ProbeIncompatible, wantDiag: "empty version"},
		{name: "bad auth response", executor: base.with("auth status --json", harness.CommandResult{Stdout: "not-json"}), wantStatus: harness.ProbeIncompatible, wantDiag: "not valid JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := New(test.executor).Probe(context.Background())
			if result.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q (%#v)", result.Status, test.wantStatus, result)
			}
			if !strings.Contains(strings.Join(result.Diagnostics, " "), test.wantDiag) {
				t.Errorf("diagnostics = %#v, want %q", result.Diagnostics, test.wantDiag)
			}
			if result.Capabilities == nil || result.Diagnostics == nil {
				t.Errorf("probe slices must not be nil: %#v", result)
			}
		})
	}
}

func TestProbeReportsAdvertisedPermissionBrokerWithoutWarning(t *testing.T) {
	executor := fakeExecutor{
		path: "/usr/local/bin/claude",
		results: map[string]harness.CommandResult{
			"--version":          {Stdout: "2.1.220"},
			"--help":             {Stdout: "--print stream-json --permission-prompt-tool"},
			"auth status --json": {Stdout: `{"loggedIn":true,"authMethod":"apiKey"}`},
		},
	}
	result := New(executor).Probe(context.Background())
	if result.Status != harness.ProbeReady || len(result.Diagnostics) != 0 || result.AuthMethod != "apiKey" {
		t.Fatalf("result = %#v", result)
	}
	want := []harness.Capability{harness.CapabilityHeadless, harness.CapabilityStreaming, harness.CapabilityBoundedApproval}
	if !reflect.DeepEqual(result.Capabilities, want) {
		t.Errorf("capabilities = %#v, want %#v", result.Capabilities, want)
	}
}

func TestReadonlyInvocationUsesDenyByDefaultTools(t *testing.T) {
	root := invocationProject(t)
	adapter := &Adapter{executor: fakeExecutor{path: "/bin/claude", env: []string{"PATH=/bin", "DATABASE_URL=secret"}}, projectRoot: root}
	request := worker.Request{
		Run:   store.Run{Team: "delivery", Agent: "reader", Permission: store.PermissionReadonly},
		Event: store.Event{Data: json.RawMessage(`{"prompt":"Inspect this folder"}`)},
	}
	specification, err := adapter.Invocation(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(specification.Args, " ")
	for _, required := range []string{"--output-format stream-json", "--permission-mode dontAsk", "--tools Read,Glob,Grep", "--strict-mcp-config", "--no-session-persistence"} {
		if !strings.Contains(joined, required) {
			t.Errorf("args do not contain %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"Edit", "Write", "Bash", "bypassPermissions"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("args unexpectedly contain %q: %s", forbidden, joined)
		}
	}
	if specification.Filter == nil {
		t.Error("invocation is missing the stream-json privacy filter")
	}
	if !containsEnvironment(specification.Env, "PATH=/bin") || containsEnvironment(specification.Env, "DATABASE_URL=secret") {
		t.Errorf("invocation inherited an unsafe environment: %#v", specification.Env)
	}
	request.Run.Permission = store.PermissionReadwrite
	if _, err := adapter.Invocation(context.Background(), request); err == nil || !strings.Contains(err.Error(), "MCP permission bridge") {
		t.Fatalf("write-capable Invocation() error = %v", err)
	}
}

func TestWriteInvocationUsesEphemeralPermissionMCP(t *testing.T) {
	root := invocationProject(t)
	adapter := &Adapter{
		executor:    fakeExecutor{path: "/bin/claude", env: []string{"PATH=/bin", "AGENTWORKS_PERMISSION_TOKEN=old"}},
		projectRoot: root, agentworksExecutable: "/bin/agentworks",
	}
	request := worker.Request{
		Run:                store.Run{Team: "delivery", Agent: "reader", Permission: store.PermissionReadwrite},
		Event:              store.Event{Data: json.RawMessage(`{"prompt":"Edit this folder"}`)},
		PermissionEndpoint: "http://127.0.0.1:1234/v1/authorize", PermissionToken: "run-secret",
	}
	specification, cleanup, err := adapter.PrepareInvocation(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(specification.Args, " ")
	for _, required := range []string{"--permission-mode manual", "--permission-prompts host", "--permission-prompt-tool mcp__agentworks_permissions__approve", "--restricted"} {
		if !strings.Contains(joined, required) {
			t.Errorf("args do not contain %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, "run-secret") {
		t.Fatal("permission token leaked into argv")
	}
	configPath := argumentAfter(t, specification.Args, "--mcp-config")
	data, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(data), `"command":"/bin/agentworks"`) || !strings.Contains(string(data), "run-secret") {
		t.Fatalf("MCP config = %s err=%v", data, err)
	}
	if containsEnvironment(specification.Env, "AGENTWORKS_PERMISSION_TOKEN=run-secret") {
		t.Fatalf("run token leaked into the Claude process environment: %#v", specification.Env)
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
		t.Fatalf("temporary configuration was not removed: %v", err)
	}
}

func argumentAfter(t *testing.T, arguments []string, flag string) string {
	t.Helper()
	for index, argument := range arguments {
		if argument == flag && index+1 < len(arguments) {
			return arguments[index+1]
		}
	}
	t.Fatalf("argument %q not found in %#v", flag, arguments)
	return ""
}

func containsEnvironment(environment []string, wanted string) bool {
	for _, entry := range environment {
		if entry == wanted {
			return true
		}
	}
	return false
}

func invocationProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		filepath.Join("agents", "reader", spec.AgentFile): "---\nname: reader\n---\nInspect carefully.",
		filepath.Join("teams", "delivery", spec.TeamFile): "name: delivery\nagents: [reader]\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type fakeExecutor struct {
	path    string
	lookErr error
	env     []string
	results map[string]harness.CommandResult
}

func (executor fakeExecutor) LookPath(string) (string, error) { return executor.path, executor.lookErr }
func (executor fakeExecutor) Environment() []string           { return executor.env }
func (executor fakeExecutor) Run(_ context.Context, _ []string, _ string, args ...string) harness.CommandResult {
	return executor.results[strings.Join(args, " ")]
}

func (executor fakeExecutor) with(command string, result harness.CommandResult) fakeExecutor {
	copy := executor
	copy.results = make(map[string]harness.CommandResult, len(executor.results))
	for key, value := range executor.results {
		copy.results[key] = value
	}
	copy.results[command] = result
	return copy
}
