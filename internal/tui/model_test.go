package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	if _, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "A demo skill."}); err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	m, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(Model)
}

// tabTo presses "tab" until browseTabs (or templateTabs, whichever the
// current pane uses) is on kind -- deterministic regardless of
// artifact.Kinds()'s order, since it just wraps around instead of assuming
// a starting position.
func tabTo(m Model, kind artifact.Kind) Model {
	for i := 0; i < len(artifact.Kinds()); i++ {
		var current artifact.Kind
		switch m.pane {
		case paneTemplates:
			current = m.currentTemplateKind()
		default:
			current = m.currentKind()
		}
		if current == kind {
			return m
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(Model)
	}
	return m
}

func TestNewBuildsATabAndListPerKind(t *testing.T) {
	m := newTestModel(t)
	if got := len(m.browseTabs.labels); got != len(artifact.Kinds()) {
		t.Fatalf("browseTabs has %d labels, want %d", got, len(artifact.Kinds()))
	}
	for _, k := range artifact.Kinds() {
		if _, ok := m.artifactLists[k]; !ok {
			t.Errorf("artifactLists missing an entry for %s", k)
		}
		if _, ok := m.templateLists[k]; !ok {
			t.Errorf("templateLists missing an entry for %s", k)
		}
	}
	if got := len(m.artifactLists[artifact.KindSkill].Items()); got != 1 {
		t.Fatalf("skill artifact list has %d items, want 1 (the scaffolded demo skill)", got)
	}
}

func TestTabSwitchesKindWithoutLeavingBrowse(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)
	if m.pane != paneBrowse {
		t.Fatalf("pane = %v, want paneBrowse (tab switches kind, it doesn't drill in)", m.pane)
	}
	if m.currentKind() != artifact.KindSkill {
		t.Fatalf("currentKind() = %v, want skill", m.currentKind())
	}
}

func TestDrillFromBrowseToDetailAndBack(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("pane = %v, want paneDetail", m.pane)
	}
	if !strings.Contains(m.viewport.View(), "demo") {
		t.Errorf("detail view missing artifact name, got: %q", m.viewport.View())
	}

	// "q" steps back to the browse pane rather than quitting immediately.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)
	if m.pane != paneBrowse {
		t.Fatalf("pane after q from detail = %v, want paneBrowse", m.pane)
	}
	if cmd != nil {
		t.Errorf("expected no quit cmd stepping back from detail, got one")
	}

	// "q" from the top-level pane quits.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected a quit cmd from the top-level pane, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", msg)
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a quit cmd for ctrl+c, got nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}
