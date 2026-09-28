package studio

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/store"
)

const maxEventRequestBytes = 1 << 20

func serveEventInbox(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "event inbox is unavailable"})
		return
	}
	events, err := runtimeStore.ListEvents(request.Context(), 200)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list events"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"events": events})
}
func serveEventDetail(writer http.ResponseWriter, request *http.Request, events *router.Service) {
	if events == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "event routing is unavailable"})
		return
	}
	event, err := events.Store.GetEvent(request.Context(), request.PathValue("eventID"))
	if err != nil {
		writeStoreError(writer, err, "load event")
		return
	}
	decision, err := events.PreviewEvent(request.Context(), event.RecordID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "preview event"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"event": event, "decision": decision})
}
func serveIngestEvent(writer http.ResponseWriter, request *http.Request, events *router.Service) {
	if events == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "event routing is unavailable"})
		return
	}
	var envelope router.Envelope
	if !decodeEventJSON(writer, request, &envelope) {
		return
	}
	result, err := events.Ingest(request.Context(), envelope)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusAccepted
	}
	writeJSON(writer, status, result)
}
func serveRoutePreview(writer http.ResponseWriter, request *http.Request, events *router.Service) {
	if events == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "event routing is unavailable"})
		return
	}
	var envelope router.Envelope
	if !decodeEventJSON(writer, request, &envelope) {
		return
	}
	event, decision, err := events.Preview(request.Context(), envelope)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"event": event, "decision": decision})
}

func serveEventAction(writer http.ResponseWriter, request *http.Request, events *router.Service, action string) {
	if events == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "event routing is unavailable"})
		return
	}
	var input struct {
		Route    string `json:"route,omitempty"`
		ReplayID string `json:"replay_id,omitempty"`
	}
	if request.ContentLength != 0 {
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid action JSON"})
			return
		}
	}
	var result router.Result
	var err error
	switch action {
	case "route":
		if strings.TrimSpace(input.Route) == "" {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "route is required"})
			return
		}
		result, err = events.RouteNow(request.Context(), request.PathValue("eventID"), input.Route)
	case "replay":
		if strings.TrimSpace(input.ReplayID) == "" {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "replay_id is required"})
			return
		}
		result, err = events.Replay(request.Context(), request.PathValue("eventID"), input.ReplayID)
	case "reevaluate":
		result, err = events.Reevaluate(request.Context(), request.PathValue("eventID"))
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, store.ErrConflict) {
			status = http.StatusConflict
		}
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func serveRoutes(writer http.ResponseWriter, request *http.Request, events *router.Service) {
	if events == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "routes are unavailable"})
		return
	}
	routes, issues, subscriptions, err := events.Definitions(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "load routes"})
		return
	}
	fixtures, err := events.TestFixtures(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "test route fixtures"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"routes": routes, "issues": issues, "subscriptions": subscriptions, "fixtures": fixtures})
}
func serveRouteEnabled(writer http.ResponseWriter, request *http.Request, events *router.Service) {
	if events == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "routes are unavailable"})
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid route update"})
		return
	}
	if err := events.SetRouteEnabled(request.Context(), request.PathValue("routeName"), input.Enabled); err != nil {
		writeStoreError(writer, err, "update route")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"route": request.PathValue("routeName"), "enabled": input.Enabled})
}

func decodeEventJSON(writer http.ResponseWriter, request *http.Request, value any) bool {
	if media := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); media != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxEventRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid event JSON: " + err.Error()})
		return false
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}
