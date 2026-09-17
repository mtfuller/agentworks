package tui

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestExportFormRestrictsToSupportingTargets(t *testing.T) {
	a := &artifact.Artifact{Frontmatter: artifact.Frontmatter{Kind: artifact.KindSkill, Name: "demo", Description: "x"}}
	form, answers := exportForm(a)
	if form == nil || answers == nil {
		t.Fatal("exportForm() = nil, nil; want a form for a skill (every target supports skills)")
	}
}

func TestExportFormNoSupportingTargets(t *testing.T) {
	// No registered target supports an artifact.Kind that doesn't exist.
	a := &artifact.Artifact{Frontmatter: artifact.Frontmatter{Kind: artifact.Kind("bogus"), Name: "demo", Description: "x"}}
	form, answers := exportForm(a)
	if form != nil || answers != nil {
		t.Fatalf("exportForm() = %v, %v; want nil, nil for an unsupported kind", form, answers)
	}
}
