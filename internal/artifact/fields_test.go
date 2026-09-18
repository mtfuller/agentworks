package artifact

import (
	"os"
	"strings"
	"testing"
)

func artifactWith(kind Kind, extra map[string]any) *Artifact {
	return &Artifact{Frontmatter: Frontmatter{Kind: kind, Name: "demo", Extra: extra}, Dir: "x/demo"}
}

func TestLintFieldsUnknownDeprecatedAndMisplaced(t *testing.T) {
	a := artifactWith(KindMCP, map[string]any{
		"comand":  "python3 x.py",      // typo
		"x-owner": "team-a",            // sanctioned custom key
		"targets": []any{"cursor"},     // deprecated
		"events":  []any{"PreToolUse"}, // a hook field on an mcp
		"command": "python3 x.py",      // fine
	})
	got := a.LintFields()
	joined := ""
	for _, w := range got {
		joined += w.Message + "\n"
	}
	for _, want := range []string{`unknown field "comand"`, `"targets" is deprecated`, `field "events" doesn't apply to a mcp`} {
		if !strings.Contains(joined, want) {
			t.Errorf("LintFields() missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "x-owner") || strings.Contains(joined, `"command"`) {
		t.Errorf("LintFields() flagged a valid or x- field:\n%s", joined)
	}
	if len(got) != 3 {
		t.Errorf("LintFields() returned %d warnings, want 3:\n%s", len(got), joined)
	}
}

func TestValidateFieldTypes(t *testing.T) {
	tests := []struct {
		name    string
		kind    Kind
		extra   map[string]any
		wantErr bool
	}{
		{"auth as a list", KindMCP, map[string]any{"auth": []any{"A", "B"}}, false},
		{"auth as a bare string", KindMCP, map[string]any{"auth": "A"}, true},
		{"auth list with a number", KindMCP, map[string]any{"auth": []any{"A", 3}}, true},
		{"empty auth key", KindMCP, map[string]any{"auth": nil}, false},
		{"headers map", KindMCP, map[string]any{"headers": map[string]any{"X": "y"}}, false},
		{"headers as a list", KindMCP, map[string]any{"headers": []any{"X"}}, true},
		{"env value not a string", KindMCP, map[string]any{"env": map[string]any{"N": 5}}, true},
		{"command as a list", KindMCP, map[string]any{"command": []any{"a"}}, true},
		{"in-memory string slice", KindAgent, map[string]any{"tools": []string{"web-search"}}, false},
		{"handlers as list of objects", KindHook, map[string]any{"handlers": []any{map[string]any{"event": "A"}}}, false},
		{"handlers as a string", KindHook, map[string]any{"handlers": "x"}, true},
		{"unknown fields aren't type-checked", KindSkill, map[string]any{"whatever": 5}, false},
	}
	for _, tt := range tests {
		err := artifactWith(tt.kind, tt.extra).ValidateFieldTypes()
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: ValidateFieldTypes() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestFieldsForIncludesCommonThenKindSpecific(t *testing.T) {
	names := func(k Kind) map[string]bool {
		m := map[string]bool{}
		for _, f := range FieldsFor(k) {
			m[f.Name] = true
		}
		return m
	}
	mcp, hook, skill := names(KindMCP), names(KindHook), names(KindSkill)
	for _, want := range []string{"kind", "name", "description", "transport", "auth", "command"} {
		if !mcp[want] {
			t.Errorf("mcp fields missing %q", want)
		}
	}
	if mcp["events"] || mcp["handlers"] {
		t.Error("mcp should not list hook fields")
	}
	if !hook["handlers"] || !hook["events"] || !hook["command"] {
		t.Error("hook should list handlers, events, and command")
	}
	if skill["command"] || skill["auth"] {
		t.Error("skill should not list command or auth")
	}
}

func TestFrontmatterReferenceIsCurrent(t *testing.T) {
	const path = "../../docs/reference/frontmatter.md"
	want := FieldsMarkdown()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (generate it with UPDATE_GOLDEN=1 go test ./internal/artifact)", path, err)
	}
	if string(got) != want {
		t.Errorf("%s is out of date with the field registry -- regenerate with: UPDATE_GOLDEN=1 go test ./internal/artifact", path)
	}
}
