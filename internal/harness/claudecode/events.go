package claudecode

import (
	"encoding/json"

	"github.com/mtfuller/agentworks/internal/harness"
)

// NormalizeLine converts one redacted Claude stream-json record to the stable
// AgentWorks event vocabulary. False means the record is protocol noise.
func NormalizeLine(line []byte) (harness.Event, bool) {
	var envelope struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Message struct {
			Content []struct {
				Type    string `json:"type"`
				Name    string `json:"name"`
				Text    string `json:"text"`
				IsError bool   `json:"is_error"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return harness.Event{Kind: harness.EventDiagnostic, Text: "invalid Claude stream record"}, true
	}
	switch envelope.Type {
	case "system":
		if envelope.Subtype == "init" {
			return harness.Event{Kind: harness.EventStarted}, true
		}
	case "assistant":
		for _, content := range envelope.Message.Content {
			if content.Type == "tool_use" {
				return harness.Event{Kind: harness.EventToolRequested, Tool: content.Name}, true
			}
			if content.Type == "text" && content.Text != "" {
				return harness.Event{Kind: harness.EventText, Text: content.Text}, true
			}
		}
	case "user":
		for _, content := range envelope.Message.Content {
			if content.Type == "tool_result" {
				return harness.Event{Kind: harness.EventToolCompleted, Success: harness.Success(!content.IsError)}, true
			}
		}
	case "result":
		return harness.Event{Kind: harness.EventCompleted, Text: envelope.Result, Success: harness.Success(!envelope.IsError)}, true
	}
	return harness.Event{}, false
}
