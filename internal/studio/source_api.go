package studio

import (
	"encoding/json"
	"net/http"

	"github.com/mtfuller/agentworks/internal/connectors"
	"github.com/mtfuller/agentworks/internal/store"
)

func serveSources(writer http.ResponseWriter, request *http.Request, service *connectors.Service) {
	if service == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "connectors are unavailable"})
		return
	}
	definitions, issues, statuses, err := service.Definitions(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "load connectors"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"definitions": definitions, "issues": issues, "sources": statuses})
}

func serveSourceAction(writer http.ResponseWriter, request *http.Request, service *connectors.Service, action string) {
	if service == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "connectors are unavailable"})
		return
	}
	name := request.PathValue("sourceName")
	var value any = map[string]any{"source": name}
	var err error
	switch action {
	case "pause":
		var input struct {
			Paused bool `json:"paused"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&input); decodeErr != nil || requireJSONEnd(decoder) != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid source update"})
			return
		}
		err = service.SetPaused(request.Context(), name, input.Paused)
		value = map[string]any{"source": name, "paused": input.Paused}
	case "test":
		err = service.Test(request.Context(), name)
	case "poll":
		value, err = service.PollNow(request.Context(), name)
	}
	if err != nil {
		status := http.StatusBadRequest
		if err == store.ErrNotFound {
			status = http.StatusNotFound
		}
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func serveSourceAttempts(writer http.ResponseWriter, request *http.Request, service *connectors.Service) {
	if service == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "connectors are unavailable"})
		return
	}
	name := request.PathValue("sourceName")
	if _, err := service.Store.Source(request.Context(), name); err != nil {
		writeStoreError(writer, err, "load source diagnostics")
		return
	}
	attempts, err := service.Store.ListSourcePollAttempts(request.Context(), name, 100)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "load source diagnostics"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"source": name, "attempts": attempts})
}
