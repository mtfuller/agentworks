package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/scaffold"
)

// templateItem adapts a scaffold.Template to bubbles/list's list.Item, the
// same shape as artifactItem/marketplaceItem. Its own kind isn't repeated
// in Description() -- the templates pane is tabbed by kind (see tabs.go),
// so whichever tab is active already says that.
type templateItem struct {
	t scaffold.Template
}

func (i templateItem) Title() string       { return i.t.Title }
func (i templateItem) Description() string { return i.t.Description }
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
// pane, there's no fetch: templateLists are already populated in New(),
// since the built-in template set is local, static data that never
// changes mid-session. The templates tab starts on whatever kind the
// browse pane was showing, so pressing "b" while looking at "skills"
// lands on the "skill" tab instead of always the first kind.
func (m Model) startTemplates() (tea.Model, tea.Cmd) {
	m.templateTabs.active = m.browseTabs.active
	m.clearStatus()
	m.pane = paneTemplates
	return m, nil
}

// startCreateFromTemplate opens the same create form manual creation uses
// ("n"), pre-filled with the selected template's kind, description, and
// the project's default targets -- commitCreate (actions.go) threads
// Template through to scaffold.New unchanged.
func (m Model) startCreateFromTemplate() (tea.Model, tea.Cmd) {
	item, ok := m.templateLists[m.currentTemplateKind()].SelectedItem().(templateItem)
	if !ok {
		return m, nil
	}

	defaults := NewArtifactAnswers{
		Kind:        string(item.t.Kind),
		Description: item.t.Description,
		Template:    item.t.ID,
		Targets:     append([]string(nil), m.defaultTargets...),
	}
	form, answers := newArtifactForm(defaults)
	m.newAnswers = answers
	m.formPurpose = formCreate
	m.formReturnPane = m.pane
	m.activeForm = m.sizeForm(form)
	m.clearStatus()
	m.pane = paneForm
	return m, m.activeForm.Init()
}
