package artifact

import (
	"encoding/json"
	"fmt"
)

// HookHandler is one thing a hook does: run Command when Event fires, optionally
// only when Matcher matches (a regex for tool events on most vendors), giving up
// after Timeout seconds. A hook artifact declares handlers either explicitly
// under `handlers:` or, for the common single-command case, with the
// `events:` + `command:` shorthand, which expands to one handler per event.
//
// Every target that has hooks supports both Matcher and Timeout, so nothing
// here is dropped on export; Timeout is always seconds in AgentWorks and is
// converted to whatever unit a vendor wants (Gemini CLI uses milliseconds).
type HookHandler struct {
	Event   string `json:"event" yaml:"event"`
	Matcher string `json:"matcher,omitempty" yaml:"matcher,omitempty"`
	Command string `json:"command" yaml:"command"`
	Timeout int    `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// HookHandlers returns a hook artifact's handlers, expanding the
// `events:` + `command:` shorthand. It returns (nil, nil) for a hook that
// declares neither -- the state a fresh scaffold is in -- and an error for an
// inconsistent declaration: shorthand and `handlers:` together, only one half
// of the shorthand, or a handler missing its event or command.
func (a *Artifact) HookHandlers() ([]HookHandler, error) {
	events := a.ExtraStringSlice("events")
	command := a.ExtraString("command")
	rawHandlers, hasHandlers := a.Extra["handlers"]
	explicit := hasHandlers && !isEmptyList(rawHandlers)

	switch {
	case explicit && (len(events) > 0 || command != ""):
		return nil, fmt.Errorf("%s: set either \"handlers\" or the \"events\" + \"command\" shorthand, not both", a.Dir)
	case explicit:
		return decodeHandlers(a.Dir, rawHandlers)
	case (len(events) > 0) != (command != ""):
		return nil, fmt.Errorf("%s: \"events\" and \"command\" must be set together (a hook needs both to do anything)", a.Dir)
	case len(events) == 0:
		return nil, nil
	}

	handlers := make([]HookHandler, len(events))
	for i, e := range events {
		handlers[i] = HookHandler{Event: e, Command: command}
	}
	return handlers, nil
}

func isEmptyList(v any) bool {
	switch l := v.(type) {
	case nil:
		return true
	case []any:
		return len(l) == 0
	case []map[string]any:
		return len(l) == 0
	case []HookHandler:
		return len(l) == 0
	}
	return false
}

// decodeHandlers converts a frontmatter `handlers:` value into HookHandlers
// by way of JSON, which accepts whatever shape YAML (or a test) produced.
func decodeHandlers(dir string, raw any) ([]HookHandler, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: \"handlers\" is not a list of handlers: %w", dir, err)
	}
	var handlers []HookHandler
	if err := json.Unmarshal(data, &handlers); err != nil {
		return nil, fmt.Errorf("%s: \"handlers\" must be a list of {event, matcher, command, timeout}: %w", dir, err)
	}
	for i, h := range handlers {
		switch {
		case h.Event == "":
			return nil, fmt.Errorf("%s: handlers[%d] has no \"event\"", dir, i)
		case h.Command == "":
			return nil, fmt.Errorf("%s: handlers[%d] (%s) has no \"command\"", dir, i, h.Event)
		case h.Timeout < 0:
			return nil, fmt.Errorf("%s: handlers[%d] (%s) has a negative \"timeout\"", dir, i, h.Event)
		}
	}
	return handlers, nil
}
