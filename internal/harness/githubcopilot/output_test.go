package githubcopilot

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

func TestJSONLFilterBuffersAndRedactsPrivateProviderFields(t *testing.T) {
	filter := jsonlFilter()
	if got := filter(awprocess.Stdout, []byte(`{"type":"assistant.message","data":{"content":"ok","apiCallId":"secret"},`)); len(got) != 0 {
		t.Fatalf("partial output = %q", got)
	}
	got := string(filter(awprocess.Stdout, []byte(`"request_id":"private","sessionId":"keep"}`+"\n")))
	if strings.Contains(got, "secret") || strings.Contains(got, "private") {
		t.Fatalf("private fields leaked: %s", got)
	}
	for _, retained := range []string{`"content":"ok"`} {
		if !strings.Contains(got, retained) {
			t.Errorf("filtered output does not contain %s: %s", retained, got)
		}
	}
	if got := string(filter(awprocess.Stderr, []byte("diagnostic\n"))); got != "diagnostic\n" {
		t.Errorf("stderr = %q", got)
	}
}

func TestRecordedReadonlyFixtureRemainsValidJSONL(t *testing.T) {
	file, err := os.Open("testdata/1.0.88-readonly-success.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("fixture line is invalid: %v", err)
		}
		seen[event.Type] = true
		for private := range privateOutputKeys {
			if strings.Contains(scanner.Text(), `"`+private+`"`) {
				t.Fatalf("fixture contains private key %q", private)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"assistant.message", "result"} {
		if !seen[required] {
			t.Errorf("fixture does not contain %q", required)
		}
	}
}

func TestJSONLFilterSuppressesMalformedProtocolPayload(t *testing.T) {
	got := string(jsonlFilter()(awprocess.Stdout, []byte("not json\n")))
	if strings.Contains(got, "not json") || !strings.Contains(got, "agentworks.unparsed-output") {
		t.Fatalf("filtered output = %q", got)
	}
}
