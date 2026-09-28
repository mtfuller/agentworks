package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStudioRequiresExplicitExperimentalFlag(t *testing.T) {
	result := runCLIRaw(t, "studio")
	if result.err == nil || !strings.Contains(result.combined(), "--experimental") {
		t.Fatalf("result = %#v", result)
	}
}

func TestStudioRequiresFormat2Project(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "agentworks.yaml"), []byte("format: 1\nname: example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runCLI(t, root, "studio", "--experimental", "--no-open")
	if result.err == nil || !strings.Contains(result.combined(), "format must be 2") {
		t.Fatalf("result = %#v", result)
	}
}

func TestStudioRejectsInvalidPortBeforeServing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "agentworks.yaml"), []byte("format: 2\nname: example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runCLI(t, root, "studio", "--experimental", "--no-open", "--port", "70000")
	if result.err == nil || !strings.Contains(result.combined(), "outside 0-65535") {
		t.Fatalf("result = %#v", result)
	}
}
