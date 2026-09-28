package studio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/store"
	awworkspace "github.com/mtfuller/agentworks/internal/workspace"
)

const maxManualRequestBytes = 128 << 10

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var errInvalidManualRequest = errors.New("invalid manual run request")

type manualRunRequest struct {
	RequestID  string          `json:"request_id"`
	Team       string          `json:"team"`
	Agent      string          `json:"agent,omitempty"`
	Workspace  string          `json:"workspace"`
	Harness    string          `json:"harness"`
	Permission spec.Permission `json:"permission"`
	Prompt     string          `json:"prompt"`
}

type manualRunData struct {
	Prompt              string          `json:"prompt"`
	Harness             string          `json:"harness"`
	Team                string          `json:"team"`
	Agent               string          `json:"agent"`
	Workspace           string          `json:"workspace"`
	RequestedPermission spec.Permission `json:"requested_permission"`
	EffectivePermission spec.Permission `json:"effective_permission"`
}

func serveCreateManualRun(writer http.ResponseWriter, request *http.Request, root string, runtimeStore *store.Store, harnesses []string, workspaces *awworkspace.Registry, wake func()) {
	if runtimeStore == nil || strings.TrimSpace(root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "manual runs are unavailable"})
		return
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxManualRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input manualRunRequest
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(writer, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body exceeds 128 KiB"})
			return
		}
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request JSON: " + err.Error()})
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	run, created, err := createManualRun(request.Context(), root, runtimeStore, input, harnesses, workspaces)
	if err != nil {
		status := http.StatusInternalServerError
		message := "create manual run"
		if errors.Is(err, errInvalidManualRequest) {
			status = http.StatusBadRequest
			message = err.Error()
		} else if errors.Is(err, store.ErrConflict) {
			status = http.StatusConflict
			message = err.Error()
		}
		writeJSON(writer, status, map[string]string{"error": message})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
		if wake != nil {
			wake()
		}
	}
	writeJSON(writer, status, map[string]any{"created": created, "run": run})
}

func createManualRun(ctx context.Context, root string, runtimeStore *store.Store, input manualRunRequest, harnesses []string, workspaces *awworkspace.Registry) (store.Run, bool, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Team = strings.TrimSpace(input.Team)
	input.Agent = strings.TrimSpace(input.Agent)
	input.Workspace = strings.TrimSpace(input.Workspace)
	input.Harness = strings.TrimSpace(input.Harness)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if !requestIDPattern.MatchString(input.RequestID) {
		return store.Run{}, false, invalidManual("request_id must be 1-128 URL-safe characters")
	}
	if input.Prompt == "" {
		return store.Run{}, false, invalidManual("prompt is required")
	}
	if len(input.Prompt) > 64<<10 {
		return store.Run{}, false, invalidManual("prompt exceeds 64 KiB")
	}
	if !containsString(harnesses, input.Harness) {
		return store.Run{}, false, invalidManual("harness %q is not enabled in Studio", input.Harness)
	}

	project, err := spec.LoadProject(root)
	if err != nil {
		return store.Run{}, false, err
	}
	if !containsString(project.Teams, input.Team) {
		return store.Run{}, false, invalidManual("team %q is not declared by the project", input.Team)
	}
	team, err := spec.LoadTeam(root, input.Team)
	if err != nil {
		return store.Run{}, false, err
	}
	if input.Agent == "" {
		input.Agent = team.DefaultAgent
	}
	if input.Agent == "" {
		return store.Run{}, false, invalidManual("team %q has no default agent; agent is required", input.Team)
	}
	if !containsString(team.Agents, input.Agent) {
		return store.Run{}, false, invalidManual("agent %q is not a member of team %q", input.Agent, input.Team)
	}
	agent, err := spec.LoadAgent(root, input.Agent)
	if err != nil {
		return store.Run{}, false, err
	}
	if !input.Permission.Valid() {
		return store.Run{}, false, invalidManual("permission %q is invalid", input.Permission)
	}
	effective, err := spec.EffectivePermission(input.Permission, agent.MaxPermission)
	if err != nil {
		return store.Run{}, false, err
	}
	if containsString(project.Workspaces, input.Workspace) {
		local, err := spec.LoadLocal(root)
		if err != nil {
			return store.Run{}, false, err
		}
		binding, ok := local.Workspaces[input.Workspace]
		if !ok {
			return store.Run{}, false, invalidManual("workspace %q has no local binding in %s", input.Workspace, spec.LocalFile)
		}
		info, err := os.Stat(binding.Path)
		if err != nil {
			return store.Run{}, false, invalidManual("workspace %q binding is unavailable: %v", input.Workspace, err)
		}
		if !info.IsDir() {
			return store.Run{}, false, invalidManual("workspace %q path is not a directory", input.Workspace)
		}
	} else {
		if workspaces == nil {
			return store.Run{}, false, invalidManual("workspace %q is not declared or confirmed for this Studio session", input.Workspace)
		}
		binding, ok := workspaces.Resolve(input.Workspace)
		if !ok {
			return store.Run{}, false, invalidManual("workspace %q is not declared or confirmed for this Studio session", input.Workspace)
		}
		if effective != spec.PermissionReadonly && !binding.Confirmed {
			return store.Run{}, false, invalidManual("ad hoc workspace requires confirmation before a write-capable run")
		}
	}
	payload, err := json.Marshal(manualRunData{
		Prompt: input.Prompt, Harness: input.Harness, Team: input.Team, Agent: input.Agent,
		Workspace: input.Workspace, RequestedPermission: input.Permission, EffectivePermission: effective,
	})
	if err != nil {
		return store.Run{}, false, fmt.Errorf("encode manual request: %w", err)
	}
	now := time.Now().UTC()
	eventID, err := newRuntimeID("evt")
	if err != nil {
		return store.Run{}, false, err
	}
	runID, err := newRuntimeID("run")
	if err != nil {
		return store.Run{}, false, err
	}
	event := store.Event{
		RecordID: eventID, Source: "manual", ExternalID: input.RequestID,
		Type: "agentworks.manual-run.requested", Subject: input.Team + "/" + input.Agent,
		OccurredAt: now, ReceivedAt: now, Data: payload,
	}
	run := store.Run{
		ID: runID, IdempotencyKey: "manual:" + input.RequestID, Team: input.Team,
		Agent: input.Agent, Workspace: input.Workspace, Harness: input.Harness,
		Permission: effective, State: store.RunPending, CreatedAt: now, UpdatedAt: now,
	}
	return runtimeStore.IngestAndRoute(ctx, event, run, "manual request")
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain one JSON document")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func newRuntimeID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("create runtime ID: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(bytes), nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func invalidManual(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidManualRequest, fmt.Sprintf(format, args...))
}
