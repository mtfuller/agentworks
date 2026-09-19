package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/export"
	"github.com/mtfuller/agentworks/internal/project"

	// Blank-imported so its init() registers the "claude-code" exporter
	// with internal/targets -- needed for TestCommitExportRunsRealExporter
	// to actually run one, the same way cmd/export.go registers it for
	// the real binary.
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
)

// TestCreateFormHasNoTargetsField confirms targets are a project-level
// setting: the new-artifact form never asks for them, so a project with
// targets configured opens the same three-field form as one without.
func TestCreateFormHasNoTargetsField(t *testing.T) {
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
		t.Fatal("newAnswers is nil, want the form's answers")
	}
	if view := m.View(); strings.Contains(strings.ToLower(view), "targets") {
		t.Errorf("create form still mentions targets:\n%s", view)
	}
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
		if m.exportAnswers == nil {
			t.Errorf("drillToDetail=%v: exportAnswers is nil, want the export form's answers", drillToDetail)
		}
	}
}

func TestCommitCreateScaffoldsAndRefreshesList(t *testing.T) {
	m := newTestModel(t)
	m.newAnswers = &NewArtifactAnswers{
		Kind:        string(artifact.KindMCP),
		Name:        "new-tool",
		Description: "A brand new tool.",
	}

	updated, _ := m.commitCreate()
	m = updated.(Model)

	if m.pane != paneBrowse {
		t.Fatalf("pane = %v, want paneBrowse", m.pane)
	}
	if m.currentKind() != artifact.KindMCP {
		t.Fatalf("currentKind() = %v, want tool", m.currentKind())
	}
	if got := len(m.artifactLists[artifact.KindMCP].Items()); got != 1 {
		t.Fatalf("tool artifact list has %d items, want 1 (the new tool)", got)
	}
	if _, err := os.Stat(filepath.Join(m.root, "mcp", "new-tool", "mcp.md")); err != nil {
		t.Errorf("expected mcp/new-tool/mcp.md to exist on disk: %v", err)
	}
	if m.statusMsg == "" {
		t.Error("expected a non-empty statusMsg reporting the creation")
	}
	if m.statusLevel != statusSuccess {
		t.Errorf("statusLevel = %v, want statusSuccess", m.statusLevel)
	}

	// The mcp tab's label should now reflect the new artifact's count.
	wantLabel := "mcp (1)"
	if got := m.browseTabs.labels[kindIndex(artifact.KindMCP)]; got != wantLabel {
		t.Errorf("mcp tab label = %q, want %q", got, wantLabel)
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

func TestCommitExportBundlesProjectForConfiguredTargets(t *testing.T) {
	m := newTestModel(t)
	setProjectTargets(t, m.root, "claude-code")

	m.exportAnswers = &ExportAnswers{Format: string(export.FormatPlugin)}
	updated, _ := m.commitExport()
	m = updated.(Model)

	want := filepath.Join(m.root, "dist", "claude-code", "proj", "skills", "demo", "SKILL.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected %s to exist: %v (status: %s)", want, err, m.statusMsg)
	}
	if m.statusLevel != statusSuccess {
		t.Errorf("statusLevel = %v (%s), want statusSuccess", m.statusLevel, m.statusMsg)
	}
}

func TestCommitExportPluginWithoutTargetsReportsHowToFixIt(t *testing.T) {
	m := newTestModel(t) // project has no targets

	m.exportAnswers = &ExportAnswers{Format: string(export.FormatPlugin)}
	updated, _ := m.commitExport()
	m = updated.(Model)

	if m.statusLevel != statusError || !strings.Contains(m.statusMsg, "targets:") {
		t.Errorf("status = %v %q, want an error pointing at agentworks.yaml targets", m.statusLevel, m.statusMsg)
	}
}

func TestCommitExportSkillsZipNeedsNoTargets(t *testing.T) {
	m := newTestModel(t)

	m.exportAnswers = &ExportAnswers{Format: string(export.FormatSkillsZip)}
	updated, _ := m.commitExport()
	m = updated.(Model)

	if _, err := os.Stat(filepath.Join(m.root, "dist", "proj-skills.zip")); err != nil {
		t.Errorf("expected the skills zip: %v (status: %s)", err, m.statusMsg)
	}
}

func setProjectTargets(t *testing.T, root string, targets ...string) {
	t.Helper()
	body := "name: proj\ntargets:\n"
	for _, tg := range targets {
		body += "  - " + tg + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, project.ManifestFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFinishFormAbortRestoresReturnPaneWithoutCommitting(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneForm
	m.formPurpose = formCreate
	m.formReturnPane = paneBrowse
	m.newAnswers = &NewArtifactAnswers{Kind: string(artifact.KindMCP), Name: "should-not-exist", Description: "x"}

	updated, _ := m.finishForm(false)
	m = updated.(Model)

	if m.pane != paneBrowse {
		t.Fatalf("pane = %v, want paneBrowse", m.pane)
	}
	if m.newAnswers != nil {
		t.Error("newAnswers should be cleared on abort")
	}
	if _, err := os.Stat(filepath.Join(m.root, "mcp", "should-not-exist")); err == nil {
		t.Error("aborting the form should not have scaffolded anything")
	}
}
