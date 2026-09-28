package claudecode

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/harness"
)

func TestNormalizeLineCoversPortableClaudeEvents(t *testing.T) {
	tests := []struct {
		line string
		kind harness.EventKind
		tool string
	}{
		{`{"type":"system","subtype":"init"}`, harness.EventStarted, ""},
		{`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`, harness.EventText, ""},
		{`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`, harness.EventToolRequested, "Bash"},
		{`{"type":"user","message":{"content":[{"type":"tool_result","is_error":false}]}}`, harness.EventToolCompleted, ""},
		{`{"type":"result","is_error":false,"result":"done"}`, harness.EventCompleted, ""},
		{`not-json`, harness.EventDiagnostic, ""},
	}
	for _, test := range tests {
		event, ok := NormalizeLine([]byte(test.line))
		if !ok || event.Kind != test.kind || event.Tool != test.tool {
			t.Errorf("NormalizeLine(%s) = %#v, %v", test.line, event, ok)
		}
	}
	if _, ok := NormalizeLine([]byte(`{"type":"rate_limit_event"}`)); ok {
		t.Fatal("protocol noise was normalized")
	}
}
