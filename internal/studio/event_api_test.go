package studio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/store"
)

func TestEventAPIIngestPreviewInspectAndDisable(t *testing.T) {
	root, runtimeStore := eventAPIFixture(t)
	events := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectRoot: root, Store: runtimeStore, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)

	routes := httptest.NewRecorder()
	handler.ServeHTTP(routes, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/routes", nil))
	if routes.Code != http.StatusOK || !strings.Contains(routes.Body.String(), `"passed":true`) {
		t.Fatalf("routes status=%d body=%s", routes.Code, routes.Body.String())
	}

	body := `{"specversion":"1.0","id":"manual-1","source":"manual","type":"work.requested","data":{"project":"PLATFORM","token":"secret"}}`
	preview := eventAPIPost(t, handler, cookie, csrf, "/api/v1/routes/preview", body)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"selected":"manual-work"`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	ingested := eventAPIPost(t, handler, cookie, csrf, "/api/v1/event-ingest", body)
	if ingested.Code != http.StatusAccepted || strings.Contains(ingested.Body.String(), "secret") {
		t.Fatalf("ingest status=%d body=%s", ingested.Code, ingested.Body.String())
	}
	var result router.Result
	if err := json.Unmarshal(ingested.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Run == nil || result.Event.Status != store.EventRouted {
		t.Fatalf("result=%#v", result)
	}

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/inbox/"+result.Event.RecordID, nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"selected":"manual-work"`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	disabled := eventAPIPost(t, handler, cookie, csrf, "/api/v1/routes/manual-work/enabled", `{"enabled":false}`)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disabled.Code, disabled.Body.String())
	}
	second := eventAPIPost(t, handler, cookie, csrf, "/api/v1/event-ingest", strings.Replace(body, "manual-1", "manual-2", 1))
	if second.Code != http.StatusAccepted || !strings.Contains(second.Body.String(), `"status":"unrouted"`) {
		t.Fatalf("disabled ingest status=%d body=%s", second.Code, second.Body.String())
	}
	var unrouted router.Result
	if err := json.Unmarshal(second.Body.Bytes(), &unrouted); err != nil {
		t.Fatal(err)
	}
	if response := eventAPIPost(t, handler, cookie, csrf, "/api/v1/routes/manual-work/enabled", `{"enabled":true}`); response.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", response.Code, response.Body.String())
	}
	reevaluated := eventAPIPost(t, handler, cookie, csrf, "/api/v1/inbox/"+unrouted.Event.RecordID+"/reevaluate", `{}`)
	if reevaluated.Code != http.StatusOK || !strings.Contains(reevaluated.Body.String(), `"status":"routed"`) {
		t.Fatalf("reevaluate status=%d body=%s", reevaluated.Code, reevaluated.Body.String())
	}
	replay := eventAPIPost(t, handler, cookie, csrf, "/api/v1/inbox/"+result.Event.RecordID+"/replay", `{"replay_id":"manual-1:replay"}`)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"replay_of":"`+result.Event.RecordID+`"`) {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	tieBody := `{"id":"tie-1","source":"manual","type":"tie.requested","data":{}}`
	tied := eventAPIPost(t, handler, cookie, csrf, "/api/v1/event-ingest", tieBody)
	if tied.Code != http.StatusAccepted || !strings.Contains(tied.Body.String(), "highest priority tied") {
		t.Fatalf("tie status=%d body=%s", tied.Code, tied.Body.String())
	}
	if err := json.Unmarshal(tied.Body.Bytes(), &unrouted); err != nil {
		t.Fatal(err)
	}
	manual := eventAPIPost(t, handler, cookie, csrf, "/api/v1/inbox/"+unrouted.Event.RecordID+"/route", `{"route":"tie-b"}`)
	if manual.Code != http.StatusOK || !strings.Contains(manual.Body.String(), "manually selected route") {
		t.Fatalf("manual route status=%d body=%s", manual.Code, manual.Body.String())
	}

	inbox := httptest.NewRecorder()
	handler.ServeHTTP(inbox, httptest.NewRequest(http.MethodGet, "http://localhost/api/v1/inbox", nil))
	if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), `"events"`) {
		t.Fatalf("inbox status=%d body=%s", inbox.Code, inbox.Body.String())
	}
}

func TestEventAPIFailsClosedWithoutRouter(t *testing.T) {
	handler, err := NewHandler("example")
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	response := eventAPIPost(t, handler, cookie, csrf, "/api/v1/routes/missing/enabled", `{"enabled":true}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/v1/event-ingest", "/api/v1/routes/preview", "/api/v1/inbox/missing/route", "/api/v1/inbox/missing/replay", "/api/v1/inbox/missing/reevaluate"} {
		response = eventAPIPost(t, handler, cookie, csrf, path, `{}`)
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/inbox", "/api/v1/inbox/missing", "/api/v1/routes"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestEventAPIRejectsMalformedRequestsAndActions(t *testing.T) {
	root, runtimeStore := eventAPIFixture(t)
	events := &router.Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	handler, err := NewHandlerWithConfig(HandlerConfig{ProjectRoot: root, Store: runtimeStore, Events: events})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)
	for _, test := range []struct {
		path, body string
		want       int
	}{
		{"/api/v1/event-ingest", `{`, http.StatusBadRequest},
		{"/api/v1/event-ingest", `{}`, http.StatusBadRequest},
		{"/api/v1/routes/preview", `{"id":"x","source":"manual","type":"x","data":{}} {}`, http.StatusBadRequest},
		{"/api/v1/routes/missing/enabled", `{`, http.StatusBadRequest},
		{"/api/v1/routes/missing/enabled", `{"enabled":true}`, http.StatusNotFound},
		{"/api/v1/inbox/missing/route", `{}`, http.StatusBadRequest},
		{"/api/v1/inbox/missing/replay", `{}`, http.StatusBadRequest},
		{"/api/v1/inbox/missing/reevaluate", `{}`, http.StatusNotFound},
	} {
		response := eventAPIPost(t, handler, cookie, csrf, test.path, test.body)
		if response.Code != test.want {
			t.Errorf("POST %s status=%d want=%d body=%s", test.path, response.Code, test.want, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/event-ingest", strings.NewReader(`{}`))
	request.Header.Set("Origin", "http://localhost")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status=%d body=%s", response.Code, response.Body.String())
	}
}

func eventAPIFixture(t *testing.T) (string, *store.Store) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	files := map[string]string{
		"agentworks.yaml":               "format: 2\nname: events\nteams: [engineering]\nworkspaces: [product]\n",
		"agentworks.local.yaml":         "workspaces:\n  product:\n    path: " + workspace + "\n",
		"teams/engineering/team.yaml":   "name: engineering\nagents: [implementer]\ndefault_agent: implementer\n",
		"agents/implementer/AGENT.md":   "---\nname: implementer\nmax_permission: readonly\n---\nInspect.\n",
		"routes/manual-work/route.yaml": "name: manual-work\npriority: 20\nwhen:\n  source: manual\n  type: work.requested\n  fields:\n    project: PLATFORM\ninvoke:\n  team: engineering\n  workspace: product\n  harness: fake\n  permission: readonly\ntests:\n  - name: platform request\n    event:\n      source: manual\n      type: work.requested\n      data:\n        project: PLATFORM\n    expect: manual-work\n",
		"routes/tie-a/route.yaml":       "name: tie-a\npriority: 5\nwhen:\n  type: tie.requested\ninvoke:\n  team: engineering\n  workspace: product\n  harness: fake\n",
		"routes/tie-b/route.yaml":       "name: tie-b\npriority: 5\nwhen:\n  type: tie.requested\ninvoke:\n  team: engineering\n  workspace: product\n  harness: fake\n",
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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

func eventAPIPost(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
