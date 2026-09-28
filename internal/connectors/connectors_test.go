package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
)

func TestJiraRecordedResponses(t *testing.T) {
	server := fixtureServer(t, "jira", func(request *http.Request) string {
		if !strings.HasPrefix(request.Header.Get("Authorization"), "Basic ") {
			t.Error("missing Jira authorization")
		}
		switch {
		case request.URL.Path == "/rest/api/3/myself":
			return "myself.json"
		case request.URL.Path == "/rest/api/3/search/jql":
			return "search.json"
		case strings.HasSuffix(request.URL.Path, "/changelog"):
			return "changelog.json"
		default:
			return ""
		}
	})
	defer server.Close()
	source := jiraSource(server.URL)
	connector, err := NewJira(source, "person@example.com", "super-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 12, 6, 0, 0, time.UTC)
	batch, err := connector.Poll(context.Background(), time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Events[0].Envelope.Type != "jira.issue.assigned" || len(batch.Outcomes) != 1 || batch.Outcomes[0].Type != "jira.issue.transitioned" {
		t.Fatalf("batch=%#v", batch)
	}
	encoded, _ := json.Marshal(batch)
	if strings.Contains(string(encoded), "super-secret") || strings.Contains(string(encoded), "person@example.com") {
		t.Fatal("credentials leaked into batch")
	}
}

func TestGitHubRecordedResponses(t *testing.T) {
	server := fixtureServer(t, "github", func(request *http.Request) string {
		if request.Header.Get("Authorization") != "Bearer super-secret" {
			t.Error("missing GitHub authorization")
		}
		switch {
		case request.URL.Path == "/repos/acme/widgets":
			return "repository.json"
		case request.URL.Path == "/repos/acme/widgets/pulls":
			return "pulls.json"
		case request.URL.Path == "/repos/acme/widgets/issues/comments":
			return "comments.json"
		case strings.HasSuffix(request.URL.Path, "/check-runs"):
			return "checks.json"
		default:
			return ""
		}
	})
	defer server.Close()
	source := githubSource(server.URL)
	connector, err := NewGitHub(source, "super-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 12, 31, 0, 0, time.UTC)
	batch, err := connector.Poll(context.Background(), time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || batch.Events[0].Envelope.Type != "github.pr.comment.created" {
		t.Fatalf("events=%#v", batch.Events)
	}
	wants := map[string]bool{"github.pr.opened": false, "github.pr.merged": false, "github.checks.passed": false}
	for _, outcome := range batch.Outcomes {
		wants[outcome.Type] = true
		if outcome.WorkItemID != stableID("work", "jira", "ABC-123") {
			t.Fatalf("work item=%q", outcome.WorkItemID)
		}
	}
	for kind, found := range wants {
		if !found {
			t.Errorf("missing %s: %#v", kind, batch.Outcomes)
		}
	}
}

func TestRecordedGitHubPollFlowsThroughDiagnosticsAndRouting(t *testing.T) {
	server := fixtureServer(t, "github", func(request *http.Request) string {
		switch {
		case request.URL.Path == "/repos/acme/widgets/pulls":
			return "pulls.json"
		case request.URL.Path == "/repos/acme/widgets/issues/comments":
			return "comments.json"
		case strings.HasSuffix(request.URL.Path, "/check-runs"):
			return "checks.json"
		default:
			return ""
		}
	})
	defer server.Close()
	root, runtimeStore := connectorFixture(t)
	writeSource := "name: github-main\nkind: github\npoll:\n  every: 1m\n  initial_lookback: 1h\ngithub:\n  api_url: " + server.URL + "\n  repository: acme/widgets\n  token_env: GITHUB_TOKEN\n"
	if err := os.WriteFile(filepath.Join(root, "sources", "github-main", "source.yaml"), []byte(writeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	routing := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	now := time.Date(2026, 9, 26, 12, 31, 0, 0, time.UTC)
	service := &Service{Root: root, Store: runtimeStore, Router: routing, Now: func() time.Time { return now }, Factory: func(source spec.Source) (Connector, error) {
		return NewGitHub(source, "super-secret", server.Client())
	}}
	result, err := service.PollNow(context.Background(), "github-main")
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.EventsInserted != 1 || result.Routed != 1 || result.Attempt.State != "succeeded" || result.Attempt.EventsObserved != 1 || result.Attempt.EventsInserted != 1 || result.Attempt.OutcomesObserved != 3 || result.Attempt.OutcomesInserted != 3 || len(result.Attempt.EventRecordIDs) != 1 || len(result.Attempt.RunIDs) != 1 {
		t.Fatalf("result=%#v", result)
	}
	// A second identical recorded response must advance diagnostics without
	// creating a second event, outcome, or run.
	second, err := service.PollNow(context.Background(), "github-main")
	if err != nil {
		t.Fatal(err)
	}
	if second.Commit.EventsInserted != 0 || second.Commit.OutcomesInserted != 0 || second.Attempt.EventsObserved != 0 || second.Attempt.EventsInserted != 0 || second.Attempt.RoutedRuns != 0 {
		t.Fatalf("second=%#v", second)
	}
}

func TestPollingAcrossJiraAndGitHubBuildsOneTimeline(t *testing.T) {
	root, runtimeStore := connectorFixture(t)
	routing := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	now := time.Date(2026, 9, 26, 13, 0, 0, 0, time.UTC)
	jiraItem := store.WorkItem{ID: stableID("work", "jira", "ABC-123"), Kind: "jira", ExternalKey: "ABC-123", Title: "Ship it", State: "In Progress", Data: json.RawMessage(`{}`)}
	factory := func(source spec.Source) (Connector, error) {
		switch source.Kind {
		case spec.SourceJira:
			return fixtureConnector{batch: Batch{Cursor: "jira-cursor", Events: []Emission{{Envelope: router.Envelope{ID: "assignment-1", Source: source.Name, Type: "jira.issue.assigned", Subject: "ABC-123", Time: now, Data: json.RawMessage(`{"issue_key":"ABC-123","workspace":"/tmp/untrusted"}`)}, WorkItem: jiraItem}}, Outcomes: []store.Outcome{{ID: "jira-transition", Type: "jira.issue.transitioned", Subject: "jira:ABC-123", WorkItemID: jiraItem.ID, OccurredAt: now, Data: json.RawMessage(`{}`)}}}}, nil
		case spec.SourceGitHub:
			return fixtureConnector{batch: Batch{Cursor: "github-cursor", Events: []Emission{{Envelope: router.Envelope{ID: "comment-1", Source: source.Name, Type: "github.pr.comment.created", Subject: "github:acme/widgets:pull/7", Time: now.Add(time.Minute), Data: json.RawMessage(`{"comment":"please revise"}`)}, WorkItem: jiraItem}}, Outcomes: []store.Outcome{{ID: "checks", Type: "github.checks.passed", WorkItemID: jiraItem.ID, OccurredAt: now.Add(2 * time.Minute), Data: json.RawMessage(`{}`)}, {ID: "merged", Type: "github.pr.merged", WorkItemID: jiraItem.ID, OccurredAt: now.Add(3 * time.Minute), Data: json.RawMessage(`{}`)}}}}, nil
		default:
			return nil, errors.New("unexpected source")
		}
	}
	service := &Service{Root: root, Store: runtimeStore, Router: routing, Factory: factory, Now: func() time.Time { return now }}
	first, err := service.PollNow(context.Background(), "jira-main")
	if err != nil || first.Routed != 1 {
		t.Fatalf("jira=%#v err=%v", first, err)
	}
	if first.Attempt.State != "succeeded" || first.Attempt.EventsObserved != 1 || first.Attempt.EventsInserted != 1 || first.Attempt.RoutedRuns != 1 || len(first.Attempt.EventRecordIDs) != 1 || len(first.Attempt.RunIDs) != 1 {
		t.Fatalf("jira diagnostics=%#v", first.Attempt)
	}
	service.Now = func() time.Time { return now.Add(5 * time.Minute) }
	second, err := service.PollNow(context.Background(), "github-main")
	if err != nil || second.Routed != 1 {
		t.Fatalf("github=%#v err=%v", second, err)
	}
	runs, _ := runtimeStore.ListRuns(context.Background(), 10)
	if len(runs) != 2 || runs[0].Workspace != "product" || runs[1].Workspace != "product" {
		t.Fatalf("runs=%#v", runs)
	}
	entries, err := runtimeStore.ContextEntries(context.Background(), runs[0])
	if err != nil || len(entries) == 0 {
		t.Fatalf("history=%#v err=%v", entries, err)
	}
	timeline, err := runtimeStore.WorkItemTimeline(context.Background(), jiraItem.ID)
	if err != nil || len(timeline) < 6 {
		t.Fatalf("timeline=%#v err=%v", timeline, err)
	}
	states, _ := runtimeStore.ListMonitorStates(context.Background())
	encoded, _ := json.Marshal(states)
	for _, want := range []string{"checks-passed", "pr-merged", "jira-transitioned"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("monitor %s missing: %s", want, encoded)
		}
	}
	// Repeating both pages does not create another event, run, or outcome.
	if _, err := service.PollNow(context.Background(), "github-main"); err != nil {
		t.Fatal(err)
	}
	runs, _ = runtimeStore.ListRuns(context.Background(), 10)
	outcomes, _ := runtimeStore.ListOutcomes(context.Background(), "", 20)
	if len(runs) != 2 || len(outcomes) != 3 {
		t.Fatalf("dedup runs=%d outcomes=%d", len(runs), len(outcomes))
	}
}

func TestFailedPollDoesNotAdvanceCursorAndRateLimitBacksOff(t *testing.T) {
	root, runtimeStore := connectorFixture(t)
	routing := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	now := time.Date(2026, 9, 26, 13, 0, 0, 0, time.UTC)
	reset := now.Add(20 * time.Minute)
	service := &Service{Root: root, Store: runtimeStore, Router: routing, Now: func() time.Time { return now }, Factory: func(spec.Source) (Connector, error) {
		return fixtureConnector{err: &RateLimitError{ResetAt: reset}}, nil
	}}
	if _, err := service.PollNow(context.Background(), "jira-main"); err == nil {
		t.Fatal("rate limit succeeded")
	}
	status, err := runtimeStore.Source(context.Background(), "jira-main")
	if err != nil {
		t.Fatal(err)
	}
	if status.Cursor != "" || status.State != "degraded" || !status.NextPollAt.Equal(reset) || status.LastError != "connector rate limit reached" {
		t.Fatalf("status=%#v", status)
	}
	attempts, err := runtimeStore.ListSourcePollAttempts(context.Background(), "jira-main", 10)
	if err != nil || len(attempts) != 1 || attempts[0].State != "failed" || attempts[0].ErrorCode != "rate_limited" || !attempts[0].RateLimitResetAt.Equal(reset) {
		t.Fatalf("diagnostics=%#v err=%v", attempts, err)
	}
}

func TestHTTPFailuresAndRateLimitsNeverExposeCredentials(t *testing.T) {
	reset := time.Now().UTC().Add(5 * time.Minute).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-RateLimit-Remaining", "0")
		writer.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset))
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"token":"super-secret"}`))
	}))
	defer server.Close()
	connector, err := NewGitHub(githubSource(server.URL), "super-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = connector.Test(context.Background())
	var limited *RateLimitError
	if !errors.As(err, &limited) || limited.ResetAt.Unix() != reset {
		t.Fatalf("error=%v reset=%v", err, limited)
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatal("credential leaked in error")
	}
}

func TestJiraTimestampVariants(t *testing.T) {
	for _, value := range []string{`"2026-09-26T12:00:00Z"`, `"2026-09-26T12:00:00.000+0000"`} {
		var parsed apiTime
		if err := json.Unmarshal([]byte(value), &parsed); err != nil || parsed.Time().IsZero() {
			t.Fatalf("value=%s parsed=%v err=%v", value, parsed, err)
		}
	}
}

func TestRuntimePollsDueSourceAndStops(t *testing.T) {
	root, runtimeStore := connectorFixture(t)
	routing := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	called := make(chan struct{}, 1)
	now := time.Date(2026, 9, 26, 13, 0, 0, 0, time.UTC)
	service := &Service{Root: root, Store: runtimeStore, Router: routing, Now: func() time.Time { return now }, Factory: func(source spec.Source) (Connector, error) {
		return signalingConnector{called: called, batch: Batch{Cursor: source.Name + "-cursor"}}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := Start(ctx, service)
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("connector was not polled")
	}
	cancel()
	runtime.Wait()
}

type fixtureConnector struct {
	batch Batch
	err   error
}

type signalingConnector struct {
	called chan struct{}
	batch  Batch
}

func (signalingConnector) Test(context.Context) error { return nil }
func (connector signalingConnector) Poll(context.Context, string, time.Time) (Batch, error) {
	select {
	case connector.called <- struct{}{}:
	default:
	}
	return connector.batch, nil
}

func (connector fixtureConnector) Test(context.Context) error { return connector.err }
func (connector fixtureConnector) Poll(context.Context, string, time.Time) (Batch, error) {
	return connector.batch, connector.err
}

func fixtureServer(t *testing.T, directory string, selectFile func(*http.Request) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name := selectFile(request)
		if name == "" {
			http.NotFound(writer, request)
			return
		}
		data, err := os.ReadFile(filepath.Join("testdata", directory, name))
		if err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-RateLimit-Remaining", "100")
		_, _ = writer.Write(data)
	}))
}

func jiraSource(base string) spec.Source {
	return spec.Source{Name: "jira-main", Kind: spec.SourceJira, Poll: spec.SourcePoll{Every: "1m", InitialLookback: "1h"}, Jira: spec.JiraSource{BaseURL: base, JQL: "assignee = currentUser()", EmailEnv: "JIRA_EMAIL", TokenEnv: "JIRA_TOKEN"}}
}
func githubSource(base string) spec.Source {
	return spec.Source{Name: "github-main", Kind: spec.SourceGitHub, Poll: spec.SourcePoll{Every: "1m", InitialLookback: "1h"}, GitHub: spec.GitHubSource{APIURL: base, Repository: "acme/widgets", TokenEnv: "GITHUB_TOKEN"}}
}

func connectorFixture(t *testing.T) (string, *store.Store) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	_ = os.MkdirAll(workspace, 0o755)
	files := map[string]string{
		"agentworks.yaml":                 "format: 2\nname: connectors\nteams: [engineering]\nworkspaces: [product]\n",
		"agentworks.local.yaml":           "workspaces:\n  product:\n    path: " + workspace + "\n",
		"teams/engineering/team.yaml":     "name: engineering\nagents: [factory]\ndefault_agent: factory\n",
		"agents/factory/AGENT.md":         "---\nname: factory\nmax_permission: readonly\n---\nHandle external work.\n",
		"sources/jira-main/source.yaml":   "name: jira-main\nkind: jira\npoll:\n  every: 1m\n  initial_lookback: 1h\njira:\n  base_url: https://example.atlassian.net\n  jql: assignee = currentUser()\n  email_env: JIRA_EMAIL\n  token_env: JIRA_TOKEN\n",
		"sources/github-main/source.yaml": "name: github-main\nkind: github\npoll:\n  every: 1m\n  initial_lookback: 1h\ngithub:\n  repository: acme/widgets\n  token_env: GITHUB_TOKEN\n",
		"routes/jira/route.yaml":          "name: jira\npriority: 10\nwhen:\n  source: jira-main\n  type: jira.issue.assigned\ninvoke:\n  team: engineering\n  workspace: product\n  harness: fake\n",
		"routes/github/route.yaml":        "name: github\npriority: 10\nwhen:\n  source: github-main\n  type: github.pr.comment.created\ninvoke:\n  team: engineering\n  workspace: product\n  harness: fake\n",
	}
	for name, value := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtimeStore.Close() })
	return root, runtimeStore
}
