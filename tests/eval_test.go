package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeEvalRunner writes a tiny fake "eval_runner" script under an
// artifact's directory that echoes canned text back on stdout -- standing
// in for a real model call the same way the starter project's jira-fetch
// tool simulates its API rather than calling a real Jira instance.
func writeEvalRunner(t *testing.T, artifactDir, response string) string {
	t.Helper()
	scriptPath := filepath.Join(artifactDir, "fake-runner.sh")
	content := "#!/bin/sh\ncat > /dev/null\necho \"" + response + "\"\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("writing fake runner: %v", err)
	}
	return "sh fake-runner.sh"
}

// TestCLIEvalPassesAndFails checks the end-to-end path: a scaffolded
// agent's default eval case is edited to match a fake runner's canned
// response (pass), then a second case is added that the same response
// can't satisfy (fail), and `agentworks eval` reports both correctly.
func TestCLIEvalPassesAndFails(t *testing.T) {
	projectDir := t.TempDir()
	if out, err := exec.Command("go", "run", "../main.go", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}
	if out, err := exec.Command("go", "run", "../main.go", "new", "agent", "researcher",
		"--description", "Investigates a question and reports findings.", "--project", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("new failed: %v\nOutput: %s", err, out)
	}

	agentDir := filepath.Join(projectDir, "agents", "researcher")
	runner := writeEvalRunner(t, agentDir, "found an outlier in the data")

	agentMD := filepath.Join(agentDir, "agent.md")
	data, err := os.ReadFile(agentMD)
	if err != nil {
		t.Fatalf("reading agent.md: %v", err)
	}
	updated := strings.Replace(string(data),
		"version: 0.1.0\n",
		"version: 0.1.0\neval_runner: \""+runner+"\"\n",
		1)
	if updated == string(data) {
		t.Fatal("failed to inject eval_runner into agent.md frontmatter")
	}
	if err := os.WriteFile(agentMD, []byte(updated), 0o644); err != nil {
		t.Fatalf("writing agent.md: %v", err)
	}

	evalsYAML := `cases:
  - name: passing case
    prompt: "does not matter, the fake runner ignores it"
    assert:
      contains: ["outlier"]
  - name: failing case
    prompt: "does not matter, the fake runner ignores it"
    assert:
      contains: ["a phrase the fake runner never returns"]
`
	if err := os.WriteFile(filepath.Join(agentDir, "evals", "example.yaml"), []byte(evalsYAML), 0o644); err != nil {
		t.Fatalf("writing evals/example.yaml: %v", err)
	}

	// loadArtifactAtPath (shared by eval/test/validate/export) resolves a
	// path argument from the process's own cwd, not relative to --project
	// -- so the target here must be an absolute path.
	cmd := exec.Command("go", "run", "../main.go", "eval", agentDir, "--project", projectDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()

	output := out.String()
	if err == nil {
		t.Fatalf("eval expected a non-zero exit (one case fails), got success\nOutput: %s", output)
	}
	if !strings.Contains(output, "passing case") || !strings.Contains(output, "passed") {
		t.Errorf("eval output should report the passing case, got: %s", output)
	}
	if !strings.Contains(output, "failing case") || !strings.Contains(output, "failed") {
		t.Errorf("eval output should report the failing case, got: %s", output)
	}
}

// TestCLIEvalSkipsArtifactWithoutRunner checks that an artifact with eval
// cases but no eval_runner (and no project default) is skipped with a
// warning rather than failing the command.
func TestCLIEvalSkipsArtifactWithoutRunner(t *testing.T) {
	projectDir := t.TempDir()
	if out, err := exec.Command("go", "run", "../main.go", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}
	if out, err := exec.Command("go", "run", "../main.go", "new", "agent", "researcher",
		"--description", "Investigates a question and reports findings.", "--project", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("new failed: %v\nOutput: %s", err, out)
	}

	cmd := exec.Command("go", "run", "../main.go", "eval", "--project", projectDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("eval with no runner configured expected success (skip, not fail), got: %v\nOutput: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "no \"eval_runner\" set") {
		t.Errorf("eval output should explain why the artifact was skipped, got: %s", out.String())
	}
}
