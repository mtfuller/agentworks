package harness

import (
	"encoding/json"
	"strings"

	"github.com/mtfuller/agentworks/internal/store"
)

const (
	ConclusionOpen  = "<agentworks-conclusion>"
	ConclusionClose = "</agentworks-conclusion>"
)

const ConclusionInstruction = `Before your final prose, emit exactly one JSON object between <agentworks-conclusion> and </agentworks-conclusion>. It must contain arrays named completed, files_changed, commands_run, checks, external_actions, outstanding_work, and suggested_memory. Use empty arrays when none apply. suggested_memory contains concise durable facts worth proposing to shared Markdown memory.`

// ExtractConclusion selects the last complete marker so partial streaming
// records and earlier examples cannot override the final handoff.
func ExtractConclusion(text string) (store.RunConclusion, bool) {
	end := strings.LastIndex(text, ConclusionClose)
	if end < 0 {
		return store.RunConclusion{}, false
	}
	start := strings.LastIndex(text[:end], ConclusionOpen)
	if start < 0 {
		return store.RunConclusion{}, false
	}
	start += len(ConclusionOpen)
	var conclusion store.RunConclusion
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(text[start:end])))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&conclusion) != nil {
		return store.RunConclusion{}, false
	}
	conclusion.Normalize()
	return conclusion, true
}
