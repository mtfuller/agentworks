package tests

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestCLIAddHelp checks that `add --help` documents what sources are
// actually supported.
func TestCLIAddHelp(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "add", "--help")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to run add --help: %v\nOutput: %s", err, out.String())
	}
	output := out.String()
	if !strings.Contains(output, "Usage:") {
		t.Errorf("add --help output should contain 'Usage:', got: %s", output)
	}
	if !strings.Contains(output, "github.com") {
		t.Errorf("add --help output should describe supported sources, got: %s", output)
	}
}

// TestCLIAddRejectsUnsupportedURL exercises the real binary against a URL it
// can't interpret. This never touches the network: ParseAddArgument rejects a
// gitlab.com web "tree" URL (whose ref/path syntax varies by host) before any
// fetch is attempted, and points at the explicit #ref:path form.
func TestCLIAddRejectsUnsupportedURL(t *testing.T) {
	projectDir := t.TempDir()
	if out, err := exec.Command("go", "run", "../main.go", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}

	cmd := exec.Command("go", "run", "../main.go", "add", "https://gitlab.com/owner/repo/-/tree/main/plugins/x", "--project", projectDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		t.Fatal("add against a gitlab.com URL expected a non-zero exit, got none")
	}
	output := out.String()
	if !strings.Contains(output, "gitlab.com") {
		t.Errorf("error output should name the unsupported host, got: %s", output)
	}
	if !strings.Contains(output, "#<ref>:<path>") {
		t.Errorf("error output should explain the explicit ref/path form, got: %s", output)
	}
}

// TestCLIAddNoArgsNonInteractive checks that add with no URL, run
// non-interactively (as `go test` always does -- stdin isn't a terminal),
// fails clearly instead of hanging or silently doing nothing.
func TestCLIAddNoArgsNonInteractive(t *testing.T) {
	projectDir := t.TempDir()
	if out, err := exec.Command("go", "run", "../main.go", "init", projectDir).CombinedOutput(); err != nil {
		t.Fatalf("init failed: %v\nOutput: %s", err, out)
	}

	cmd := exec.Command("go", "run", "../main.go", "add", "--project", projectDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		t.Fatal("add with no URL, non-interactively, expected a non-zero exit, got none")
	}
	if !strings.Contains(out.String(), "pass a URL") {
		t.Errorf("error output should explain a URL is needed, got: %s", out.String())
	}
}

// TestCLIAddOutsideProject checks that add fails clearly when not run
// inside an AgentWorks project, before ever looking at the URL argument.
func TestCLIAddOutsideProject(t *testing.T) {
	emptyDir := t.TempDir()
	cmd := exec.Command("go", "run", "../main.go", "add", "owner/repo", "--project", emptyDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		t.Fatal("add outside an AgentWorks project expected a non-zero exit, got none")
	}
	if !strings.Contains(out.String(), "agentworks init") {
		t.Errorf("error output should point at 'agentworks init', got: %s", out.String())
	}
}
