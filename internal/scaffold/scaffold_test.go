package scaffold

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestNewEachKind(t *testing.T) {
	for _, k := range artifact.Kinds() {
		k := k
		t.Run(string(k), func(t *testing.T) {
			root := t.TempDir()
			a, err := New(root, k, "demo", Options{
				Description: "A demo " + string(k) + ".",
				Targets:     []string{"claude-code"},
			})
			if err != nil {
				t.Fatalf("New(%s) error = %v", k, err)
			}
			if a.Name != "demo" || a.Kind != k {
				t.Errorf("New(%s) artifact = %+v", k, a.Frontmatter)
			}
			if err := a.Validate(); err != nil {
				t.Errorf("scaffolded artifact fails Validate(): %v", err)
			}

			manifestPath := filepath.Join(root, k.DirName(), "demo", k.FileName())
			if _, err := os.Stat(manifestPath); err != nil {
				t.Errorf("expected manifest at %s: %v", manifestPath, err)
			}

			loaded, err := artifact.Load(a.Dir, k)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if loaded.Description != a.Description {
				t.Errorf("loaded description = %q, want %q", loaded.Description, a.Description)
			}
		})
	}
}

func TestNewRefusesExisting(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root, artifact.KindSkill, "demo", Options{Description: "x"}); err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	if _, err := New(root, artifact.KindSkill, "demo", Options{Description: "x"}); err == nil {
		t.Fatal("second New() expected error, got nil")
	}
}

func TestNewRejectsInvalidName(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root, artifact.KindSkill, "Not A Valid Name", Options{Description: "x"}); err == nil {
		t.Fatal("New() with invalid name expected error, got nil")
	}
}

func TestNewSkillCreatesSupportingFiles(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, artifact.KindSkill, "csv-analyzer", Options{Description: "x"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, rel := range []string{"scripts/main.py", "tests/test_main.py", "samples"} {
		if _, err := os.Stat(filepath.Join(a.Dir, rel)); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestNewToolHasCommandAndAuthFields(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, artifact.KindTool, "jira-fetch", Options{Description: "x"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Empty by default -- AgentWorks can't guess how to run the tool -- but
	// present so exporters and authors both know the fields exist.
	if got := a.ExtraString("command"); got != "" {
		t.Errorf("command = %q, want empty default", got)
	}
	if _, ok := a.Extra["auth"]; !ok {
		t.Error("expected an \"auth\" key in Extra by default")
	}
}
