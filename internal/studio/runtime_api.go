package studio

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/memory"
	"github.com/mtfuller/agentworks/internal/monitors"
	runtimestorage "github.com/mtfuller/agentworks/internal/storage"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/worker"
)

type attemptResponse struct {
	ID         string         `json:"id"`
	Number     int            `json:"number"`
	WorkerID   string         `json:"worker_id"`
	ProcessID  int            `json:"process_id,omitempty"`
	State      store.RunState `json:"state"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	LogURL     string         `json:"log_url,omitempty"`
}

func serveRunDetail(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	run, err := runtimeStore.GetRun(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeStoreError(writer, err, "load run")
		return
	}
	attempts, err := runtimeStore.ListAttempts(request.Context(), run.ID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list attempts"})
		return
	}
	approvals, err := runtimeStore.ListApprovals(request.Context(), run.ID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list approvals"})
		return
	}
	conclusion, conclusionErr := runtimeStore.GetRunConclusion(request.Context(), run.ID)
	if conclusionErr != nil && !errors.Is(conclusionErr, store.ErrNotFound) {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "load structured conclusion"})
		return
	}
	proposals, err := runtimeStore.ListMemoryProposals(request.Context(), run.ID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list memory proposals"})
		return
	}
	result := make([]attemptResponse, 0, len(attempts))
	for _, attempt := range attempts {
		entry := attemptResponse{ID: attempt.ID, Number: attempt.Number, WorkerID: attempt.WorkerID,
			ProcessID: attempt.ProcessID, State: attempt.State, StartedAt: attempt.StartedAt, FinishedAt: attempt.FinishedAt,
			LogURL: "/api/v1/runs/" + run.ID + "/attempts/" + attempt.ID + "/log"}
		if len(attempt.Result) != 0 {
			_ = json.Unmarshal(attempt.Result, &entry.Result)
			if logValue, ok := entry.Result["log"].(map[string]any); ok {
				delete(logValue, "path")
			}
		}
		result = append(result, entry)
	}
	response := map[string]any{"run": run, "attempts": result, "approvals": approvals, "memory_proposals": proposals}
	if executionPlan, planErr := runtimeStore.GetExecutionPlan(request.Context(), run.ID); planErr == nil {
		response["execution_plan"] = executionPlan
	} else if !errors.Is(planErr, store.ErrNotFound) {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "load execution plan"})
		return
	}
	if conclusionErr == nil {
		response["structured_conclusion"] = conclusion
	}
	writeJSON(writer, http.StatusOK, response)
}

func serveOutcomes(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	values, err := runtimeStore.ListOutcomes(request.Context(), request.URL.Query().Get("work_item_id"), 200)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list outcomes"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"outcomes": values})
}

func serveCreateOutcome(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 128<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		ID            string `json:"id,omitempty"`
		Type          string `json:"type"`
		Subject       string `json:"subject,omitempty"`
		EventRecordID string `json:"event_record_id,omitempty"`
		RunID         string `json:"run_id,omitempty"`
		WorkItem      *struct {
			ID          string          `json:"id,omitempty"`
			Kind        string          `json:"kind"`
			ExternalKey string          `json:"external_key"`
			Title       string          `json:"title,omitempty"`
			State       string          `json:"state,omitempty"`
			Data        json.RawMessage `json:"data,omitempty"`
		} `json:"work_item,omitempty"`
		OccurredAt time.Time       `json:"occurred_at,omitempty"`
		Data       json.RawMessage `json:"data,omitempty"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid outcome JSON: " + err.Error()})
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if input.ID == "" {
		input.ID, _ = newRuntimeID("out")
	}
	workItemID := ""
	if input.WorkItem != nil {
		if input.WorkItem.ID == "" {
			input.WorkItem.ID, _ = newRuntimeID("work")
		}
		item, err := runtimeStore.UpsertWorkItem(request.Context(), store.WorkItem{ID: input.WorkItem.ID, Kind: input.WorkItem.Kind, ExternalKey: input.WorkItem.ExternalKey, Title: input.WorkItem.Title, State: input.WorkItem.State, Data: input.WorkItem.Data})
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		workItemID = item.ID
	}
	value, err := runtimeStore.RecordOutcome(request.Context(), store.Outcome{ID: input.ID, Type: input.Type, Subject: input.Subject, EventRecordID: input.EventRecordID, RunID: input.RunID, WorkItemID: workItemID, OccurredAt: input.OccurredAt, Data: input.Data})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = monitors.EvaluateAndStore(request.Context(), runtimeStore, time.Now().UTC())
	states, _ := runtimeStore.ListMonitorStates(request.Context())
	writeJSON(writer, http.StatusCreated, map[string]any{"outcome": value, "monitors": states})
}

func serveMonitors(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	_ = monitors.EvaluateAndStore(request.Context(), runtimeStore, time.Now().UTC())
	states, err := runtimeStore.ListMonitorStates(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list monitors"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"monitors": states})
}

func serveTimeline(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	entries, err := runtimeStore.WorkItemTimeline(request.Context(), request.PathValue("workItemID"))
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "load timeline"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"timeline": entries})
}

func serveMemoryDecision(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store, root string) {
	if runtimeStore == nil || strings.TrimSpace(root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "memory proposals are unavailable"})
		return
	}
	var input struct {
		Decision string `json:"decision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid decision"})
		return
	}
	var err error
	switch input.Decision {
	case "approved":
		err = memory.Apply(request.Context(), runtimeStore, root, request.PathValue("proposalID"))
	case "rejected":
		err = memory.Reject(request.Context(), runtimeStore, request.PathValue("proposalID"))
	default:
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "decision must be approved or rejected"})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrConflict) {
			status = http.StatusConflict
		}
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	proposal, _ := runtimeStore.GetMemoryProposal(request.Context(), request.PathValue("proposalID"))
	writeJSON(writer, http.StatusOK, map[string]any{"proposal": proposal})
}

func serveApprovalDecision(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request JSON: " + err.Error()})
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	decision := store.ApprovalState(input.Decision)
	approval, err := runtimeStore.DecideApproval(request.Context(), request.PathValue("approvalID"), decision, strings.TrimSpace(input.Reason), time.Now().UTC())
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
	writeJSON(writer, http.StatusOK, map[string]any{"approval": approval})
}

func serveCancelRun(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store, controller RunController) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	id := request.PathValue("runID")
	if controller != nil && controller.Cancel(id) {
		writeJSON(writer, http.StatusAccepted, map[string]any{"cancelled": true, "active": true})
		return
	}
	cancelled, err := runtimeStore.CancelPendingRun(request.Context(), id, time.Now().UTC())
	if err != nil {
		writeStoreError(writer, err, "cancel run")
		return
	}
	if !cancelled {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "run is not pending or active in this Studio process"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"cancelled": true, "active": false})
}

func servePinRun(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store) {
	if runtimeStore == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "runtime storage is unavailable"})
		return
	}
	var input struct {
		Pinned bool `json:"pinned"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid pin request"})
		return
	}
	if err := requireJSONEnd(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := runtimeStore.SetRunPinned(request.Context(), request.PathValue("runID"), input.Pinned, time.Now().UTC()); err != nil {
		writeStoreError(writer, err, "pin run")
		return
	}
	run, _ := runtimeStore.GetRun(request.Context(), request.PathValue("runID"))
	writeJSON(writer, http.StatusOK, map[string]any{"run": run})
}

func serveDeleteRunDetail(writer http.ResponseWriter, request *http.Request, manager *runtimestorage.Manager) {
	if manager == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "storage management is unavailable"})
		return
	}
	if err := manager.DeleteRunDetail(request.Context(), request.PathValue("runID")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, store.ErrConflict) {
			status = http.StatusConflict
		}
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"deleted": true, "retained": "summary"})
}

func serveAttemptLog(writer http.ResponseWriter, request *http.Request, runtimeStore *store.Store, root string) {
	if runtimeStore == nil || strings.TrimSpace(root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "run logs are unavailable"})
		return
	}
	attempts, err := runtimeStore.ListAttempts(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "list attempts"})
		return
	}
	known := false
	for _, attempt := range attempts {
		known = known || attempt.ID == request.PathValue("attemptID")
	}
	if !known {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "attempt not found"})
		return
	}
	data, err := os.ReadFile(worker.LogPath(root, request.PathValue("runID"), request.PathValue("attemptID")))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": "attempt log is not available yet"})
			return
		}
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "read attempt log"})
		return
	}
	writer.Header().Set("Content-Type", "application/x-ndjson")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write(data)
}

func writeStoreError(writer http.ResponseWriter, err error, fallback string) {
	status := http.StatusInternalServerError
	message := fallback
	if errors.Is(err, store.ErrNotFound) {
		status, message = http.StatusNotFound, "not found"
	}
	writeJSON(writer, status, map[string]string{"error": message})
}
