package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the full-screen project browser for the project at root. It
// blocks until the user quits.
func Run(root string) error {
	m, err := New(root)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
