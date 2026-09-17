package scaffold

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/workflowsteps"
)

func TestEveryTemplateScaffoldsAndValidates(t *testing.T) {
	for _, tmpl := range Templates() {
		tmpl := tmpl
		t.Run(string(tmpl.Kind)+"/"+tmpl.ID, func(t *testing.T) {
			root := t.TempDir()
			a, err := New(root, tmpl.Kind, "demo", Options{
				Description: tmpl.Description,
				Template:    tmpl.ID,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := a.Validate(); err != nil {
				t.Fatalf("scaffolded artifact fails Validate(): %v", err)
			}
			if _, err := os.Stat(filepath.Join(a.Dir, tmpl.Kind.FileName())); err != nil {
				t.Errorf("manifest not written: %v", err)
			}

			// A workflow template must not prefill `steps:` with names that
			// don't exist yet -- that would fail validate/export on a fresh
			// project before the user ever gets to fill it in themselves.
			if tmpl.Kind == artifact.KindWorkflow {
				if _, err := workflowsteps.Resolve(a); err != nil {
					t.Errorf("workflow template %q has unresolvable steps out of the box: %v", tmpl.ID, err)
				}
			}
		})
	}
}

func TestGetTemplate(t *testing.T) {
	if _, ok := GetTemplate(artifact.KindTool, "api-wrapper"); !ok {
		t.Error("GetTemplate(tool, api-wrapper) = false, want true")
	}
	if _, ok := GetTemplate(artifact.KindTool, "does-not-exist"); ok {
		t.Error("GetTemplate(tool, does-not-exist) = true, want false")
	}
	// A real ID under the wrong kind should also miss -- templates are
	// scoped per kind.
	if _, ok := GetTemplate(artifact.KindSkill, "api-wrapper"); ok {
		t.Error("GetTemplate(skill, api-wrapper) = true, want false (that ID belongs to tool)")
	}
}

func TestNewRejectsUnknownTemplate(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root, artifact.KindTool, "demo", Options{Description: "x", Template: "does-not-exist"}); err == nil {
		t.Fatal("New() with an unknown template expected error, got nil")
	}
}

func TestTemplatesForKind(t *testing.T) {
	tools := TemplatesForKind(artifact.KindTool)
	if len(tools) == 0 {
		t.Fatal("TemplatesForKind(tool) = 0, want at least 1")
	}
	for _, tmpl := range tools {
		if tmpl.Kind != artifact.KindTool {
			t.Errorf("TemplatesForKind(tool) returned a %s template: %+v", tmpl.Kind, tmpl)
		}
	}
}

func TestEveryKindHasAtLeastOneTemplate(t *testing.T) {
	for _, k := range artifact.Kinds() {
		if len(TemplatesForKind(k)) == 0 {
			t.Errorf("kind %s has no templates", k)
		}
	}
}
