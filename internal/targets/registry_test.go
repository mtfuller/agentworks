package targets

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestGetAndSupports(t *testing.T) {
	tgt, ok := Get("claude-code")
	if !ok {
		t.Fatal("Get(claude-code) not found")
	}
	if tgt.Name != "Claude Code" {
		t.Errorf("Name = %q, want Claude Code", tgt.Name)
	}
	if !Supports("claude-code", artifact.KindSkill) {
		t.Error("Supports(claude-code, skill) = false, want true")
	}
	if Supports("chatgpt", artifact.KindHook) {
		t.Error("Supports(chatgpt, hook) = true, want false")
	}
	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get(does-not-exist) found a target, want not found")
	}
}

func TestGetExporterUnknownTarget(t *testing.T) {
	if _, err := GetExporter("does-not-exist"); err == nil {
		t.Fatal("GetExporter(does-not-exist) expected error, got nil")
	}
}

func TestGetExporterUnregisteredTarget(t *testing.T) {
	// chatgpt is a known target but has no Exporter registered in this
	// package (only internal/targets/claudecode registers one, and it's
	// not imported here) -- confirm that surfaces as a clear error rather
	// than a nil-map panic.
	if _, err := GetExporter("chatgpt"); err == nil {
		t.Fatal("GetExporter(chatgpt) expected 'not implemented' error, got nil")
	}
}

type stubExporter struct{ id string }

func (s stubExporter) TargetID() string { return s.id }
func (s stubExporter) Export(a *artifact.Artifact, outDir string, opts ExportOptions) (string, error) {
	return outDir, nil
}

func TestRegisterAndGetExporter(t *testing.T) {
	Register(stubExporter{id: "chatgpt"})
	e, err := GetExporter("chatgpt")
	if err != nil {
		t.Fatalf("GetExporter() error = %v", err)
	}
	if e.TargetID() != "chatgpt" {
		t.Errorf("TargetID() = %q, want chatgpt", e.TargetID())
	}
}
