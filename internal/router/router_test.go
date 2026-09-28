package router

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

func TestPreviewAndDispatchUseSameOneWinnerEvaluator(t *testing.T) {
	root, runtimeStore := routingFixture(t)
	writeRoute(t, root, "primary", 100)
	writeRoute(t, root, "secondary", 10)
	service := &Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	envelope := Envelope{ID: "delivery-1", Source: "manual", Type: "work.requested", Subject: "ABC-1", Time: time.Now().UTC(), Data: json.RawMessage(`{"project":"PLATFORM","workspace":"/tmp/untrusted"}`), RawData: json.RawMessage(`{"project":"PLATFORM","token":"do-not-store"}`)}
	_, preview, err := service.Preview(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Selected != "primary" {
		t.Fatalf("preview=%#v", preview)
	}
	result, err := service.Ingest(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision.Selected != preview.Selected || result.Run == nil || result.Run.Workspace != "product" {
		t.Fatalf("result=%#v", result)
	}
	if strings.Contains(string(result.Event.RawData), "do-not-store") || strings.Contains(string(result.Event.Data), "/tmp/untrusted") && result.Run.Workspace != "product" {
		t.Fatalf("unsafe event=%s run=%#v", result.Event.RawData, result.Run)
	}
	duplicate, err := service.Ingest(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Created || duplicate.Event.RoutedRunID != result.Run.ID {
		t.Fatalf("duplicate=%#v", duplicate)
	}
	runs, err := runtimeStore.ListRuns(context.Background(), 100)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
}

func TestTiedRoutesStayUnroutedUntilManualSelection(t *testing.T) {
	root, runtimeStore := routingFixture(t)
	writeRoute(t, root, "alpha", 50)
	writeRoute(t, root, "beta", 50)
	write(t, filepath.Join(root, "routes", "broken", "route.yaml"), "name: broken\nunknown: true\n")
	service := &Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	result, err := service.Ingest(context.Background(), Envelope{ID: "tie-1", Source: "manual", Type: "work.requested", Time: time.Now().UTC(), Data: json.RawMessage(`{"project":"PLATFORM"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Event.Status != store.EventUnrouted || result.Run != nil || result.Decision.Reason != "highest priority tied" || len(result.Decision.Issues) != 1 {
		t.Fatalf("result=%#v", result)
	}
	result, err = service.RouteNow(context.Background(), result.Event.RecordID, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if result.Run == nil || result.Event.Status != store.EventRouted {
		t.Fatalf("manual result=%#v", result)
	}
}

func TestDisableReevaluateAndReplay(t *testing.T) {
	root, runtimeStore := routingFixture(t)
	writeRoute(t, root, "primary", 100)
	service := &Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	if _, _, _, err := service.Definitions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.SetRouteEnabled(context.Background(), "primary", false); err != nil {
		t.Fatal(err)
	}
	first, err := service.Ingest(context.Background(), Envelope{ID: "disabled-1", Source: "manual", Type: "work.requested", Time: time.Now().UTC(), Data: json.RawMessage(`{"project":"PLATFORM"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if first.Event.Status != store.EventUnrouted {
		t.Fatalf("first=%#v", first)
	}
	if err := service.SetRouteEnabled(context.Background(), "primary", true); err != nil {
		t.Fatal(err)
	}
	routed, err := service.Reevaluate(context.Background(), first.Event.RecordID)
	if err != nil || routed.Run == nil {
		t.Fatalf("reevaluate=%#v err=%v", routed, err)
	}
	replayed, err := service.Replay(context.Background(), first.Event.RecordID, "disabled-1:replay-1")
	if err != nil || replayed.Run == nil || replayed.Event.ReplayOf != first.Event.RecordID {
		t.Fatalf("replay=%#v err=%v", replayed, err)
	}
}

func TestNormalizeRejectsIdentifiersAndRedactsNestedSecrets(t *testing.T) {
	if _, err := Normalize(Envelope{ID: "../bad", Source: "manual", Type: "x", Data: json.RawMessage(`{}`)}, time.Now()); err == nil {
		t.Fatal("unsafe id accepted")
	}
	event, err := Normalize(Envelope{ID: "ok", Source: "manual", Type: "x", Data: json.RawMessage(`{"nested":{"password":"secret","api_key":"key","privateKey":"private","credential_id":"credential"}}`)}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{`"password":"secret"`, `"api_key":"key"`, `"privateKey":"private"`, `"credential_id":"credential"`} {
		if strings.Contains(string(event.Data), sensitive) {
			t.Fatalf("data=%s", event.Data)
		}
	}
	if strings.Count(string(event.Data), "REDACTED") != 4 {
		t.Fatalf("data=%s", event.Data)
	}
}

func TestRecoverIngestedDispatchesCommittedConnectorEvents(t *testing.T) {
	root, runtimeStore := routingFixture(t)
	writeRoute(t, root, "primary", 100)
	service := &Service{Root: root, Store: runtimeStore, Harnesses: []string{"fake"}}
	now := time.Now().UTC()
	for _, id := range []string{"delivery-1", "delivery-2"} {
		event, err := Normalize(Envelope{ID: id, Source: "manual", Type: "work.requested", Time: now, Data: json.RawMessage(`{"project":"PLATFORM"}`)}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, created, err := runtimeStore.IngestEvent(context.Background(), event); err != nil || !created {
			t.Fatalf("created=%v err=%v", created, err)
		}
	}
	if err := service.RecoverIngested(context.Background()); err != nil {
		t.Fatal(err)
	}
	runs, err := runtimeStore.ListRuns(context.Background(), 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
	if err := service.RecoverIngested(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := service.DispatchStored(context.Background(), runs[0].EventRecordID)
	if err != nil || result.Event.Status != store.EventRouted || result.Run != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func routingFixture(t *testing.T) (string, *store.Store) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	for _, dir := range []string{workspace, filepath.Join(root, "teams", "engineering"), filepath.Join(root, "agents", "implementer")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(root, "agentworks.yaml"), "format: 2\nname: routes\nteams: [engineering]\nworkspaces: [product]\n")
	write(t, filepath.Join(root, "agentworks.local.yaml"), "workspaces:\n  product:\n    path: "+workspace+"\n")
	write(t, filepath.Join(root, "teams", "engineering", "team.yaml"), "name: engineering\nagents: [implementer]\ndefault_agent: implementer\n")
	write(t, filepath.Join(root, "agents", "implementer", "AGENT.md"), "---\nname: implementer\nmax_permission: readwrite\n---\nImplement safely.\n")
	runtimeStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtimeStore.Close() })
	return root, runtimeStore
}
func writeRoute(t *testing.T, root, name string, priority int) {
	t.Helper()
	write(t, filepath.Join(root, "routes", name, "route.yaml"), "name: "+name+"\npriority: "+fmt.Sprint(priority)+"\nwhen:\n  source: manual\n  type: work.requested\n  fields:\n    project: PLATFORM\ninvoke:\n  team: engineering\n  agent: implementer\n  workspace: product\n  harness: fake\n  permission: readonly\n")
}
func write(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
