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

// RunMarketplace launches the same browser as Run, but opens straight into
// the marketplace search pane with a search already in flight -- the entry
// point for `agentworks add` with no URL in an interactive terminal.
func RunMarketplace(root string) error {
	m, err := New(root)
	if err != nil {
		return err
	}

	updated, cmd := m.startMarketplace()
	m = updated.(Model)
	m.initCmd = cmd

	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
