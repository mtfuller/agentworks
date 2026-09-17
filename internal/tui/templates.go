package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/scaffold"
)

// templateItem adapts a scaffold.Template to bubbles/list's list.Item, the
// same shape as kindItem/artifactItem/marketplaceItem.
type templateItem struct {
	t scaffold.Template
}

func (i templateItem) Title() string { return i.t.Title }
func (i templateItem) Description() string {
	return fmt.Sprintf("%s -- %s", i.t.Kind, i.t.Description)
}
func (i templateItem) FilterValue() string {
	return i.t.Title + " " + string(i.t.Kind) + " " + i.t.Description
}

// templateItems converts templates into list.Items -- kept as its own
// function so the conversion is testable without a running program.
func templateItems(templates []scaffold.Template) []list.Item {
	items := make([]list.Item, len(templates))
	for i, t := range templates {
		items[i] = templateItem{t: t}
	}
	return items
}

// startTemplates opens the browse-templates pane. Unlike the marketplace
// pane, there's no fetch: templateList is already populated in New(), since
// the built-in template set is local, static data that never changes
// mid-session.
func (m Model) startTemplates() (tea.Model, tea.Cmd) {
	m.statusMsg = ""
	m.pane = paneTemplates
	return m, nil
}

// startCreateFromTemplate opens the same create form manual creation uses
// ("n"), pre-filled with the selected template's kind and description --
// commitCreate (actions.go) threads Template through to scaffold.New
// unchanged.
func (m Model) startCreateFromTemplate() (tea.Model, tea.Cmd) {
	item, ok := m.templateList.SelectedItem().(templateItem)
	if !ok {
		return m, nil
	}

	defaults := NewArtifactAnswers{
		Kind:        string(item.t.Kind),
		Description: item.t.Description,
		Template:    item.t.ID,
	}
	form, answers := newArtifactForm(defaults)
	m.newAnswers = answers
	m.formPurpose = formCreate
	m.formReturnPane = m.pane
	m.activeForm = m.sizeForm(form)
	m.statusMsg = ""
	m.pane = paneForm
	return m, m.activeForm.Init()
}
