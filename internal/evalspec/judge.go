package evalspec

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// A rubric assertion is graded by a judge: a command the project supplies (in
// eval.judge_runner, or an artifact's judge_runner) that reads a JudgeRequest as
// one JSON object on stdin and prints a JudgeVerdict as one JSON object on
// stdout. AgentWorks never calls a model itself -- the judge command is the
// project's own, exactly like eval_runner is, and typically wraps a model call.

// JudgeRequest is what a judge command receives on stdin.
type JudgeRequest struct {
	// Subject is the name of the artifact being evaluated.
	Subject string `json:"subject"`
	// Prompt is what the runner was asked.
	Prompt string `json:"prompt"`
	// Response is the runner's response text.
	Response string `json:"response"`
	// Rubric is the criteria to grade the response against.
	Rubric string `json:"rubric"`
}

// JudgeVerdict is what a judge command prints on stdout.
type JudgeVerdict struct {
	// Pass is whether the response meets the rubric. It is required.
	Pass bool `json:"pass"`
	// Reason says why, and is shown when the case fails.
	Reason string `json:"reason,omitempty"`
}

// ParseVerdict decodes a judge's stdout. A missing "pass" is an error rather
// than a silent false, so a judge that prints something else can't quietly
// fail (or pass) every case.
func ParseVerdict(stdout string) (JudgeVerdict, error) {
	trimmed := bytes.TrimSpace([]byte(stdout))
	if len(trimmed) == 0 {
		return JudgeVerdict{}, fmt.Errorf("the judge printed nothing (expected {\"pass\": true|false, \"reason\": \"...\"})")
	}
	var raw struct {
		Pass   *bool  `json:"pass"`
		Reason string `json:"reason"`
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(&raw); err != nil {
		return JudgeVerdict{}, fmt.Errorf("the judge's output is not a JSON object: %v", err)
	}
	if dec.More() {
		return JudgeVerdict{}, fmt.Errorf("the judge printed more than one JSON value; stdout must be exactly one object (send logs to stderr)")
	}
	if raw.Pass == nil {
		return JudgeVerdict{}, fmt.Errorf("the judge's JSON has no \"pass\" field")
	}
	return JudgeVerdict{Pass: *raw.Pass, Reason: raw.Reason}, nil
}
