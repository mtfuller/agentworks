package githubcopilot

import (
	"bytes"
	"encoding/json"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

var privateOutputKeys = map[string]struct{}{
	"apiCallId":            {},
	"github_request_id":    {},
	"id":                   {},
	"interactionId":        {},
	"messageId":            {},
	"model_call_id":        {},
	"originatingMessageId": {},
	"parentAgentTaskId":    {},
	"parentId":             {},
	"request_id":           {},
	"sessionId":            {},
	"toolCallId":           {},
}

// jsonlFilter preserves Copilot's structured stream while removing opaque
// provider request material that is not required to render or audit a run.
func jsonlFilter() awprocess.Filter {
	var stdout bytes.Buffer
	return func(stream awprocess.Stream, chunk []byte) []byte {
		if stream != awprocess.Stdout {
			return chunk
		}
		_, _ = stdout.Write(chunk)
		var result bytes.Buffer
		for {
			line, err := stdout.ReadBytes('\n')
			if err != nil {
				// ReadBytes consumed the partial record; put it back until the
				// next chunk completes the JSONL line.
				_, _ = stdout.Write(line)
				break
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var value any
			if json.Unmarshal(line, &value) != nil {
				encoded, _ := json.Marshal(map[string]any{
					"type": "agentworks.unparsed-output", "data": map[string]any{"bytes": len(line)},
				})
				result.Write(encoded)
				result.WriteByte('\n')
				continue
			}
			redactPrivateOutput(value)
			encoded, _ := json.Marshal(value)
			result.Write(encoded)
			result.WriteByte('\n')
		}
		return result.Bytes()
	}
}

func redactPrivateOutput(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, private := privateOutputKeys[key]; private {
				delete(typed, key)
				continue
			}
			redactPrivateOutput(child)
		}
	case []any:
		for _, child := range typed {
			redactPrivateOutput(child)
		}
	}
}
