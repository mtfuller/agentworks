package githubcopilot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/harness"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

func TestProbeStates(t *testing.T) {
	base := fakeExecutor{
		path: "/usr/local/bin/copilot",
		env:  []string{"PATH=/usr/bin"},
		results: map[string]harness.CommandResult{
			"--version": {Stdout: "1.0.0\n"},
			"--help":    {Stdout: "-p, --prompt --stream --allow-tool"},
		},
	}
	tests := []struct {
		name       string
		executor   fakeExecutor
		wantStatus harness.ProbeStatus
		wantDiag   string
	}{
		{name: "token ready", executor: base.withEnv("GITHUB_TOKEN=token"), wantStatus: harness.ProbeReady},
		{name: "missing", executor: fakeExecutor{lookErr: errors.New("not found")}, wantStatus: harness.ProbeMissing, wantDiag: "not found"},
		{name: "no verifiable auth", executor: base, wantStatus: harness.ProbeAuthUnverified, wantDiag: "secure-store authentication"},
		{name: "old interface", executor: base.with("--help", harness.CommandResult{Stdout: "interactive only"}), wantStatus: harness.ProbeIncompatible, wantDiag: "programmatic prompt"},
		{name: "empty version", executor: base.with("--version", harness.CommandResult{}), wantStatus: harness.ProbeIncompatible, wantDiag: "empty version"},
		{name: "version failure", executor: base.with("--version", harness.CommandResult{Err: errors.New("broken")}), wantStatus: harness.ProbeIncompatible, wantDiag: "version failed"},
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

func TestEnvironmentAuthPrecedence(t *testing.T) {
	got := environmentAuthMethod([]string{
		"GITHUB_TOKEN=github",
		"GH_TOKEN=gh",
		"COPILOT_GITHUB_TOKEN=copilot",
	})
	if got != "COPILOT_GITHUB_TOKEN" {
		t.Errorf("environmentAuthMethod() = %q", got)
	}
}

func TestAdapterConstructorsAndPreparationFailures(t *testing.T) {
	if adapter := New(nil); adapter == nil || adapter.executor == nil {
		t.Fatal("New(nil) did not install the system executor")
	}
	root := invocationProject(t)
	if adapter := ForProject(root); adapter.projectRoot != root {
		t.Fatalf("ForProject root = %q", adapter.projectRoot)
	}
	adapter := &Adapter{executor: fakeExecutor{path: "/bin/copilot"}, projectRoot: root}
	request := worker.Request{
		Run:   store.Run{Team: "delivery", Agent: "reader", Permission: store.PermissionReadwrite},
		Event: store.Event{Data: json.RawMessage(`{"prompt":"write"}`)},
	}
	if _, _, err := adapter.PrepareInvocation(context.Background(), request); err == nil || !strings.Contains(err.Error(), "permission credentials") {
		t.Fatalf("missing credentials error = %v", err)
	}
	path, err := adapter.agentworksPath()
	if err != nil || path == "" {
		t.Fatalf("agentworksPath() = %q, %v", path, err)
	}
	request.Run.Permission = store.PermissionReadonly
	if _, cleanup, err := adapter.PrepareInvocation(context.Background(), request); err != nil {
		t.Fatal(err)
	} else {
		cleanup()
	}
	if event, ok := adapter.NormalizeLine([]byte(`{"type":"result","exitCode":0}`)); !ok || event.Kind != harness.EventCompleted {
		t.Fatalf("adapter normalization = %#v, %v", event, ok)
	}
	broken := &Adapter{
		executor: fakeExecutor{lookErr: errors.New("missing")}, projectRoot: root,
		agentworksExecutable: "/bin/agentworks",
	}
	request.Run.Permission = store.PermissionReadwrite
	request.PermissionEndpoint = "http://127.0.0.1:1/v1/authorize"
	request.PermissionToken = "token"
	if _, _, err := broken.PrepareInvocation(context.Background(), request); err == nil || !strings.Contains(err.Error(), "locate copilot") {
		t.Fatalf("missing Copilot error = %v", err)
	}
}

func TestWriteJSONReportsFilesystemErrors(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(parent, "child.json"), map[string]any{}); err == nil {
		t.Fatal("writeJSON accepted a file as its parent directory")
	}
}

func TestReadonlyInvocationIsExplicitlyRestricted(t *testing.T) {
	root := invocationProject(t)
	adapter := &Adapter{executor: fakeExecutor{path: "/bin/copilot", env: []string{"PATH=/bin", "AWS_SECRET_ACCESS_KEY=secret"}}, projectRoot: root}
	request := worker.Request{
		Run:   store.Run{Team: "delivery", Agent: "reader", Permission: store.PermissionReadonly},
		Event: store.Event{Data: json.RawMessage(`{"prompt":"Inspect this folder"}`)},
	}
	specification, err := adapter.Invocation(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(specification.Args, " ")
	for _, required := range []string{"--allow-all-tools", "--available-tools=view", "--available-tools=rg", "--available-tools=glob", "--output-format=json"} {
		if !strings.Contains(joined, required) {
			t.Errorf("args do not contain %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"--allow-all-paths", "--allow-all-urls", "--yolo"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("args unexpectedly contain %q: %s", forbidden, joined)
		}
	}
	if specification.Filter == nil || len(specification.Env) != 1 {
		t.Errorf("invocation is missing output filtering or exact environment: %#v", specification)
	}
	if containsEnvironment(specification.Env, "AWS_SECRET_ACCESS_KEY=secret") {
		t.Errorf("invocation inherited an unsafe environment: %#v", specification.Env)
	}
	request.Run.Permission = store.PermissionReadwrite
	if _, err := adapter.Invocation(context.Background(), request); err == nil || !strings.Contains(err.Error(), "permissionRequest") {
		t.Fatalf("write-capable Invocation() error = %v", err)
	}
}

func TestWriteInvocationUsesEphemeralPermissionHook(t *testing.T) {
	root := invocationProject(t)
	adapter := &Adapter{
		executor:    fakeExecutor{path: "/bin/copilot", env: []string{"PATH=/bin", "AGENTWORKS_PERMISSION_TOKEN=old"}},
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
	for _, required := range []string{"--plugin-dir", "--allow-tool=view", "--available-tools=edit", "--no-ask-user"} {
		if !strings.Contains(joined, required) {
			t.Errorf("args do not contain %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, "--allow-all-tools") || strings.Contains(joined, "run-secret") {
		t.Fatalf("write invocation is overbroad or leaked its token: %s", joined)
	}
	pluginDir := argumentAfter(t, specification.Args, "--plugin-dir")
	data, err := os.ReadFile(filepath.Join(pluginDir, "com.github.copilot", "hooks", "hooks.json"))
	if err != nil || !strings.Contains(string(data), `"permissionRequest"`) || !strings.Contains(string(data), `"exec":"/bin/agentworks"`) || !strings.Contains(string(data), "run-secret") {
		t.Fatalf("hooks config = %s err=%v", data, err)
	}
	if containsEnvironment(specification.Env, "AGENTWORKS_PERMISSION_TOKEN=run-secret") {
		t.Fatalf("run token leaked into the Copilot process environment: %#v", specification.Env)
	}
	cleanup()
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Fatalf("temporary plugin was not removed: %v", err)
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

func (executor fakeExecutor) withEnv(entries ...string) fakeExecutor {
	copy := executor
	copy.env = append(append([]string(nil), executor.env...), entries...)
	return copy
}
