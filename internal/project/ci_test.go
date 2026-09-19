package project

import (
	"os"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/version"
)

func TestCIWorkflowPinsTheRunningRelease(t *testing.T) {
	orig := version.Version
	defer func() { version.Version = orig }()

	version.Version = "v1.2.3"
	text := ciWorkflowText()
	if !strings.Contains(text, "mtfuller/agentworks@v1.2.3") || !strings.Contains(text, "version: v1.2.3") {
		t.Errorf("a release build should pin itself:\n%s", text)
	}

	version.Version = "dev"
	if !strings.Contains(ciWorkflowText(), "mtfuller/agentworks@main") {
		t.Error("a development build has no tag to pin, so it tracks main")
	}
	if strings.Contains(ciWorkflowText(), "__REF__") {
		t.Error("placeholder left in the workflow")
	}
}

func TestWriteCIWorkflowRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteCIWorkflow(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCIWorkflow(dir); err == nil {
		t.Error("second write should refuse")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
