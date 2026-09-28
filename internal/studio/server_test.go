package studio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	awmemory "github.com/mtfuller/agentworks/internal/memory"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
	awworkspace "github.com/mtfuller/agentworks/internal/workspace"
)

func TestHandlerServesEmbeddedUIAndHealth(t *testing.T) {
	handler, err := NewHandler("engineering-agents")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "AgentWorks Studio") {
		t.Fatalf("index response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Error("index response has no Content-Security-Policy")
	}

	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/health", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var health map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["status"] != "ok" || health["project"] != "engineering-agents" {
		t.Fatalf("health = %#v", health)
	}
}

func TestHandlerRejectsNonLoopbackHost(t *testing.T) {
	handler, _ := NewHandler("example")
	request := httptest.NewRequest(http.MethodGet, "http://evil.example/api/v1/health", nil)
	request.Host = "evil.example"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestHandlerReportsStorageAndListsDurableRuns(t *testing.T) {
	ctx := context.Background()
	runtimeStore, err := store.Open(ctx, filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeStore.Close() })
	now := time.Unix(1_800_000_000, 0).UTC()
	_, _, err = runtimeStore.CreateRun(ctx, store.Run{
		ID: "run-1", IdempotencyKey: "manual:1", Agent: "builder", Workspace: "default",
		Harness: "claude-code", Permission: store.PermissionReadwrite, State: store.RunPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	usageRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(usageRoot, "sample.log"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", Store: runtimeStore, LogRoot: filepath.Join(usageRoot, "logs"), StorageLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var health map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["storage"] != "ready" || health["schema_version"] != float64(9) {
		t.Fatalf("health = %#v", health)
	}
	if health["storage_bytes"] != float64(3) || health["storage_limit_bytes"] != float64(100) || health["storage_limit_exceeded"] != false {
		t.Fatalf("storage usage health = %#v", health)
	}

	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result struct {
		Runs []store.Run `json:"runs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Runs) != 1 || result.Runs[0].ID != "run-1" {
		t.Fatalf("runs = %#v", result.Runs)
	}
}

func TestOutcomeAPIUpdatesTimelineAndMonitors(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectRoot: root, Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	body := `{"type":"github.checks.failed","subject":"github:org/repo:pull/1","work_item":{"kind":"jira","external_key":"ABC-1","data":{}},"data":{}}`
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/outcomes", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://localhost")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Outcome store.Outcome `json:"outcome"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Outcome.WorkItemID == "" {
		t.Fatal("outcome was not correlated")
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/work-items/"+created.Outcome.WorkItemID+"/timeline", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "github.checks.failed") {
		t.Fatalf("timeline=%d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/monitors", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"monitor_id":"checks-passed"`) || !strings.Contains(response.Body.String(), `"state":"unhealthy"`) {
		t.Fatalf("monitors=%d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/outcomes", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "github.checks.failed") {
		t.Fatalf("outcomes=%d %s", response.Code, response.Body.String())
	}
}

func TestMemoryProposalDecisionAPIAppliesApprovedDiff(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	target := "memory/agents/reviewer.md"
	absolute := filepath.Join(root, target)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		t.Fatal(err)
	}
	before := []byte("# Memory\n")
	if err := os.WriteFile(absolute, before, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := runtimeStore.CreateRun(context.Background(), store.Run{ID: "run-memory", IdempotencyKey: "memory", Agent: "reviewer", Workspace: "product", Harness: "fake", State: store.RunSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	proposal := awmemory.AppendProposal("proposal-1", "run-memory", "agent", target, before, []string{"Keep tests focused"}, now)
	if err := runtimeStore.CreateMemoryProposal(context.Background(), proposal); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectRoot: root, Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/memory-proposals/proposal-1/decision", strings.NewReader(`{"decision":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://localhost")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	data, _ := os.ReadFile(absolute)
	if !strings.Contains(string(data), "Keep tests focused") {
		t.Fatalf("memory=%q", data)
	}
}

func TestM5APIValidationAndUnavailableStates(t *testing.T) {
	for name, serve := range map[string]func(http.ResponseWriter, *http.Request){"outcomes": func(w http.ResponseWriter, r *http.Request) { serveOutcomes(w, r, nil) }, "monitors": func(w http.ResponseWriter, r *http.Request) { serveMonitors(w, r, nil) }, "timeline": func(w http.ResponseWriter, r *http.Request) { serveTimeline(w, r, nil) }, "memory": func(w http.ResponseWriter, r *http.Request) { serveMemoryDecision(w, r, nil, "") }} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			serve(response, httptest.NewRequest(http.MethodGet, "http://localhost/", nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
	root, runtimeStore := manualRunFixture(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(`{`))
	serveMemoryDecision(response, request, runtimeStore, root)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(`{"decision":"later"}`))
	serveMemoryDecision(response, request, runtimeStore, root)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid decision status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(`{}`))
	serveCreateOutcome(response, request, runtimeStore)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid outcome status=%d", response.Code)
	}
	now := time.Now().UTC()
	if _, _, err := runtimeStore.CreateRun(context.Background(), store.Run{ID: "reject-run", IdempotencyKey: "reject-run", Agent: "reviewer", Workspace: "product", Harness: "fake", State: store.RunSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	target := "memory/reject.md"
	before := []byte("old\n")
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, target), before, 0o600); err != nil {
		t.Fatal(err)
	}
	proposal := awmemory.AppendProposal("reject-proposal", "reject-run", "agent", target, before, []string{"new"}, now)
	if err := runtimeStore.CreateMemoryProposal(context.Background(), proposal); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(`{"decision":"rejected"}`))
	request.SetPathValue("proposalID", proposal.ID)
	serveMemoryDecision(response, request, runtimeStore, root)
	if response.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", response.Code, response.Body.String())
	}
	conflict := awmemory.AppendProposal("conflict-proposal", "reject-run", "agent", target, before, []string{"conflict"}, now)
	if err := runtimeStore.CreateMemoryProposal(context.Background(), conflict); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, target), []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(`{"decision":"approved"}`))
	request.SetPathValue("proposalID", conflict.ID)
	serveMemoryDecision(response, request, runtimeStore, root)
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRunListRequiresStorage(t *testing.T) {
	handler, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestDefinitionEditorListsReadsAndAtomicallyUpdatesCanonicalFiles(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	memoryDir := filepath.Join(root, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "team.md"), []byte("# Team memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectName: "example", ProjectRoot: root, Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/definitions", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var listing struct {
		Files []definitionFile `json:"files"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range listing.Files {
		paths[file.Path] = true
	}
	for _, wanted := range []string{"agentworks.yaml", "teams/engineering/team.yaml", "agents/reviewer/AGENT.md", "memory/team.md"} {
		if !paths[wanted] {
			t.Errorf("definition listing missing %q: %#v", wanted, listing.Files)
		}
	}
	if paths["agentworks.local.yaml"] {
		t.Fatal("machine-local configuration exposed by definition editor")
	}

	path := "agents/reviewer/AGENT.md"
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/definitions/"+path, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var document definitionDocument
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Path != path || document.SHA256 == "" {
		t.Fatalf("document = %#v", document)
	}

	cookie, csrf := studioSession(t, handler)
	updated := strings.Replace(document.Content, "Review without editing.", "Review carefully without editing.", 1)
	response = putDefinition(t, handler, cookie, csrf, path, document.SHA256, updated)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", response.Code, response.Body.String())
	}
	var saved definitionDocument
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || saved.SHA256 == "" {
		t.Fatalf("saved definition = %#v err=%v", saved, err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || string(onDisk) != updated {
		t.Fatalf("on-disk definition = %q err=%v", onDisk, err)
	}

	response = putDefinition(t, handler, cookie, csrf, path, document.SHA256, updated+"again")
	if response.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d body=%s", response.Code, response.Body.String())
	}
	invalid := putDefinition(t, handler, cookie, csrf, path, saved.SHA256, "not markdown")
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid update status = %d body=%s", invalid.Code, invalid.Body.String())
	}
	onDisk, _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if string(onDisk) != updated {
		t.Fatalf("invalid update changed file: %q", onDisk)
	}
}

func TestDefinitionEditorRejectsLocalConfigAndRequiresCSRF(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectName: "example", ProjectRoot: root, Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/definitions/agentworks.local.yaml", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("local config status = %d", response.Code)
	}
	body := `{"expected_sha256":"anything","content":"format: 2"}`
	request = httptest.NewRequest(http.MethodPut, "http://localhost/api/v1/definitions/agentworks.yaml", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unprotected update status = %d", response.Code)
	}
}

func TestManualRunRequiresSessionAndCSRF(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", ProjectRoot: root, Store: runtimeStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"request_id":"request-1","team":"engineering","agent":"reviewer","workspace":"product","harness":"claude-code","permission":"readwrite","prompt":"Review this change"}`
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/runs", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unprotected status = %d, want 403", response.Code)
	}

	cookie, csrf := studioSession(t, handler)
	request = httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/runs", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://evil.example")
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/runs", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://localhost:9090")
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-port status = %d, want 403", response.Code)
	}
}

func TestManualRunCreatesDurableCappedRunAndDeduplicates(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", ProjectRoot: root, Store: runtimeStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	body := `{"request_id":"request-1","team":"engineering","workspace":"product","harness":"claude-code","permission":"autonomous","prompt":"Review this change"}`

	response := postManualRun(t, handler, cookie, csrf, body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Created bool      `json:"created"`
		Run     store.Run `json:"run"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.Run.Permission != store.PermissionReadonly || created.Run.Harness != "claude-code" {
		t.Fatalf("created response = %#v", created)
	}

	response = postManualRun(t, handler, cookie, csrf, body)
	if response.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d body=%s", response.Code, response.Body.String())
	}
	var duplicate struct {
		Created bool      `json:"created"`
		Run     store.Run `json:"run"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Created || duplicate.Run.ID != created.Run.ID {
		t.Fatalf("duplicate response = %#v", duplicate)
	}
	changed := strings.Replace(body, `"claude-code"`, `"github-copilot"`, 1)
	response = postManualRun(t, handler, cookie, csrf, changed)
	if response.Code != http.StatusConflict {
		t.Fatalf("changed duplicate status = %d body=%s", response.Code, response.Body.String())
	}
	runs, err := runtimeStore.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("durable run count = %d, want 1", len(runs))
	}
}

func TestManualRunWakesControllerAndPendingRunCanBeCancelled(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	controller := &testRunController{}
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", ProjectRoot: root, Store: runtimeStore,
		Harnesses: []string{"fake"}, Controller: controller,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	response := postManualRun(t, handler, cookie, csrf, `{"request_id":"fake-1","team":"engineering","workspace":"product","harness":"fake","permission":"readonly","prompt":"exercise runtime"}`)
	if response.Code != http.StatusAccepted || controller.wakes != 1 {
		t.Fatalf("create status=%d wakes=%d body=%s", response.Code, controller.wakes, response.Body.String())
	}
	var result struct {
		Run store.Run `json:"run"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/runs/"+result.Run.ID+"/cancel", nil)
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", response.Code, response.Body.String())
	}
	run, _ := runtimeStore.GetRun(context.Background(), result.Run.ID)
	if run.State != store.RunCancelled {
		t.Fatalf("state = %s", run.State)
	}
}

func TestAdHocWorkspaceConfirmationUsesOnlyEphemeralAliasDurably(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	registry := awworkspace.NewRegistry()
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", ProjectRoot: root, Store: runtimeStore,
		Harnesses: []string{"fake"}, Workspaces: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	adHocPath := t.TempDir()
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/workspaces/confirm", strings.NewReader(`{"path":`+strconv.Quote(adHocPath)+`}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", response.Code, response.Body.String())
	}
	var confirmation struct {
		Alias string `json:"alias"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &confirmation); err != nil {
		t.Fatal(err)
	}
	canonicalPath, err := filepath.EvalSymlinks(adHocPath)
	if err != nil {
		t.Fatal(err)
	}
	if !awworkspace.IsAdHoc(confirmation.Alias) || confirmation.Path != canonicalPath {
		t.Fatalf("confirmation = %#v", confirmation)
	}
	body := fmt.Sprintf(`{"request_id":"adhoc-1","team":"engineering","workspace":%q,"harness":"fake","permission":"readonly","prompt":"inspect this folder"}`, confirmation.Alias)
	response = postManualRun(t, handler, cookie, csrf, body)
	if response.Code != http.StatusAccepted {
		t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
	}
	var created struct {
		Run store.Run `json:"run"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	event, err := runtimeStore.GetEvent(context.Background(), created.Run.EventRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Run.Workspace != confirmation.Alias || strings.Contains(string(event.Data), confirmation.Path) {
		t.Fatalf("durable run=%#v event=%s", created.Run, event.Data)
	}
}

func TestUnconfirmedAdHocWorkspaceRejectsWriteCapableRun(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	agentPath := filepath.Join(root, "agents", "reviewer", "AGENT.md")
	if err := os.WriteFile(agentPath, []byte("---\nname: reviewer\nmax_permission: readwrite\n---\n\nReview and edit.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := awworkspace.NewRegistry()
	binding, err := registry.Bind(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", ProjectRoot: root, Store: runtimeStore,
		Harnesses: []string{"fake"}, Workspaces: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	body := fmt.Sprintf(`{"request_id":"adhoc-write","team":"engineering","workspace":%q,"harness":"fake","permission":"readwrite","prompt":"edit"}`, binding.Alias)
	response := postManualRun(t, handler, cookie, csrf, body)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "requires confirmation") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRunDetailRedactsLogPathAndServesPersistedLog(t *testing.T) {
	_, runtimeStore := manualRunFixture(t)
	logRoot := t.TempDir()
	now := time.Now().UTC()
	_, _, err := runtimeStore.CreateRun(context.Background(), store.Run{
		ID: "run-log", IdempotencyKey: "log:1", Agent: "reviewer", Workspace: "product",
		Harness: "fake", Permission: store.PermissionReadonly, State: store.RunPending,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := runtimeStore.ClaimNext(context.Background(), "worker", now, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	if err := runtimeStore.PrepareClaim(context.Background(), claim, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.StartAttempt(context.Background(), claim, 123, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	logPath := worker.LogPath(logRoot, claim.Run.ID, claim.Attempt.ID)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	logData := []byte(`{"sequence":1,"stream":"stdout","time":"2026-01-01T00:00:00Z","data_base64":"aGVsbG8="}` + "\n")
	if err := os.WriteFile(logPath, logData, 0o600); err != nil {
		t.Fatal(err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"exit_code": 0, "log": map[string]any{"path": logPath, "bytes": len(logData)}})
	if err := runtimeStore.CompleteClaim(context.Background(), claim, store.RunSucceeded, "done", resultJSON, now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandlerWithConfig(HandlerConfig{Store: runtimeStore, LogRoot: logRoot})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs/run-log", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), logPath) || !strings.Contains(response.Body.String(), `"log_url"`) {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs/run-log/attempts/"+claim.Attempt.ID+"/log", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != string(logData) {
		t.Fatalf("log status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRuntimeAPIHandlesUnavailableMissingAndTerminalRuns(t *testing.T) {
	handler, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs/missing", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable detail status = %d", response.Code)
	}

	_, runtimeStore := manualRunFixture(t)
	now := time.Now().UTC()
	if _, _, err := runtimeStore.CreateRun(context.Background(), store.Run{
		ID: "terminal", IdempotencyKey: "terminal:1", Agent: "reviewer", Workspace: "product",
		Harness: "fake", Permission: store.PermissionReadonly, State: store.RunCancelled,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	controller := &testRunController{activeRun: "active"}
	handler, err = NewHandlerWithConfig(HandlerConfig{Store: runtimeStore, Controller: controller, LogRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs/missing", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing detail status = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs/terminal/attempts/missing/log", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing log status = %d", response.Code)
	}

	cookie, csrf := studioSession(t, handler)
	for id, want := range map[string]int{"terminal": http.StatusConflict, "missing": http.StatusNotFound, "active": http.StatusAccepted} {
		request = httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/runs/"+id+"/cancel", nil)
		request.Header.Set("X-AgentWorks-CSRF", csrf)
		request.AddCookie(cookie)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("cancel %s status = %d, want %d body=%s", id, response.Code, want, response.Body.String())
		}
	}
}

func TestApprovalDecisionEndpointResumesRun(t *testing.T) {
	_, runtimeStore := manualRunFixture(t)
	now := time.Now().UTC()
	if _, _, err := runtimeStore.CreateRun(context.Background(), store.Run{
		ID: "approval-run", IdempotencyKey: "approval:run", Agent: "reviewer", Workspace: "product",
		Harness: "fake", Permission: store.PermissionReadwrite, State: store.RunPending,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	claim, found, err := runtimeStore.ClaimNext(context.Background(), "worker", now, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim found=%v err=%v", found, err)
	}
	if err := runtimeStore.PrepareClaim(context.Background(), claim, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	approval, _, err := runtimeStore.RequestApproval(context.Background(), claim, store.Approval{
		ID: "approval-1", Kind: "filesystem.write", Scope: json.RawMessage(`{"directory":"."}`),
	}, now.Add(2*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler("example", runtimeStore)
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/approvals/"+approval.ID+"/decision", strings.NewReader(`{"decision":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("decision status=%d body=%s", response.Code, response.Body.String())
	}
	run, _ := runtimeStore.GetRun(context.Background(), claim.Run.ID)
	if run.State != store.RunPreparing {
		t.Fatalf("run state = %s", run.State)
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/runs/"+claim.Run.ID, nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"approvals":[`) || !strings.Contains(response.Body.String(), `"state":"approved"`) {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkspaceConfirmationAndApprovalDecisionValidation(t *testing.T) {
	_, runtimeStore := manualRunFixture(t)
	registry := awworkspace.NewRegistry()
	handler, err := NewHandlerWithConfig(HandlerConfig{Store: runtimeStore, Workspaces: registry})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	tests := []struct {
		name        string
		path        string
		contentType string
		body        string
		want        int
	}{
		{"workspace content type", "/api/v1/workspaces/confirm", "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{"workspace bad json", "/api/v1/workspaces/confirm", "application/json", `{`, http.StatusBadRequest},
		{"workspace extra json", "/api/v1/workspaces/confirm", "application/json", `{"path":"x"} {}`, http.StatusBadRequest},
		{"workspace missing path", "/api/v1/workspaces/confirm", "application/json", `{"path":""}`, http.StatusBadRequest},
		{"approval content type", "/api/v1/approvals/missing/decision", "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{"approval bad json", "/api/v1/approvals/missing/decision", "application/json", `{`, http.StatusBadRequest},
		{"approval extra json", "/api/v1/approvals/missing/decision", "application/json", `{"decision":"approved"} {}`, http.StatusBadRequest},
		{"approval bad decision", "/api/v1/approvals/missing/decision", "application/json", `{"decision":"maybe"}`, http.StatusBadRequest},
		{"approval missing", "/api/v1/approvals/missing/decision", "application/json", `{"decision":"approved"}`, http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://localhost"+test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("X-AgentWorks-CSRF", csrf)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}

	unavailable, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf = studioSession(t, unavailable)
	for _, path := range []string{"/api/v1/workspaces/confirm", "/api/v1/approvals/missing/decision"} {
		request := httptest.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(`{"path":"x","decision":"approved"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-AgentWorks-CSRF", csrf)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		unavailable.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("unavailable %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestManualRunValidatesContentAndDefinitions(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	handler, err := NewHandlerWithConfig(HandlerConfig{
		ProjectName: "example", ProjectRoot: root, Store: runtimeStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"content type", "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{"unknown field", "application/json", `{"unknown":true}`, http.StatusBadRequest},
		{"bad request ID", "application/json", `{"request_id":"bad id","team":"engineering","workspace":"product","harness":"claude-code","permission":"readonly","prompt":"hello"}`, http.StatusBadRequest},
		{"empty prompt", "application/json", `{"request_id":"r0","team":"engineering","workspace":"product","harness":"claude-code","permission":"readonly","prompt":"  "}`, http.StatusBadRequest},
		{"unknown team", "application/json", `{"request_id":"r1","team":"other","workspace":"product","harness":"claude-code","permission":"readonly","prompt":"hello"}`, http.StatusBadRequest},
		{"unknown agent", "application/json", `{"request_id":"r1a","team":"engineering","agent":"builder","workspace":"product","harness":"claude-code","permission":"readonly","prompt":"hello"}`, http.StatusBadRequest},
		{"unbound workspace", "application/json", `{"request_id":"r2","team":"engineering","workspace":"unbound","harness":"claude-code","permission":"readonly","prompt":"hello"}`, http.StatusBadRequest},
		{"unknown harness", "application/json", `{"request_id":"r3","team":"engineering","workspace":"product","harness":"other","permission":"readonly","prompt":"hello"}`, http.StatusBadRequest},
		{"unknown permission", "application/json", `{"request_id":"r4","team":"engineering","workspace":"product","harness":"claude-code","permission":"root","prompt":"hello"}`, http.StatusBadRequest},
		{"extra document", "application/json", `{"request_id":"r5"} {"second":true}`, http.StatusBadRequest},
		{"invalid trailing JSON", "application/json", `{"request_id":"r6"} {`, http.StatusBadRequest},
		{"too large", "application/json", `{"request_id":"r7","prompt":"` + strings.Repeat("x", maxManualRequestBytes) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/runs", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("X-AgentWorks-CSRF", csrf)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestManualRunUnavailableWithoutRuntimeConfiguration(t *testing.T) {
	handler, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	response := postManualRun(t, handler, cookie, csrf, `{}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestCatalogDoesNotExposeWorkspacePaths(t *testing.T) {
	root, runtimeStore := manualRunFixture(t)
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectRoot: root, Store: runtimeStore})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/catalog", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), root) {
		t.Fatalf("catalog exposed absolute path: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"bound":true`) || !strings.Contains(response.Body.String(), `"bound":false`) {
		t.Fatalf("catalog bindings = %s", response.Body.String())
	}
}

func TestCatalogUnavailableAndInvalidProject(t *testing.T) {
	handler, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/catalog", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d", response.Code)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "agentworks.yaml"), []byte("not: a-project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler, err = NewHandlerWithConfig(HandlerConfig{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/catalog", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("invalid project status = %d", response.Code)
	}
}

func TestSameOriginRequest(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		origin string
		want   bool
	}{
		{"missing origin", "http://localhost:8080/", "", true},
		{"same origin", "http://127.0.0.1:8080/", "http://127.0.0.1:8080", true},
		{"different port", "http://localhost:8080/", "http://localhost:9090", false},
		{"non-loopback", "http://localhost:8080/", "http://example.com", false},
		{"bad origin", "http://localhost:8080/", "://", false},
		{"userinfo", "http://localhost:8080/", "http://user@localhost:8080", false},
		{"wrong scheme", "http://localhost:8080/", "https://localhost:8080", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.url, nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if got := sameOriginRequest(request); got != test.want {
				t.Fatalf("sameOriginRequest = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEventStreamEmitsReadyEvent(t *testing.T) {
	handler, _ := NewHandler("example")
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/events", nil).WithContext(ctx)
	writer := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flush: cancel}
	handler.ServeHTTP(writer, request)
	if contentType := writer.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if body := writer.Body.String(); !strings.Contains(body, "event: studio.ready") || !strings.Contains(body, `"project":"example"`) {
		t.Errorf("event body = %q", body)
	}
}

func TestEventStreamReplaysAfterLastEventID(t *testing.T) {
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeStore.Close() })
	for index := 1; index <= 2; index++ {
		if _, err := runtimeStore.AppendRuntimeEvent(context.Background(), store.RuntimeEvent{
			Topic: "run.state", EntityType: "run", EntityID: "run-1",
			Data: json.RawMessage(fmt.Sprintf(`{"index":%d}`, index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := NewHandler("example", runtimeStore)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/events", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", "1")
	writer := &countingFlushRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, cancelAt: 2}
	handler.ServeHTTP(writer, request)
	body := writer.Body.String()
	if strings.Contains(body, `"index":1`) || !strings.Contains(body, "id: 2\n") || !strings.Contains(body, `"index":2`) {
		t.Fatalf("replayed SSE = %q", body)
	}
}

func TestEventStreamRejectsInvalidCursor(t *testing.T) {
	handler, _ := NewHandler("example")
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/events", nil)
	request.Header.Set("Last-Event-ID", "not-a-number")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestStartBindsLoopbackAndShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	instance, err := Start(ctx, Config{Host: "127.0.0.1", Port: 0, ProjectName: "example"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(instance.URL() + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	stream, err := http.Get(instance.URL() + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stream.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != "event: studio.ready\n" {
		t.Fatalf("first SSE line = %q, %v", line, err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- instance.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Studio did not shut down")
	}
	stream.Body.Close()
}

func TestStartRejectsUnsafeBindings(t *testing.T) {
	for _, config := range []Config{{Host: "0.0.0.0"}, {Host: "example.com"}, {Host: "127.0.0.1", Port: -1}} {
		if instance, err := Start(context.Background(), config); err == nil {
			instance.server.Close()
			t.Fatalf("Start(%#v) succeeded", config)
		}
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flush func()
}

type countingFlushRecorder struct {
	*httptest.ResponseRecorder
	cancel   func()
	flushes  int
	cancelAt int
}

func (recorder *countingFlushRecorder) Flush() {
	recorder.ResponseRecorder.Flush()
	recorder.flushes++
	if recorder.flushes >= recorder.cancelAt {
		recorder.cancel()
	}
}

type testRunController struct {
	wakes     int
	activeRun string
}

func (controller *testRunController) Wake() { controller.wakes++ }
func (controller *testRunController) Cancel(runID string) bool {
	return runID == controller.activeRun
}

func (recorder *flushRecorder) Flush() {
	recorder.ResponseRecorder.Flush()
	recorder.flush()
}

func manualRunFixture(t *testing.T) (string, *store.Store) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{
		workspace,
		filepath.Join(root, "teams", "engineering"),
		filepath.Join(root, "agents", "reviewer"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"agentworks.yaml":       "format: 2\nname: example\nteams: [engineering]\nworkspaces: [product, unbound]\n",
		"agentworks.local.yaml": "workspaces:\n  product:\n    path: " + workspace + "\n",
		filepath.Join("teams", "engineering", "team.yaml"): "name: engineering\nagents: [reviewer]\ndefault_agent: reviewer\n",
		filepath.Join("agents", "reviewer", "AGENT.md"):    "---\nname: reviewer\nmax_permission: readonly\n---\n\nReview without editing.\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeStore.Close() })
	return root, runtimeStore
}

func studioSession(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/session", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	result := response.Result()
	cookies := result.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookies = %d", len(cookies))
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", cookies[0])
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["csrf_token"] == "" {
		t.Fatal("empty CSRF token")
	}
	return cookies[0], body["csrf_token"]
}

func postManualRun(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/runs", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://localhost")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func putDefinition(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, path, expected, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"expected_sha256": expected, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "http://localhost/api/v1/definitions/"+path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.Header.Set("Origin", "http://localhost")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
