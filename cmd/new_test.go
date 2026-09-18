package cmd

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestNeedsInteractiveWizard(t *testing.T) {
	tests := []struct {
		name                                             string
		kindStr, artifactName, description, fromTemplate string
		want                                             bool
	}{
		{"everything given", "tool", "demo", "x", "", false},
		{"missing kind", "", "demo", "x", "", true},
		{"missing name", "tool", "", "x", "", true},
		{"missing description, no template", "tool", "demo", "", "", true},
		{"missing description, with template", "tool", "demo", "", "api-wrapper", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsInteractiveWizard(tt.kindStr, tt.artifactName, tt.description, tt.fromTemplate)
			if got != tt.want {
				t.Errorf("needsInteractiveWizard(%q, %q, %q, %q) = %v, want %v",
					tt.kindStr, tt.artifactName, tt.description, tt.fromTemplate, got, tt.want)
			}
		})
	}
}

func TestResolveDescription(t *testing.T) {
	if got := resolveDescription("explicit", artifact.KindMCP, "api-wrapper"); got != "explicit" {
		t.Errorf("resolveDescription() = %q, want the explicit description preserved", got)
	}
	if got := resolveDescription("", artifact.KindMCP, ""); got != "" {
		t.Errorf("resolveDescription() = %q, want empty (no template to fall back to)", got)
	}

	got := resolveDescription("", artifact.KindMCP, "api-wrapper")
	if got == "" {
		t.Error("resolveDescription() = empty, want the template's own description")
	}

	// An unknown template is left for scaffold.New to reject with its own
	// clear error -- resolveDescription shouldn't mask that by silently
	// falling back to empty in a way that looks like success.
	if got := resolveDescription("", artifact.KindMCP, "does-not-exist"); got != "" {
		t.Errorf("resolveDescription() = %q, want empty for an unknown template", got)
	}
}
