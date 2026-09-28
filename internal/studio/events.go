package studio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/store"
)

const runtimeEventBatchSize = 250

func serveRuntimeEvents(writer http.ResponseWriter, request *http.Request, projectName string, runtimeStore *store.Store) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	cursor, err := runtimeEventCursor(request)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	payload, _ := json.Marshal(map[string]any{"project": projectName, "after": cursor})
	fmt.Fprintf(writer, "event: studio.ready\ndata: %s\n\n", payload)
	flusher.Flush()

	poll := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		if runtimeStore != nil {
			events, err := runtimeStore.ListRuntimeEventsAfter(request.Context(), cursor, runtimeEventBatchSize)
			if err != nil {
				fmt.Fprintf(writer, "event: studio.error\ndata: {\"error\":\"runtime event journal unavailable\"}\n\n")
				flusher.Flush()
				return
			}
			for _, event := range events {
				data, _ := json.Marshal(event)
				fmt.Fprintf(writer, "id: %d\nevent: runtime.event\ndata: %s\n\n", event.Sequence, data)
				cursor = event.Sequence
			}
			if len(events) != 0 {
				flusher.Flush()
				if len(events) == runtimeEventBatchSize {
					continue
				}
			}
		}
		select {
		case <-request.Context().Done():
			return
		case <-poll.C:
		case now := <-heartbeat.C:
			fmt.Fprintf(writer, "event: heartbeat\ndata: {\"time\":%q}\n\n", now.UTC().Format(time.RFC3339Nano))
			flusher.Flush()
		}
	}
}

func runtimeEventCursor(request *http.Request) (int64, error) {
	raw := strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	if raw == "" {
		raw = strings.TrimSpace(request.URL.Query().Get("after"))
	}
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0, fmt.Errorf("Last-Event-ID must be a non-negative integer")
	}
	return cursor, nil
}
