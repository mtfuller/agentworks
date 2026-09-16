package tests

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestCLIVersion tests the version command
func TestCLIVersion(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to run version command: %v\nOutput: %s", err, out.String())
	}

	output := out.String()
	if !strings.Contains(output, "agentworks") {
		t.Errorf("Version output should contain 'agentworks', got: %s", output)
	}
	if !strings.Contains(output, "Version:") {
		t.Errorf("Version output should contain 'Version:', got: %s", output)
	}
}

// TestCLIVersionShort tests the version command with --short flag
func TestCLIVersionShort(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "version", "--short")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to run version --short command: %v\nOutput: %s", err, out.String())
	}

	output := strings.TrimSpace(out.String())
	// Should only output the version string
	if output != "dev" {
		t.Errorf("Version --short output should be 'dev', got: %s", output)
	}
}

// TestCLIHelp tests the help command
func TestCLIHelp(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "--help")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to run help command: %v\nOutput: %s", err, out.String())
	}

	output := out.String()
	if !strings.Contains(output, "Usage:") {
		t.Errorf("Help output should contain 'Usage:', got: %s", output)
	}
	if !strings.Contains(output, "Available Commands:") {
		t.Errorf("Help output should contain 'Available Commands:', got: %s", output)
	}
}
