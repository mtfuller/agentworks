package tui

import (
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// testFinishedMsg reports the outcome of a test command started by
// startTest, once tea.ExecProcess's suspended process exits.
type testFinishedMsg struct {
	name string
	err  error
}

// startTest shells out to the selected/viewed artifact's declared "test:"
// command, the same way `agentworks test` does (see cmd/test.go), via
// tea.ExecProcess -- which suspends the Bubble Tea renderer and hands the
// real terminal to the child process, so its output (a pytest run, go
// test -v, whatever) shows live and unmangled instead of fighting the
// alt-screen buffer.
func (m Model) startTest() (tea.Model, tea.Cmd) {
	subject := m.selectedArtifact()
	if subject == nil {
		return m, nil
	}

	testCommand := subject.ExtraString("test")
	if testCommand == "" {
		m.statusMsg = fmt.Sprintf("%s has no \"test:\" command declared", subject.Name)
		return m, nil
	}

	c := exec.Command("sh", "-c", testCommand)
	c.Dir = subject.Dir
	name := subject.Name
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return testFinishedMsg{name: name, err: err}
	})
}

func (m Model) handleTestFinished(msg testFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("%s: tests failed: %v", msg.name, msg.err)
	} else {
		m.statusMsg = fmt.Sprintf("%s: tests passed", msg.name)
	}
	return m, nil
}
