package harness

import "encoding/json"

// EventKind is AgentWorks' stable vocabulary for harness output. Vendor JSON
// stays in the raw attempt log; these events are the portable live/UI surface.
type EventKind string

const (
	EventStarted       EventKind = "started"
	EventText          EventKind = "text"
	EventToolRequested EventKind = "tool.requested"
	EventToolCompleted EventKind = "tool.completed"
	EventCompleted     EventKind = "completed"
	EventDiagnostic    EventKind = "diagnostic"
)

// Event is deliberately small. Data contains a redacted vendor fragment when
// portable fields cannot express useful diagnostics.
type Event struct {
	Kind    EventKind       `json:"kind"`
	Text    string          `json:"text,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Success *bool           `json:"success,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func Success(value bool) *bool { return &value }
