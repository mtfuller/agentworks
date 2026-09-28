package githubcopilot

import (
	"encoding/json"

	"github.com/mtfuller/agentworks/internal/harness"
)

// NormalizeLine converts one redacted Copilot JSONL record to the stable
// AgentWorks event vocabulary. False means the record is protocol noise.
func NormalizeLine(line []byte) (harness.Event, bool) {
	var envelope struct {
		Type     string `json:"type"`
		ExitCode int    `json:"exitCode"`
		Data     struct {
			Content      string `json:"content"`
			DeltaContent string `json:"deltaContent"`
			ToolName     string `json:"toolName"`
			Success      *bool  `json:"success"`
			Error        any    `json:"error"`
			ToolRequests []struct {
				Name string `json:"name"`
			} `json:"toolRequests"`
		} `json:"data"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return harness.Event{Kind: harness.EventDiagnostic, Text: "invalid Copilot stream record"}, true
	}
	switch envelope.Type {
	case "session.tools_updated":
		return harness.Event{Kind: harness.EventStarted}, true
	case "assistant.message_delta":
		if envelope.Data.DeltaContent != "" {
			return harness.Event{Kind: harness.EventText, Text: envelope.Data.DeltaContent}, true
		}
	case "assistant.message":
		if len(envelope.Data.ToolRequests) != 0 {
			return harness.Event{Kind: harness.EventToolRequested, Tool: envelope.Data.ToolRequests[0].Name}, true
		}
		if envelope.Data.Content != "" {
			return harness.Event{Kind: harness.EventText, Text: envelope.Data.Content}, true
		}
	case "tool.execution_start":
		return harness.Event{Kind: harness.EventToolRequested, Tool: envelope.Data.ToolName}, true
	case "tool.execution_complete":
		success := envelope.Data.Error == nil
		if envelope.Data.Success != nil {
			success = *envelope.Data.Success
		}
		return harness.Event{Kind: harness.EventToolCompleted, Tool: envelope.Data.ToolName, Success: harness.Success(success)}, true
	case "result":
		return harness.Event{Kind: harness.EventCompleted, Success: harness.Success(envelope.ExitCode == 0)}, true
	}
	return harness.Event{}, false
}
