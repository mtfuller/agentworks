package tests

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestCLIValidateWarnsOnWeakDescription checks that a structurally valid
// but low-quality description is reported as a warning, and doesn't fail
// the command, unless --strict is passed.
func TestCLIValidateWarnsOnWeakDescription(t *testing.T) {
	projectDir := t.TempDir()
	if out, err := exec.Command("go", "run", "../main.go", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}
	if out, err := exec.Command("go", "run", "../main.go", "new", "skill", "vague-thing",
		"--description", "Helps with stuff.", "--project", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("new failed: %v\nOutput: %s", err, out)
	}

	plain := exec.Command("go", "run", "../main.go", "validate", "--project", projectDir)
	var plainOut bytes.Buffer
	plain.Stdout = &plainOut
	plain.Stderr = &plainOut
	if err := plain.Run(); err != nil {
		t.Fatalf("validate (non-strict) expected success despite the warning, got: %v\nOutput: %s", err, plainOut.String())
	}
	if !strings.Contains(plainOut.String(), "very short") {
		t.Errorf("validate output should warn about the weak description, got: %s", plainOut.String())
	}

	strict := exec.Command("go", "run", "../main.go", "validate", "--project", projectDir, "--strict")
	var strictOut bytes.Buffer
	strict.Stdout = &strictOut
	strict.Stderr = &strictOut
	if err := strict.Run(); err == nil {
		t.Fatalf("validate --strict expected a non-zero exit for the weak description, got none\nOutput: %s", strictOut.String())
	}
}

// TestCLIValidateOverlapAcrossArtifacts checks that two same-kind skills
// with near-duplicate descriptions are both flagged when validating the
// whole project.
func TestCLIValidateOverlapAcrossArtifacts(t *testing.T) {
	projectDir := t.TempDir()
	if out, err := exec.Command("go", "run", "../main.go", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}
	desc := "Analyze a CSV file and flag rows that stand out from the rest of the data."
	for _, name := range []string{"csv-outliers", "csv-anomalies"} {
		if out, err := exec.Command("go", "run", "../main.go", "new", "skill", name,
			"--description", desc, "--project", projectDir).CombinedOutput(); err != nil {
			t.Fatalf("new %s failed: %v\nOutput: %s", name, err, out)
		}
	}

	cmd := exec.Command("go", "run", "../main.go", "validate", "--project", projectDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("validate (non-strict) expected success despite the overlap warning, got: %v\nOutput: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "overlaps heavily") {
		t.Errorf("validate output should warn about overlapping descriptions, got: %s", out.String())
	}
}
