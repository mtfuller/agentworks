package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"

	// Blank-imported so its init() registers the "claude-code" exporter
	// with internal/targets -- needed for TestCommitExportRunsRealExporter
	// to actually run one, the same way cmd/export.go registers it for
	// the real binary.
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
)

// TestStartCreateFormPrefillsProjectDefaultTargets confirms the fix this
// package's create flows exist for: a project with agentworks.yaml
// "targets:" already set shouldn't make every artifact re-pick them from a
// blank multi-select -- startCreateForm should carry the project's
// defaults into the form up front.
func TestStartCreateFormPrefillsProjectDefaultTargets(t *testing.T) {
	root := t.TempDir()
	if _, err := project.Init(root, "proj", []string{"claude-code", "chatgpt"}); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	m, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if m.newAnswers == nil {
		t.Fatal("newAnswers is nil, want it prefilled")
	}
	want := []string{"claude-code", "chatgpt"}
	if got := m.newAnswers.Targets; !equalStrings(got, want) {
		t.Errorf("newAnswers.Targets = %v, want %v (the project's default)", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStartCreateFormPrefillsCurrentTabKind(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if m.pane != paneForm {
		t.Fatalf("pane = %v, want paneForm", m.pane)
	}
	if m.formPurpose != formCreate {
		t.Fatalf("formPurpose = %v, want formCreate", m.formPurpose)
	}
	if m.activeForm == nil {
		t.Fatal("activeForm is nil, want a form")
	}
	if m.newAnswers == nil || m.newAnswers.Kind != string(artifact.KindSkill) {
		t.Fatalf("newAnswers = %+v, want Kind prefilled to skill", m.newAnswers)
	}
	if m.formReturnPane != paneBrowse {
		t.Fatalf("formReturnPane = %v, want paneBrowse", m.formReturnPane)
	}
	if cmd == nil {
		t.Error("expected a non-nil init cmd from starting the form")
	}
}

func TestNDoesNothingFromDetail(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("pane = %v, want paneDetail", m.pane)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("pane after 'n' from detail = %v, want unchanged paneDetail ('n' isn't valid there)", m.pane)
	}
}

func TestNIsIgnoredWhileFiltering(t *testing.T) {
	m := newTestModel(t)
	al := m.artifactLists[m.currentKind()]
	al.SetFilterState(list.Filtering)
	m.artifactLists[m.currentKind()] = al

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if m.pane != paneBrowse {
		t.Fatalf("pane = %v, want paneBrowse (filtering should absorb 'n' as text, not start a form)", m.pane)
	}
	if m.activeForm != nil {
		t.Error("activeForm should remain nil while filtering absorbs 'n'")
	}
}

func TestStartExportFormFromBrowseAndDetail(t *testing.T) {
	for _, drillToDetail := range []bool{false, true} {
		m := newTestModel(t)
		m = tabTo(m, artifact.KindSkill)
		if drillToDetail {
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(Model)
		}

		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
		m = updated.(Model)
		if m.pane != paneForm || m.formPurpose != formExport {
			t.Fatalf("drillToDetail=%v: pane=%v formPurpose=%v, want paneForm/formExport", drillToDetail, m.pane, m.formPurpose)
		}
		if m.exportSubject == nil || m.exportSubject.Name != "demo" {
			t.Errorf("drillToDetail=%v: exportSubject = %+v, want the demo skill", drillToDetail, m.exportSubject)
		}
	}
}

func TestCommitCreateScaffoldsAndRefreshesList(t *testing.T) {
	m := newTestModel(t)
	m.newAnswers = &NewArtifactAnswers{
		Kind:        string(artifact.KindTool),
		Name:        "new-tool",
		Description: "A brand new tool.",
	}

	updated, _ := m.commitCreate()
	m = updated.(Model)

	if m.pane != paneBrowse {
		t.Fatalf("pane = %v, want paneBrowse", m.pane)
	}
	if m.currentKind() != artifact.KindTool {
		t.Fatalf("currentKind() = %v, want tool", m.currentKind())
	}
	if got := len(m.artifactLists[artifact.KindTool].Items()); got != 1 {
		t.Fatalf("tool artifact list has %d items, want 1 (the new tool)", got)
	}
	if _, err := os.Stat(filepath.Join(m.root, "tools", "new-tool", "tool.md")); err != nil {
		t.Errorf("expected tools/new-tool/tool.md to exist on disk: %v", err)
	}
	if m.statusMsg == "" {
		t.Error("expected a non-empty statusMsg reporting the creation")
	}
	if m.statusLevel != statusSuccess {
		t.Errorf("statusLevel = %v, want statusSuccess", m.statusLevel)
	}

	// The tools tab's label should now reflect the new artifact's count.
	wantLabel := "tools (1)"
	if got := m.browseTabs.labels[kindIndex(artifact.KindTool)]; got != wantLabel {
		t.Errorf("tools tab label = %q, want %q", got, wantLabel)
	}
}

func TestCommitCreateInvalidKindReportsError(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneForm
	m.formReturnPane = paneBrowse
	m.newAnswers = &NewArtifactAnswers{Kind: "not-a-kind", Name: "x", Description: "x"}

	updated, _ := m.commitCreate()
	m = updated.(Model)
	if m.statusMsg == "" {
		t.Error("expected a statusMsg reporting the invalid kind")
	}
	if m.statusLevel != statusError {
		t.Errorf("statusLevel = %v, want statusError", m.statusLevel)
	}
}

func TestCommitExportRunsRealExporter(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)
	item, ok := m.artifactLists[artifact.KindSkill].SelectedItem().(artifactItem)
	if !ok {
		t.Fatal("no artifact selected")
	}

	m.exportSubject = item.a
	m.exportAnswers = &ExportAnswers{Target: "claude-code", Zip: false}

	updated, _ := m.commitExport()
	m = updated.(Model)

	wantSkillMD := filepath.Join(m.root, "dist", "demo", "SKILL.md")
	if _, err := os.Stat(wantSkillMD); err != nil {
		t.Errorf("expected %s to exist: %v", wantSkillMD, err)
	}
	if m.statusMsg == "" {
		t.Error("expected a non-empty statusMsg reporting the export")
	}
	if m.statusLevel != statusSuccess {
		t.Errorf("statusLevel = %v, want statusSuccess", m.statusLevel)
	}
}

func TestFinishFormAbortRestoresReturnPaneWithoutCommitting(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneForm
	m.formPurpose = formCreate
	m.formReturnPane = paneBrowse
	m.newAnswers = &NewArtifactAnswers{Kind: string(artifact.KindTool), Name: "should-not-exist", Description: "x"}

	updated, _ := m.finishForm(false)
	m = updated.(Model)

	if m.pane != paneBrowse {
		t.Fatalf("pane = %v, want paneBrowse", m.pane)
	}
	if m.newAnswers != nil {
		t.Error("newAnswers should be cleared on abort")
	}
	if _, err := os.Stat(filepath.Join(m.root, "tools", "should-not-exist")); err == nil {
		t.Error("aborting the form should not have scaffolded anything")
	}
}
