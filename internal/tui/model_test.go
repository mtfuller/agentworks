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

func TestNewListsAllKinds(t *testing.T) {
	m := newTestModel(t)
	if got := len(m.kindList.Items()); got != len(artifact.Kinds()) {
		t.Fatalf("kindList has %d items, want %d", got, len(artifact.Kinds()))
	}
}

func TestDrillFromKindsToArtifactToDetailAndBack(t *testing.T) {
	m := newTestModel(t)

	// Kinds() is [agent, skill, tool, hook, workflow]; move down once to
	// land on "skills", which has our one scaffolded artifact.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneArtifacts {
		t.Fatalf("pane = %v, want paneArtifacts", m.pane)
	}
	if got := len(m.artifactList.Items()); got != 1 {
		t.Fatalf("artifactList has %d items, want 1", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneDetail {
		t.Fatalf("pane = %v, want paneDetail", m.pane)
	}
	if !strings.Contains(m.viewport.View(), "demo") {
		t.Errorf("detail view missing artifact name, got: %q", m.viewport.View())
	}

	// "q" steps back one pane at a time rather than quitting immediately.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)
	if m.pane != paneArtifacts {
		t.Fatalf("pane after q from detail = %v, want paneArtifacts", m.pane)
	}
	if cmd != nil {
		t.Errorf("expected no quit cmd stepping back from detail, got one")
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)
	if m.pane != paneKinds {
		t.Fatalf("pane after q from artifacts = %v, want paneKinds", m.pane)
	}
	if cmd != nil {
		t.Errorf("expected no quit cmd stepping back from artifacts, got one")
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
