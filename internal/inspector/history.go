package inspector

import (
	"fmt"
	"sort"
	"time"

	"github.com/mtfuller/agentworks/internal/mcpclient"
)

// callRecord is one completed "tools/call" this session, kept in
// Model.history so a user can review or compare past calls without
// re-invoking the tool -- selecting one from the history pane re-renders
// its result in the detail viewport (see Model.showHistoryEntry).
type callRecord struct {
	at       time.Time
	toolName string
	args     map[string]any
	result   *mcpclient.CallToolResult
	err      error
	duration time.Duration
}

// ok reports whether the call completed successfully at the protocol
// level and the tool itself didn't report an error (CallToolResult.IsError).
func (r callRecord) ok() bool {
	return r.err == nil && (r.result == nil || !r.result.IsError)
}

// historyItem adapts a callRecord for bubbles/list.
type historyItem struct {
	record callRecord
}

func (i historyItem) Title() string {
	status := "ok"
	if !i.record.ok() {
		status = "error"
	}
	return fmt.Sprintf("%s  %s  %s", i.record.at.Format("15:04:05"), i.record.toolName, status)
}

func (i historyItem) Description() string {
	return fmt.Sprintf("%s, %s", formatArgsSummary(i.record.args), i.record.duration.Round(time.Millisecond))
}

func (i historyItem) FilterValue() string {
	return i.record.toolName
}

// formatArgsSummary renders a call's arguments compactly for the history
// list -- full argument values already appear in the log pane's raw
// request line, so this only needs to be a recognizable summary, not a
// complete record. Keys are sorted rather than iterated in Go's
// randomized map order, so the same record's summary doesn't visibly
// reshuffle between renders.
func formatArgsSummary(args map[string]any) string {
	if len(args) == 0 {
		return "no arguments"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		if i >= 3 {
			out += "..."
			break
		}
		out += fmt.Sprintf("%s=%v", k, args[k])
	}
	return out
}
