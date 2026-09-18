package tui

import (
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/export"
)

func skillArt() []*artifact.Artifact {
	return []*artifact.Artifact{{Frontmatter: artifact.Frontmatter{Kind: artifact.KindSkill, Name: "demo", Description: "x"}}}
}

func TestExportFormOffersPluginOnlyWithTargets(t *testing.T) {
	form, answers := exportForm([]string{"claude-code"}, []string{"."}, skillArt())
	if form == nil || answers.Format != string(export.FormatPlugin) {
		t.Fatalf("with targets configured the form should default to a plugin export, got %+v", answers)
	}

	// No targets: plugin formats are unavailable, but the skill archives
	// need no target, so the form still opens -- defaulting to one.
	form, answers = exportForm(nil, []string{"."}, skillArt())
	if form == nil || answers.Format != string(export.FormatSkillsZip) {
		t.Fatalf("without targets the form should fall back to a skill archive, got %+v", answers)
	}
}

func TestExportFormNothingToOffer(t *testing.T) {
	agentOnly := []*artifact.Artifact{{Frontmatter: artifact.Frontmatter{Kind: artifact.KindAgent, Name: "a", Description: "x"}}}
	if form, answers := exportForm(nil, []string{"."}, agentOnly); form != nil || answers != nil {
		t.Fatalf("exportForm() = %v, %v; want nil, nil when there are no targets and no skills", form, answers)
	}
}

func TestExportAnswersRequest(t *testing.T) {
	a := &ExportAnswers{Format: formatNamespaces, Namespaces: []string{"obra"}, Zip: true}
	req := a.request("/p", "proj")
	if req.Format != export.FormatPlugin || len(req.Namespaces) != 1 || !req.Zip {
		t.Errorf("request() = %+v, want a zipped plugin export limited to obra", req)
	}
	if req.OutDir != filepath.Join("/p", "dist") {
		t.Errorf("OutDir = %s", req.OutDir)
	}

	// Zip only applies to plugins; a stale answer must not leak into a skill export.
	req = (&ExportAnswers{Format: string(export.FormatSkillFiles), Zip: true}).request("/p", "proj")
	if req.Zip {
		t.Error("Zip should be ignored for skill formats")
	}
}
