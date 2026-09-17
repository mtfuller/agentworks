package inspector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mtfuller/agentworks/internal/mcpclient"
)

// maxLogLines caps the raw traffic/stderr transcript kept in memory --
// long-running inspection sessions shouldn't grow this without bound.
// Oldest lines are dropped first (see Model.appendLogLine).
const maxLogLines = 2000

// formatTraceLine renders one JSON-RPC line for the log pane: a
// timestamp, a directional arrow ("->" client-to-server, "<-" the
// reverse), and the line itself, re-indented if it's valid JSON (it
// always will be for real JSON-RPC traffic -- prettyJSON's plain-text
// fallback exists for the rare malformed line, not the common case).
func formatTraceLine(direction string, raw []byte) string {
	arrow := "<-"
	if direction == "->" {
		arrow = "->"
	}
	return fmt.Sprintf("%s %s %s", time.Now().Format("15:04:05.000"), arrow, prettyJSON(string(raw)))
}

// formatStderrLine renders one line of the server process's captured
// stderr for the log pane, tagged so it's not mistaken for JSON-RPC
// traffic.
func formatStderrLine(line string) string {
	return fmt.Sprintf("%s [stderr] %s", time.Now().Format("15:04:05.000"), line)
}

// prettyJSON re-indents s if it parses as JSON, and returns it unchanged
// otherwise -- used for both the log pane's wire traffic and a "text"
// content block in a call result, since a tool's JSON response is far
// more legible reformatted than as the single compact line MCP actually
// sends over the wire.
func prettyJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// renderCallResult formats a completed call for the detail viewport:
// each content block (pretty-printed if it's JSON text), an isError
// banner if the tool itself reported failure, and how long the call
// took. A protocol-level error (err != nil, no result at all) is
// rendered distinctly from a tool-level one.
func renderCallResult(record callRecord) string {
	var out string
	if !record.at.IsZero() {
		out += fmt.Sprintf("Called at %s (%s)\n\n", record.at.Format("15:04:05"), record.duration.Round(time.Millisecond))
	}

	if record.err != nil {
		out += errorStyle.Render(fmt.Sprintf("Error: %v", record.err))
		return out
	}

	if record.result == nil {
		out += "(no result)"
		return out
	}

	if record.result.IsError {
		out += errorStyle.Render("Tool reported an error:") + "\n\n"
	}
	if len(record.result.Content) == 0 {
		out += "(empty result)"
		return out
	}

	for i, block := range record.result.Content {
		if i > 0 {
			out += "\n\n"
		}
		out += renderContentBlock(block)
	}
	return out
}

func renderContentBlock(block mcpclient.ContentBlock) string {
	switch block.Type {
	case "text":
		return prettyJSON(block.Text)
	default:
		return fmt.Sprintf("[%s content -- see log pane for the raw response]", block.Type)
	}
}
