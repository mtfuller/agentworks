package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestStartTestWithNoCommandReportsStatus(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(Model)
	if cmd != nil {
		t.Error("expected no tea.ExecProcess cmd when the artifact has no test: command")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a statusMsg reporting no test: command")
	}
	if m.statusLevel != statusWarn {
		t.Errorf("statusLevel = %v, want statusWarn", m.statusLevel)
	}
	// Pane must not change -- unlike "n"/"e", "t" never enters paneForm.
	if m.pane != paneBrowse {
		t.Errorf("pane after 't' with no test command = %v, want unchanged paneBrowse", m.pane)
	}
}

func TestStartTestWithCommandReturnsExecCmd(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)

	item, ok := m.artifactLists[artifact.KindSkill].SelectedItem().(artifactItem)
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
	if m.statusLevel != statusSuccess {
		t.Errorf("statusLevel = %v, want statusSuccess", m.statusLevel)
	}

	updated, _ = m.handleTestFinished(testFinishedMsg{name: "demo", err: errBoom})
	m = updated.(Model)
	if m.statusMsg == "" {
		t.Fatal("expected a statusMsg reporting a failing test")
	}
	if m.statusLevel != statusError {
		t.Errorf("statusLevel = %v, want statusError", m.statusLevel)
	}
}

// errBoom is a stand-in error for TestHandleTestFinishedReportsPassOrFail.
var errBoom = fmt.Errorf("boom")

func TestTIsNoOpFromMarketplace(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneMarketplace

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = updated.(Model)
	if m.pane != paneMarketplace {
		t.Fatalf("pane = %v, want unchanged paneMarketplace", m.pane)
	}
	if cmd != nil {
		t.Error("expected no cmd for 't' from paneMarketplace")
	}
}
