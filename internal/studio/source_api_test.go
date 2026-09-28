package studio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/connectors"
	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/spec"
)

func TestSourceAPIListsTestsPollsAndPauses(t *testing.T) {
	root, runtimeStore := eventAPIFixture(t)
	path := filepath.Join(root, "sources", "jira-main", "source.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("name: jira-main\nkind: jira\npoll:\n  every: 1m\njira:\n  base_url: https://example.atlassian.net\n  jql: assignee = currentUser()\n  email_env: JIRA_EMAIL\n  token_env: JIRA_TOKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	routing := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	service := &connectors.Service{Root: root, Store: runtimeStore, Router: routing, Factory: func(spec.Source) (connectors.Connector, error) { return studioConnector{}, nil }, Now: func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }}
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectRoot: root, Store: runtimeStore, Events: routing, Connectors: service})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/sources", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "jira-main") {
		t.Fatalf("list=%d %s", response.Code, response.Body.String())
	}
	for _, action := range []string{"test", "poll"} {
		response = eventAPIPost(t, handler, cookie, csrf, "/api/v1/sources/jira-main/"+action, `{}`)
		if response.Code != http.StatusOK {
			t.Fatalf("%s=%d %s", action, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/sources/jira-main/attempts", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"succeeded"`) {
		t.Fatalf("diagnostics=%d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/sources/missing/attempts", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing diagnostics=%d %s", response.Code, response.Body.String())
	}
	response = eventAPIPost(t, handler, cookie, csrf, "/api/v1/sources/jira-main/pause", `{"paused":true}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "true") {
		t.Fatalf("pause=%d %s", response.Code, response.Body.String())
	}
	response = eventAPIPost(t, handler, cookie, csrf, "/api/v1/sources/jira-main/poll", `{}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("paused poll=%d %s", response.Code, response.Body.String())
	}
}

func TestSourceAPIUnavailableAndMalformed(t *testing.T) {
	handler, _ := NewHandler("example")
	cookie, csrf := studioSession(t, handler)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/sources", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("list=%d", response.Code)
	}
	response = eventAPIPost(t, handler, cookie, csrf, "/api/v1/sources/x/test", `{}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("test=%d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/sources/x/attempts", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("diagnostics=%d", response.Code)
	}
}

type studioConnector struct{}

func (studioConnector) Test(context.Context) error { return nil }
func (studioConnector) Poll(context.Context, string, time.Time) (connectors.Batch, error) {
	return connectors.Batch{Cursor: "cursor"}, nil
}
