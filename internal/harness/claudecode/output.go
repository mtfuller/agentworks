package claudecode

import (
	"bytes"
	"encoding/json"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

var privateOutputKeys = map[string]struct{}{
	"agents":                {},
	"apiKeySource":          {},
	"cwd":                   {},
	"id":                    {},
	"messaging_socket_path": {},
	"parent_tool_use_id":    {},
	"plugins":               {},
	"request_id":            {},
	"session_id":            {},
	"signature":             {},
	"skills":                {},
	"slash_commands":        {},
	"tool_use_id":           {},
	"uuid":                  {},
}

// jsonlFilter retains Claude's stream-json protocol while removing local paths,
// session identifiers, and user-level configuration inventories before the
// worker persists output.
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
