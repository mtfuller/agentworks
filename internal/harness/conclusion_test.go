package harness

import "testing"

func TestExtractConclusionUsesLastCompleteStrictRecord(t *testing.T) {
	text := ConclusionOpen + `{"completed":["old"]}` + ConclusionClose + "\n" +
		ConclusionOpen + `{"completed":["done"],"files_changed":[],"commands_run":[],"checks":[],"external_actions":[],"outstanding_work":["review"],"suggested_memory":["Prefer focused tests"]}` + ConclusionClose
	value, ok := ExtractConclusion(text)
	if !ok || len(value.Completed) != 1 || value.Completed[0] != "done" || value.SuggestedMemory[0] != "Prefer focused tests" {
		t.Fatalf("ExtractConclusion() = %#v, %v", value, ok)
	}
	if _, ok := ExtractConclusion(ConclusionOpen + `{"completed":[],"unknown":true}` + ConclusionClose); ok {
		t.Fatal("unknown conclusion field was accepted")
	}
	for _, malformed := range []string{"", ConclusionOpen + `{}`, `{}` + ConclusionClose} {
		if _, ok := ExtractConclusion(malformed); ok {
			t.Fatalf("malformed conclusion %q was accepted", malformed)
		}
	}
	if value := Success(true); value == nil || !*value {
		t.Fatal("Success(true) did not return true pointer")
	}
}
