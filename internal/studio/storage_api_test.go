package studio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimestorage "github.com/mtfuller/agentworks/internal/storage"
	"github.com/mtfuller/agentworks/internal/store"
)

func TestRunPinAndDeleteDetailAPI(t *testing.T) {
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	event := store.Event{RecordID: "event-1", Source: "manual", ExternalID: "one", Type: "request", OccurredAt: now, ReceivedAt: now, Data: json.RawMessage(`{"prompt":"raw"}`), RawData: json.RawMessage(`{"prompt":"raw"}`)}
	run := store.Run{ID: "run-1", IdempotencyKey: "one", Agent: "builder", Workspace: "workspace", Harness: "fake", State: store.RunSucceeded, CreatedAt: now, UpdatedAt: now}
	if _, _, err := runtimeStore.IngestAndRoute(context.Background(), event, run, "test"); err != nil {
		t.Fatal(err)
	}
	manager := &runtimestorage.Manager{Store: runtimeStore, LogRoot: filepath.Join(t.TempDir(), "logs"), Limit: 1_000_000}
	handler, err := NewHandlerWithConfig(HandlerConfig{Store: runtimeStore, Storage: manager})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf := studioSession(t, handler)

	response := eventAPIPost(t, handler, cookie, csrf, "/api/v1/runs/run-1/pin", `{"pinned":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("pin=%d %s", response.Code, response.Body.String())
	}
	response = storageDelete(t, handler, cookie, csrf, "/api/v1/runs/run-1/detail")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("delete pinned=%d %s", response.Code, response.Body.String())
	}
	response = eventAPIPost(t, handler, cookie, csrf, "/api/v1/runs/run-1/pin", `{"pinned":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("unpin=%d %s", response.Code, response.Body.String())
	}
	response = storageDelete(t, handler, cookie, csrf, "/api/v1/runs/run-1/detail")
	if response.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", response.Code, response.Body.String())
	}
	stored, err := runtimeStore.GetEvent(context.Background(), "event-1")
	if err != nil || string(stored.Data) != `{"pruned":true}` {
		t.Fatalf("event=%#v err=%v", stored, err)
	}
}

func storageDelete(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodDelete, "http://localhost"+path, strings.NewReader(""))
	request.Header.Set("Origin", "http://localhost")
	request.Header.Set("X-AgentWorks-CSRF", csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
