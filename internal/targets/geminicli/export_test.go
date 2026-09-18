package geminicli

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func TestExportRejectsWorkflow(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindWorkflow, "demo", scaffold.Options{Description: "A demo workflow."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}
	if _, err := (exporter{}).Export(a, t.TempDir(), targets.ExportOptions{}); err == nil {
		t.Fatal("Export() of a workflow expected error, got nil")
	}
}

func TestRegisteredWithTargets(t *testing.T) {
	e, err := targets.GetExporter(TargetID)
	if err != nil {
		t.Fatalf("GetExporter(%s) error = %v", TargetID, err)
	}
	if e.TargetID() != TargetID {
		t.Errorf("TargetID() = %q, want %q", e.TargetID(), TargetID)
	}
}
