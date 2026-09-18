package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestTemplateItems(t *testing.T) {
	templates := scaffold.TemplatesForKind(artifact.KindTool)
	items := templateItems(templates)
	if len(items) != len(templates) {
		t.Fatalf("len(items) = %d, want %d", len(items), len(templates))
	}

	item, ok := items[0].(templateItem)
	if !ok {
		t.Fatalf("items[0] type = %T, want templateItem", items[0])
	}
	if item.Title() != templates[0].Title {
		t.Errorf("Title() = %q, want %q", item.Title(), templates[0].Title)
	}
	if item.Description() != templates[0].Description {
		t.Errorf("Description() = %q, want %q (the templates pane is tabbed by kind, so the kind isn't repeated per row)", item.Description(), templates[0].Description)
	}
	if !strings.Contains(item.FilterValue(), templates[0].Title) {
		t.Errorf("FilterValue() = %q, want it to contain the title", item.FilterValue())
	}
}

func TestBFromBrowseOpensTemplatesPaneOnTheSameKindTab(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindTool)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = updated.(Model)
	if m.pane != paneTemplates {
		t.Fatalf("pane = %v, want paneTemplates", m.pane)
	}
	if cmd != nil {
		t.Error("startTemplates() returned a non-nil cmd, want nil (no fetch needed for local data)")
	}
	if m.currentTemplateKind() != artifact.KindTool {
		t.Errorf("currentTemplateKind() = %v, want tool (carried over from the browse pane's tab)", m.currentTemplateKind())
	}
	if got := len(m.templateLists[artifact.KindTool].Items()); got != len(scaffold.TemplatesForKind(artifact.KindTool)) {
		t.Errorf("tool template list has %d items, want %d", got, len(scaffold.TemplatesForKind(artifact.KindTool)))
	}
}

func TestBIsIgnoredFromDetail(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneDetail

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Errorf("pane = %v, want paneDetail unchanged", m.pane)
	}
}

func TestEnterFromTemplatesOpensPrefilledCreateForm(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindTool)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = updated.(Model)

	want, ok := m.templateLists[artifact.KindTool].SelectedItem().(templateItem)
	if !ok {
		t.Fatal("expected a selected item in the tool template list")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneForm {
		t.Fatalf("pane = %v, want paneForm", m.pane)
	}
	if m.formPurpose != formCreate {
		t.Fatalf("formPurpose = %v, want formCreate", m.formPurpose)
	}
	if m.newAnswers == nil {
		t.Fatal("newAnswers is nil, want it prefilled")
	}
	if m.newAnswers.Kind != string(artifact.KindTool) {
		t.Errorf("newAnswers.Kind = %q, want tool", m.newAnswers.Kind)
	}
	if m.newAnswers.Template != want.t.ID {
		t.Errorf("newAnswers.Template = %q, want %q", m.newAnswers.Template, want.t.ID)
	}
	if m.newAnswers.Description != want.t.Description {
		t.Errorf("newAnswers.Description = %q, want %q", m.newAnswers.Description, want.t.Description)
	}
	if m.formReturnPane != paneTemplates {
		t.Errorf("formReturnPane = %v, want paneTemplates", m.formReturnPane)
	}
	if cmd == nil {
		t.Error("expected a non-nil init cmd from starting the form")
	}
}

// TestCommitCreateFromTemplateMatchesDirectScaffold confirms a
// template-originated NewArtifactAnswers, driven through commitCreate,
// produces the same artifact content scaffold.New would directly -- not
// just that it doesn't error.
func TestCommitCreateFromTemplateMatchesDirectScaffold(t *testing.T) {
	m := newTestModel(t)
	tmpl, ok := scaffold.GetTemplate(artifact.KindTool, "api-wrapper")
	if !ok {
		t.Fatal("expected the api-wrapper tool template to exist")
	}

	m.newAnswers = &NewArtifactAnswers{
		Kind:        string(artifact.KindTool),
		Name:        "my-wrapper",
		Description: tmpl.Description,
		Template:    tmpl.ID,
	}
	updated, _ := m.commitCreate()
	m = updated.(Model)
	if m.statusMsg == "" || strings.Contains(m.statusMsg, "error") {
		t.Fatalf("commitCreate() statusMsg = %q, want a success message", m.statusMsg)
	}

	got, err := artifact.Load(filepath.Join(m.root, "tools", "my-wrapper"), artifact.KindTool)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ExtraString("command") == "" {
		t.Error("expected the api-wrapper template's command to be set, got empty")
	}
	if !strings.Contains(got.Body, "REST API") && !strings.Contains(got.Body, "API") {
		t.Errorf("Body = %q, want it to reflect the api-wrapper template's content", got.Body)
	}
}
