package githubcopilot

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/harness"
)

func TestNormalizeLineCoversPortableCopilotEvents(t *testing.T) {
	tests := []struct {
		line string
		kind harness.EventKind
		tool string
	}{
		{`{"type":"session.tools_updated","data":{}}`, harness.EventStarted, ""},
		{`{"type":"assistant.message_delta","data":{"deltaContent":"working"}}`, harness.EventText, ""},
		{`{"type":"assistant.message","data":{"toolRequests":[{"name":"create"}]}}`, harness.EventToolRequested, "create"},
		{`{"type":"assistant.message","data":{"content":"done"}}`, harness.EventText, ""},
		{`{"type":"tool.execution_start","data":{"toolName":"bash"}}`, harness.EventToolRequested, "bash"},
		{`{"type":"tool.execution_complete","data":{"toolName":"create","success":true}}`, harness.EventToolCompleted, "create"},
		{`{"type":"tool.execution_complete","data":{"toolName":"bash","error":{"message":"failed"}}}`, harness.EventToolCompleted, "bash"},
		{`{"type":"result","exitCode":0}`, harness.EventCompleted, ""},
		{`not-json`, harness.EventDiagnostic, ""},
	}
	for _, test := range tests {
		event, ok := NormalizeLine([]byte(test.line))
		if !ok || event.Kind != test.kind || event.Tool != test.tool {
			t.Errorf("NormalizeLine(%s) = %#v, %v", test.line, event, ok)
		}
	}
	if _, ok := NormalizeLine([]byte(`{"type":"session.info","data":{}}`)); ok {
		t.Fatal("protocol noise was normalized")
	}
}
