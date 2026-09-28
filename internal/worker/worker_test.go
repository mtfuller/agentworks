package worker

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/approval"
	"github.com/mtfuller/agentworks/internal/harness"
	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/store"
)

const workerHelperEnvironment = "AGENTWORKS_WORKER_TEST_HELPER"

func TestWorkerHarnessHelper(t *testing.T) {
	if os.Getenv(workerHelperEnvironment) != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(90)
	}
	action := os.Args[separator+1]
	switch action {
	case "success":
		fmt.Fprint(os.Stdout, "structured output")
		fmt.Fprint(os.Stderr, "diagnostic output")
		if err := os.WriteFile("result.txt", []byte("created by fake harness"), 0o644); err != nil {
			os.Exit(91)
		}
	case "fail":
		fmt.Fprint(os.Stderr, "expected failure")
		os.Exit(7)
	case "slow":
		time.Sleep(180 * time.Millisecond)
		fmt.Fprint(os.Stdout, "finished slowly")
	case "hang":
		if err := os.WriteFile("ready", []byte("ready"), 0o644); err != nil {
			os.Exit(92)
		}
		for {
			time.Sleep(time.Second)
		}
	default:
		os.Exit(93)
	}
	os.Exit(0)
}

func TestWorkerExecutesFakeHarnessAndPersistsLog(t *testing.T) {
	fixture := newWorkerFixture(t, "success")
	found, err := fixture.worker.RunOnce(context.Background())
	if err != nil || !found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	run, err := fixture.store.GetRun(context.Background(), fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != store.RunSucceeded || run.Conclusion != "harness completed" {
		t.Fatalf("run = %#v", run)
	}
	data, err := os.ReadFile(filepath.Join(fixture.workspace, "result.txt"))
	if err != nil || string(data) != "created by fake harness" {
		t.Fatalf("workspace result = %q, %v", data, err)
	}
	records := readLogRecords(t, fixture.logRoot)
	if len(records) != 2 || decodeRecord(t, records[0])+decodeRecord(t, records[1]) != "structured outputdiagnostic output" && decodeRecord(t, records[0])+decodeRecord(t, records[1]) != "diagnostic outputstructured output" {
		t.Fatalf("log records = %#v", records)
	}
	events, err := fixture.store.ListRuntimeEventsAfter(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var outputEvents int
	for _, event := range events {
		if event.Topic == "run.output" {
			outputEvents++
			if strings.Contains(string(event.Data), "structured output") || strings.Contains(string(event.Data), "diagnostic output") {
				t.Fatalf("runtime event persisted raw output: %s", event.Data)
			}
		}
	}
	if outputEvents != 2 {
		t.Fatalf("output event count = %d, want 2; events=%#v", outputEvents, events)
	}
	attempts, err := fixture.store.ListAttempts(context.Background(), fixture.runID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
	var result struct {
		Changes struct {
			Added []string `json:"added"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(attempts[0].Result, &result); err != nil {
		t.Fatal(err)
	}
	excerpts := fixture.worker.transcriptExcerpts(context.Background(), []store.TimelineEntry{{Kind: "run", ID: fixture.runID}})
	if len(excerpts) != 1 || !strings.Contains(excerpts[0], "structured output") {
		t.Fatalf("transcript excerpts=%#v", excerpts)
	}
	if len(result.Changes.Added) != 1 || result.Changes.Added[0] != "result.txt" {
		t.Fatalf("changes = %#v", result.Changes)
	}
}

func TestWorkerPublishesNormalizedHarnessEventsWithoutRawPayloads(t *testing.T) {
	fixture := newWorkerFixture(t, "success")
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "agentworks.yaml"), []byte("format: 2\nname: test-project\nteams: [engineering]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "teams", "engineering"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "teams", "engineering", "team.yaml"), []byte("name: engineering\nagents: [implementer]\ndefault_agent: implementer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "agents", "implementer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "memory", "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "agents", "implementer", "AGENT.md"), []byte("---\nname: implementer\nmemory: memory/agents/implementer.md\n---\nImplement."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "memory", "agents", "implementer.md"), []byte("# Memory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.worker.config.ProjectRoot = projectRoot
	fixture.worker.config.Adapters = map[string]Adapter{"fake": normalizingAdapter{fakeAdapter{action: "success"}}}
	if found, err := fixture.worker.RunOnce(context.Background()); err != nil || !found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	events, err := fixture.store.ListRuntimeEventsAfter(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Topic == "harness.event" {
			if strings.Contains(string(event.Data), "structured output") || !strings.Contains(string(event.Data), `"kind":"text"`) {
				t.Fatalf("normalized event = %s", event.Data)
			}
			conclusion, err := fixture.store.GetRunConclusion(context.Background(), fixture.runID)
			if err != nil || !conclusion.Structured || len(conclusion.Conclusion.Completed) != 1 {
				t.Fatalf("structured conclusion = %#v, %v", conclusion, err)
			}
			proposals, err := fixture.store.ListMemoryProposals(context.Background(), fixture.runID)
			if err != nil || len(proposals) != 1 {
				t.Fatalf("memory proposals=%#v err=%v", proposals, err)
			}
			return
		}
	}
	t.Fatal("normalized harness event was not published")
}

func TestWorkerPassesRunScopedGatewayCredentialsToPreparedAdapter(t *testing.T) {
	fixture := newWorkerFixture(t, "success")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gateway, err := approval.StartGateway(ctx, fixture.store, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &credentialAdapter{fakeAdapter: fakeAdapter{action: "success"}}
	fixture.worker.config.ApprovalGateway = gateway
	fixture.worker.config.Adapters = map[string]Adapter{"fake": adapter}
	if found, err := fixture.worker.RunOnce(ctx); err != nil || !found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	if adapter.endpoint == "" || adapter.token == "" {
		t.Fatalf("permission credentials were not passed: %#v", adapter)
	}
}

func TestWorkerPersistsHarnessFailure(t *testing.T) {
	fixture := newWorkerFixture(t, "fail")
	found, err := fixture.worker.RunOnce(context.Background())
	if err != nil || !found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	run, err := fixture.store.GetRun(context.Background(), fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != store.RunFailed || run.Conclusion != "harness exited with code 7" {
		t.Fatalf("run = %#v", run)
	}
}

func TestWorkerSchedulesAndExecutesFreshRetryAttempt(t *testing.T) {
	fixture := newWorkerFixture(t, "retry")
	if found, err := fixture.worker.RunOnce(context.Background()); err != nil || !found {
		t.Fatalf("first RunOnce() found=%v err=%v", found, err)
	}
	run, err := fixture.store.GetRun(context.Background(), fixture.runID)
	if err != nil || run.State != store.RunRetryScheduled || run.Conclusion != "test transient failure" {
		t.Fatalf("scheduled run = %#v err=%v", run, err)
	}
	time.Sleep(30 * time.Millisecond)
	if found, err := fixture.worker.RunOnce(context.Background()); err != nil || !found {
		t.Fatalf("second RunOnce() found=%v err=%v", found, err)
	}
	run, err = fixture.store.GetRun(context.Background(), fixture.runID)
	if err != nil || run.State != store.RunSucceeded {
		t.Fatalf("retried run = %#v err=%v", run, err)
	}
	attempts, err := fixture.store.ListAttempts(context.Background(), fixture.runID)
	if err != nil || len(attempts) != 2 || attempts[0].State != store.RunFailed || attempts[1].State != store.RunSucceeded {
		t.Fatalf("attempts = %#v err=%v", attempts, err)
	}
}

func TestWorkerHeartbeatsLongRun(t *testing.T) {
	fixture := newWorkerFixtureWithTiming(t, "slow", 60*time.Millisecond, 10*time.Millisecond)
	found, err := fixture.worker.RunOnce(context.Background())
	if err != nil || !found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	run, err := fixture.store.GetRun(context.Background(), fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != store.RunSucceeded {
		t.Fatalf("run state = %s", run.State)
	}
}

func TestWorkerCancellationStopsHarnessAndCompletesRun(t *testing.T) {
	fixture := newWorkerFixtureWithTiming(t, "hang", time.Second, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := fixture.worker.RunOnce(ctx)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(fixture.workspace, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake harness did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	run, err := fixture.store.GetRun(context.Background(), fixture.runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != store.RunCancelled {
		t.Fatalf("run state = %s", run.State)
	}
}

func TestWorkerCleansPreparedInvocationAfterEveryProcessOutcome(t *testing.T) {
	for _, action := range []string{"success", "fail", "hang"} {
		t.Run(action, func(t *testing.T) {
			fixture := newWorkerFixtureWithTiming(t, action, time.Second, 20*time.Millisecond)
			adapter := &lifecycleAdapter{action: action}
			fixture.worker.config.Adapters = map[string]Adapter{"fake": adapter}
			ctx, cancel := context.WithCancel(context.Background())
			if action != "hang" {
				if found, err := fixture.worker.RunOnce(ctx); err != nil || !found {
					t.Fatalf("RunOnce() found=%v err=%v", found, err)
				}
			} else {
				done := make(chan error, 1)
				go func() {
					_, err := fixture.worker.RunOnce(ctx)
					done <- err
				}()
				waitForFile(t, filepath.Join(fixture.workspace, "ready"))
				cancel()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			cancel()
			if !adapter.cleaned.Load() {
				t.Fatal("prepared invocation cleanup was not called")
			}
			if _, err := os.Stat(adapter.temporary); !os.IsNotExist(err) {
				t.Fatalf("temporary invocation directory remains: %v", err)
			}
		})
	}
}

func TestWorkerWaitsForAndUsesDurableApproval(t *testing.T) {
	fixture := newWorkerFixtureWithTiming(t, "approval", time.Second, 20*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, err := fixture.worker.RunOnce(context.Background())
		done <- err
	}()
	requested := waitForApproval(t, fixture.store, fixture.runID)
	if _, err := fixture.store.DecideApproval(context.Background(), requested.ID, store.ApprovalApproved, "test", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not resume after approval")
	}
	run, _ := fixture.store.GetRun(context.Background(), fixture.runID)
	if run.State != store.RunSucceeded {
		t.Fatalf("run state = %s", run.State)
	}
}

func TestWorkerStopsBeforeProcessWhenApprovalDenied(t *testing.T) {
	fixture := newWorkerFixtureWithTiming(t, "approval", time.Second, 20*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, err := fixture.worker.RunOnce(context.Background())
		done <- err
	}()
	requested := waitForApproval(t, fixture.store, fixture.runID)
	if _, err := fixture.store.DecideApproval(context.Background(), requested.ID, store.ApprovalDenied, "test", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not conclude after denial")
	}
	run, _ := fixture.store.GetRun(context.Background(), fixture.runID)
	if run.State != store.RunCancelled || run.Conclusion != "approval denied" {
		t.Fatalf("run = %#v", run)
	}
	if _, err := os.Stat(filepath.Join(fixture.workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("harness ran despite denial: %v", err)
	}
}

func TestWorkerLeavesUnsupportedHarnessPending(t *testing.T) {
	fixture := newWorkerFixture(t, "success")
	fixture.worker.config.Adapters = map[string]Adapter{}
	found, err := fixture.worker.RunOnce(context.Background())
	if found || err != nil {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
	run, getErr := fixture.store.GetRun(context.Background(), fixture.runID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if run.State != store.RunPending {
		t.Fatalf("run = %#v", run)
	}
}

func TestWorkerNoPendingRun(t *testing.T) {
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	worker, err := New(Config{
		Store: runtimeStore, Owner: "worker", LogRoot: t.TempDir(),
		ResolveWorkspace: func(string) (string, error) { return t.TempDir(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := worker.RunOnce(context.Background())
	if err != nil || found {
		t.Fatalf("RunOnce() found=%v err=%v", found, err)
	}
}

func TestWorkerConfigurationValidation(t *testing.T) {
	valid := Config{Owner: "worker", LogRoot: "logs", ResolveWorkspace: func(string) (string, error) { return "", nil }}
	if _, err := New(valid); err == nil {
		t.Fatal("nil store accepted")
	}
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	valid.Store = runtimeStore
	for name, mutate := range map[string]func(*Config){
		"owner":     func(config *Config) { config.Owner = "" },
		"log root":  func(config *Config) { config.LogRoot = "" },
		"resolver":  func(config *Config) { config.ResolveWorkspace = nil },
		"heartbeat": func(config *Config) { config.LeaseTTL = time.Second; config.HeartbeatInterval = time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if _, err := New(config); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestReadTranscriptExcerptUsesOnlyBoundedStdout(t *testing.T) {
	root := t.TempDir()
	log, err := openOutputLog(root, "run", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	log.Write(awprocess.Output{Sequence: 1, Stream: awprocess.Stderr, Data: []byte("secret diagnostic")})
	log.Write(awprocess.Output{Sequence: 2, Stream: awprocess.Stdout, Data: []byte(strings.Repeat("x", maxTranscriptExcerptBytes+10) + "tail")})
	if _, err := log.Close(); err != nil {
		t.Fatal(err)
	}
	excerpt := readTranscriptExcerpt(LogPath(root, "run", "attempt"))
	if strings.Contains(excerpt, "secret diagnostic") || len(excerpt) != maxTranscriptExcerptBytes || !strings.HasSuffix(excerpt, "tail") {
		t.Fatalf("excerpt len=%d suffix=%q", len(excerpt), excerpt[len(excerpt)-4:])
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s did not appear", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type fakeAdapter struct{ action string }

type normalizingAdapter struct{ fakeAdapter }

type credentialAdapter struct {
	fakeAdapter
	endpoint string
	token    string
}

func (adapter *credentialAdapter) PrepareInvocation(ctx context.Context, request Request) (awprocess.Spec, func(), error) {
	adapter.endpoint = request.PermissionEndpoint
	adapter.token = request.PermissionToken
	specification, err := adapter.Invocation(ctx, request)
	return specification, func() {}, err
}

func (normalizingAdapter) NormalizeLine([]byte) (harness.Event, bool) {
	return harness.Event{Kind: harness.EventText, Text: harness.ConclusionOpen + `{"completed":["done"],"files_changed":[],"commands_run":[],"checks":[],"external_actions":[],"outstanding_work":[],"suggested_memory":["Keep focused tests"]}` + harness.ConclusionClose}, true
}

type lifecycleAdapter struct {
	action    string
	temporary string
	cleaned   atomic.Bool
}

func (adapter *lifecycleAdapter) Invocation(ctx context.Context, request Request) (awprocess.Spec, error) {
	return fakeAdapter{action: adapter.action}.Invocation(ctx, request)
}

func (adapter *lifecycleAdapter) PrepareInvocation(ctx context.Context, request Request) (awprocess.Spec, func(), error) {
	directory, err := os.MkdirTemp("", "agentworks-worker-test-")
	if err != nil {
		return awprocess.Spec{}, nil, err
	}
	adapter.temporary = directory
	specification, err := adapter.Invocation(ctx, request)
	if err != nil {
		_ = os.RemoveAll(directory)
		return awprocess.Spec{}, nil, err
	}
	return specification, func() {
		_ = os.RemoveAll(directory)
		adapter.cleaned.Store(true)
	}, nil
}

func (adapter fakeAdapter) Invocation(_ context.Context, request Request) (awprocess.Spec, error) {
	action := adapter.action
	if action == "approval" {
		action = "success"
	}
	if action == "retry" {
		action = "fail"
		if request.Attempt.Number > 1 {
			action = "success"
		}
	}
	return awprocess.Spec{
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestWorkerHarnessHelper", "--", action},
		Env:         append(os.Environ(), workerHelperEnvironment+"=1"),
		GracePeriod: 50 * time.Millisecond,
	}, nil
}

func (adapter fakeAdapter) RetryDecision(_ context.Context, request Request, _ awprocess.Result, processErr error) RetryDecision {
	if adapter.action != "retry" || request.Attempt.Number != 1 || processErr == nil {
		return RetryDecision{}
	}
	return RetryDecision{Retry: true, Delay: 20 * time.Millisecond, Reason: "test transient failure", SafeForWrite: true}
}

func (adapter fakeAdapter) ApprovalRequest(_ context.Context, _ Request) (*approval.Request, error) {
	if adapter.action != "approval" {
		return nil, nil
	}
	return &approval.Request{Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`)}, nil
}

type workerFixture struct {
	worker    *Worker
	store     *store.Store
	runID     string
	workspace string
	logRoot   string
}

func newWorkerFixture(t *testing.T, action string) workerFixture {
	t.Helper()
	return newWorkerFixtureWithTiming(t, action, time.Second, 20*time.Millisecond)
}

func newWorkerFixtureWithTiming(t *testing.T, action string, ttl, heartbeat time.Duration) workerFixture {
	t.Helper()
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeStore.Close() })
	workspace := t.TempDir()
	logRoot := t.TempDir()
	now := time.Now().UTC()
	event := store.Event{
		RecordID: "event-1", Source: "manual", ExternalID: "request-1",
		Type: "agentworks.manual-run.requested", OccurredAt: now,
		Data: json.RawMessage(`{"prompt":"test the worker"}`),
	}
	run := store.Run{
		ID: "run-1", IdempotencyKey: "manual:request-1", Team: "engineering",
		Agent: "implementer", Workspace: "product", Harness: "fake",
		Permission: store.PermissionReadwrite, State: store.RunPending,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, _, err := runtimeStore.IngestAndRoute(context.Background(), event, run, "test"); err != nil {
		t.Fatal(err)
	}
	worker, err := New(Config{
		Store: runtimeStore, Owner: "test-worker", LogRoot: logRoot,
		LeaseTTL: ttl, HeartbeatInterval: heartbeat,
		ResolveWorkspace: func(alias string) (string, error) {
			if alias != "product" {
				return "", fmt.Errorf("unexpected alias %s", alias)
			}
			return workspace, nil
		},
		Adapters: map[string]Adapter{"fake": fakeAdapter{action: action}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return workerFixture{worker: worker, store: runtimeStore, runID: run.ID, workspace: workspace, logRoot: logRoot}
}

func readLogRecords(t *testing.T, root string) []outputRecord {
	t.Helper()
	var logPath string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			logPath = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if logPath == "" {
		t.Fatal("no run log found")
	}
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var records []outputRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record outputRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func decodeRecord(t *testing.T, record outputRecord) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(record.Data)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitForApproval(t *testing.T, runtimeStore *store.Store, runID string) store.Approval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		approvals, err := runtimeStore.ListApprovals(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if len(approvals) != 0 {
			return approvals[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("approval was not requested")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
