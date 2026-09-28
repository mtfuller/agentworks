package studio

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	awworkspace "github.com/mtfuller/agentworks/internal/workspace"
)

const maxWorkspaceConfirmBytes = 16 << 10

func serveConfirmWorkspace(writer http.ResponseWriter, request *http.Request, registry *awworkspace.Registry) {
	if registry == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "ad hoc workspaces are unavailable"})
		return
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxWorkspaceConfirmBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Path string `json:"path"`
	}
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(writer, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body exceeds 16 KiB"})
			return
		}
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request JSON: " + err.Error()})
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	binding, err := registry.Bind(input.Path, true)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// The canonical path is returned only to the local browser for review. The
	// alias, not this path, is stored in durable events and runs.
	writeJSON(writer, http.StatusOK, map[string]any{
		"alias": binding.Alias, "path": binding.Path, "confirmed_for_write": binding.Confirmed,
	})
}
