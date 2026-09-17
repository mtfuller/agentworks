package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartTestWithNoCommandReportsStatus(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pane != paneArtifacts {
		t.Fatalf("pane = %v, want paneArtifacts", m.pane)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(Model)
	if cmd != nil {
		t.Error("expected no tea.ExecProcess cmd when the artifact has no test: command")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a statusMsg reporting no test: command")
	}
	// Pane must not change -- unlike "n"/"e", "t" never enters paneForm.
	if m.pane != paneArtifacts {
		t.Errorf("pane after 't' with no test command = %v, want unchanged paneArtifacts", m.pane)
	}
}

func TestStartTestWithCommandReturnsExecCmd(t *testing.T) {
	m := newTestModel(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	item, ok := m.artifactList.SelectedItem().(artifactItem)
	if !ok {
		t.Fatal("no artifact selected")
	}
	item.a.Extra["test"] = "true"
	if err := item.a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// startTest's returned cmd wraps tea.ExecProcess: calling it directly
	// (rather than through a running tea.Program) only unwraps to the
	// package-internal execMsg bubbletea uses to actually spawn the
	// process with the terminal suspended -- that machinery is
	// bubbletea's own, tested concern. What's ours to verify is that a
	// declared test: command produces a non-nil cmd at all.
	_, cmd := m.startTest()
	if cmd == nil {
		t.Fatal("expected a non-nil cmd when the artifact declares a test: command")
	}
}

func TestHandleTestFinishedReportsPassOrFail(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.handleTestFinished(testFinishedMsg{name: "demo", err: nil})
	m = updated.(Model)
	if m.statusMsg == "" {
		t.Fatal("expected a statusMsg reporting a passing test")
	}

	updated, _ = m.handleTestFinished(testFinishedMsg{name: "demo", err: errBoom})
	m = updated.(Model)
	if m.statusMsg == "" {
		t.Fatal("expected a statusMsg reporting a failing test")
	}
}

// errBoom is a stand-in error for TestHandleTestFinishedReportsPassOrFail.
var errBoom = fmt.Errorf("boom")

func TestTIsNoOpFromKindsPane(t *testing.T) {
	m := newTestModel(t)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(Model)
	if m.pane != paneKinds {
		t.Fatalf("pane = %v, want unchanged paneKinds", m.pane)
	}
	if cmd != nil {
		t.Error("expected no cmd for 't' from paneKinds")
	}
}
