package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"

	// Blank-imported so its init() registers the "claude-code" exporter
	// with internal/targets -- needed for TestCommitExportRunsRealExporter
	// to actually run one, the same way cmd/export.go registers it for
	// the real binary.
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
)

func TestStartCreateFormFromKinds(t *testing.T) {
	m := newTestModel(t)
	// Kinds() is [agent, skill, tool, hook, workflow]; move down once to
	// highlight "skills".
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)

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
	if m.formReturnPane != paneKinds {
		t.Fatalf("formReturnPane = %v, want paneKinds", m.formReturnPane)
	}
	if cmd == nil {
		t.Error("expected a non-nil init cmd from starting the form")
	}
}

func TestStartCreateFormFromArtifacts(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneArtifacts {
		t.Fatalf("pane = %v, want paneArtifacts", m.pane)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if m.pane != paneForm || m.formPurpose != formCreate {
		t.Fatalf("pane=%v formPurpose=%v, want paneForm/formCreate", m.pane, m.formPurpose)
	}
	if m.newAnswers.Kind != string(artifact.KindSkill) {
		t.Errorf("newAnswers.Kind = %q, want skill (from currentKind)", m.newAnswers.Kind)
	}
	if m.formReturnPane != paneArtifacts {
		t.Errorf("formReturnPane = %v, want paneArtifacts", m.formReturnPane)
	}
}

func TestNDoesNothingFromDetail(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
	m.kindList.SetFilterState(list.Filtering)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if m.pane != paneKinds {
		t.Fatalf("pane = %v, want paneKinds (filtering should absorb 'n' as text, not start a form)", m.pane)
	}
	if m.activeForm != nil {
		t.Error("activeForm should remain nil while filtering absorbs 'n'")
	}
}

func TestStartExportFormFromArtifactsAndDetail(t *testing.T) {
	for _, drillToDetail := range []bool{false, true} {
		m := newTestModel(t)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if drillToDetail {
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(Model)
		}

		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
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

	if m.pane != paneArtifacts {
		t.Fatalf("pane = %v, want paneArtifacts", m.pane)
	}
	if m.currentKind != artifact.KindTool {
		t.Fatalf("currentKind = %v, want tool", m.currentKind)
	}
	if got := len(m.artifactList.Items()); got != 1 {
		t.Fatalf("artifactList has %d items, want 1 (the new tool)", got)
	}
	if _, err := os.Stat(filepath.Join(m.root, "tools", "new-tool", "tool.md")); err != nil {
		t.Errorf("expected tools/new-tool/tool.md to exist on disk: %v", err)
	}
	if m.statusMsg == "" {
		t.Error("expected a non-empty statusMsg reporting the creation")
	}

	// The kind list's count for "tools" should now reflect the new artifact.
	for _, item := range m.kindList.Items() {
		ki, ok := item.(kindItem)
		if ok && ki.kind == artifact.KindTool && ki.count != 1 {
			t.Errorf("tools kindItem count = %d, want 1", ki.count)
		}
	}
}

func TestCommitCreateInvalidKindReportsError(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneForm
	m.formReturnPane = paneKinds
	m.newAnswers = &NewArtifactAnswers{Kind: "not-a-kind", Name: "x", Description: "x"}

	updated, _ := m.commitCreate()
	m = updated.(Model)
	if m.statusMsg == "" {
		t.Error("expected a statusMsg reporting the invalid kind")
	}
}

func TestCommitExportRunsRealExporter(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	item, ok := m.artifactList.SelectedItem().(artifactItem)
	if !ok {
		t.Fatal("no artifact selected")
	}

	m.exportSubject = item.a
	m.exportAnswers = &ExportAnswers{Target: "claude-code", Zip: false}

	updated, _ = m.commitExport()
	m = updated.(Model)

	wantSkillMD := filepath.Join(m.root, "dist", "demo", "SKILL.md")
	if _, err := os.Stat(wantSkillMD); err != nil {
		t.Errorf("expected %s to exist: %v", wantSkillMD, err)
	}
	if m.statusMsg == "" {
		t.Error("expected a non-empty statusMsg reporting the export")
	}
}

func TestFinishFormAbortRestoresReturnPaneWithoutCommitting(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneForm
	m.formPurpose = formCreate
	m.formReturnPane = paneKinds
	m.newAnswers = &NewArtifactAnswers{Kind: string(artifact.KindTool), Name: "should-not-exist", Description: "x"}

	updated, _ := m.finishForm(false)
	m = updated.(Model)

	if m.pane != paneKinds {
		t.Fatalf("pane = %v, want paneKinds", m.pane)
	}
	if m.newAnswers != nil {
		t.Error("newAnswers should be cleared on abort")
	}
	if _, err := os.Stat(filepath.Join(m.root, "tools", "should-not-exist")); err == nil {
		t.Error("aborting the form should not have scaffolded anything")
	}
}
