package artifact

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndRenderRoundTrip(t *testing.T) {
	src := `---
kind: skill
name: csv-analyzer
description: Analyze a CSV file and flag rows that stand out.
version: 0.1.0
targets:
    - claude-code
    - chatgpt
entrypoint: scripts/main.py
test: python3 -m pytest tests
---

# CSV Analyzer

Body text here.
`
	fm, body, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if fm.Kind != KindSkill {
		t.Errorf("Kind = %q, want %q", fm.Kind, KindSkill)
	}
	if fm.Name != "csv-analyzer" {
		t.Errorf("Name = %q, want csv-analyzer", fm.Name)
	}
	if got := fm.ExtraString("entrypoint"); got != "scripts/main.py" {
		t.Errorf("ExtraString(entrypoint) = %q, want scripts/main.py", got)
	}
	// Targets are project-level now; a legacy per-artifact `targets:` key
	// must still parse and round-trip untouched rather than erroring.
	if _, ok := fm.Extra["targets"]; !ok {
		t.Errorf("legacy targets key not preserved in Extra: %v", fm.Extra)
	}
	if !strings.Contains(body, "# CSV Analyzer") {
		t.Errorf("body missing heading, got: %q", body)
	}

	rendered, err := Render(fm, body)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fm2, body2, err := Parse(rendered)
	if err != nil {
		t.Fatalf("re-parsing rendered output: %v", err)
	}
	if fm2.Name != fm.Name || fm2.Description != fm.Description {
		t.Errorf("round-trip frontmatter mismatch: got %+v, want %+v", fm2, fm)
	}
	if strings.TrimSpace(body2) != strings.TrimSpace(body) {
		t.Errorf("round-trip body mismatch: got %q, want %q", body2, body)
	}
}

func TestParseTrimsAllLeadingBlankLinesInBody(t *testing.T) {
	// Render always emits exactly one blank line between the closing
	// delimiter and the body; Parse must fully undo that (not just strip
	// one newline), so Save -> Load -> Save doesn't accumulate blank
	// lines and downstream consumers like the claude-code exporter don't
	// see a stray leading blank line.
	fm := Frontmatter{Kind: KindSkill, Name: "demo", Description: "x"}
	rendered, err := Render(fm, "# Demo\n\nBody text.\n")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	_, body, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if strings.HasPrefix(body, "\n") {
		t.Fatalf("Parse() left a leading blank line in body: %q", body)
	}
	if !strings.HasPrefix(body, "# Demo") {
		t.Fatalf("Parse() body = %q, want it to start with '# Demo'", body)
	}
}

func TestParseMissingFrontmatter(t *testing.T) {
	_, _, err := Parse([]byte("# just markdown, no frontmatter\n"))
	if err == nil {
		t.Fatal("Parse() expected error for missing frontmatter, got nil")
	}
}

func TestParseUnclosedFrontmatter(t *testing.T) {
	_, _, err := Parse([]byte("---\nname: broken\n"))
	if err == nil {
		t.Fatal("Parse() expected error for unclosed frontmatter, got nil")
	}
}

func TestLoadAndSaveRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills", "demo")
	a := &Artifact{
		Frontmatter: Frontmatter{
			Kind:        KindSkill,
			Name:        "demo",
			Description: "A demo skill.",
			Version:     "0.1.0",
		},
		Body: "# Demo\n\nInstructions.\n",
		Dir:  dir,
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(dir, KindSkill)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != a.Name || loaded.Description != a.Description {
		t.Errorf("loaded = %+v, want name/description matching %+v", loaded.Frontmatter, a.Frontmatter)
	}

	viaFile, err := LoadFile(a.File())
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if viaFile.Name != a.Name {
		t.Errorf("LoadFile().Name = %q, want %q", viaFile.Name, a.Name)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		a       Artifact
		wantErr bool
	}{
		{
			name: "valid",
			a: Artifact{
				Frontmatter: Frontmatter{Kind: KindSkill, Name: "demo", Description: "A demo skill."},
				Dir:         "skills/demo",
			},
			wantErr: false,
		},
		{
			name:    "missing description",
			a:       Artifact{Frontmatter: Frontmatter{Kind: KindSkill, Name: "demo"}, Dir: "skills/demo"},
			wantErr: true,
		},
		{
			name:    "bad name",
			a:       Artifact{Frontmatter: Frontmatter{Kind: KindSkill, Name: "Demo Skill", Description: "x"}, Dir: "skills/Demo Skill"},
			wantErr: true,
		},
		{
			name:    "unknown kind",
			a:       Artifact{Frontmatter: Frontmatter{Kind: "bogus", Name: "demo", Description: "x"}, Dir: "skills/demo"},
			wantErr: true,
		},
		{
			name:    "name does not match directory",
			a:       Artifact{Frontmatter: Frontmatter{Kind: KindSkill, Name: "demo", Description: "x"}, Dir: "skills/other"},
			wantErr: true,
		},
		{
			name: "namespaced, matching parent dir",
			a: Artifact{
				Frontmatter: Frontmatter{Kind: KindSkill, Name: "demo", Namespace: "team-a", Description: "x"},
				Dir:         "skills/team-a/demo",
			},
			wantErr: false,
		},
		{
			name: "namespaced, mismatched parent dir",
			a: Artifact{
				Frontmatter: Frontmatter{Kind: KindSkill, Name: "demo", Namespace: "team-a", Description: "x"},
				Dir:         "skills/team-b/demo",
			},
			wantErr: true,
		},
		{
			name: "invalid namespace slug",
			a: Artifact{
				Frontmatter: Frontmatter{Kind: KindSkill, Name: "demo", Namespace: "Team A", Description: "x"},
				Dir:         "skills/Team A/demo",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.a.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQualifiedName(t *testing.T) {
	unnamespaced := Artifact{Frontmatter: Frontmatter{Name: "demo"}}
	if got := unnamespaced.QualifiedName(); got != "demo" {
		t.Errorf("QualifiedName() = %q, want %q", got, "demo")
	}

	namespaced := Artifact{Frontmatter: Frontmatter{Name: "demo", Namespace: "team-a"}}
	if got := namespaced.QualifiedName(); got != "team-a/demo" {
		t.Errorf("QualifiedName() = %q, want %q", got, "team-a/demo")
	}
}

func TestKindHelpers(t *testing.T) {
	if KindSkill.FileName() != "skill.md" {
		t.Errorf("FileName() = %q, want skill.md", KindSkill.FileName())
	}
	if KindSkill.DirName() != "skills" {
		t.Errorf("DirName() = %q, want skills", KindSkill.DirName())
	}
	if _, err := ParseKind("bogus"); err == nil {
		t.Error("ParseKind(bogus) expected error, got nil")
	}
	k, err := ParseKind("agent")
	if err != nil || k != KindAgent {
		t.Errorf("ParseKind(agent) = %v, %v; want KindAgent, nil", k, err)
	}
}
