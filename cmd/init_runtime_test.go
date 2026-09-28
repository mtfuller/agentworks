package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeInitPlanAndPackFreshProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fresh-runtime")
	result := runCLIRaw(t, "init", root, "--runtime", "--name", "fresh-runtime")
	if result.err != nil {
		t.Fatalf("runtime init: %v\n%s", result.err, result.combined())
	}
	if result := runCLI(t, root, "plan", "default", "--provider", "host", "--json"); result.err != nil || !strings.Contains(result.stdout, `"team": "default"`) {
		t.Fatalf("runtime plan: %v\n%s", result.err, result.combined())
	}
	archive := filepath.Join(t.TempDir(), "fresh.agentworks")
	if result := runCLI(t, root, "pack", "default", "--output", archive); result.err != nil {
		t.Fatalf("runtime pack: %v\n%s", result.err, result.combined())
	}
	if info, err := os.Stat(archive); err != nil || info.Size() == 0 {
		t.Fatalf("runtime archive: info=%v err=%v", info, err)
	}
}

func TestRuntimeInitRejectsLegacyOnlyFlags(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fresh-runtime")
	result := runCLIRaw(t, "init", root, "--runtime", "--target", "claude-code")
	if result.err == nil || !strings.Contains(result.err.Error(), "cannot currently be combined") {
		t.Fatalf("result=%v output=%s", result.err, result.combined())
	}
}
