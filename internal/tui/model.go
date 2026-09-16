// Package tui is AgentWorks' Bubble Tea front end: a project browser
// (Model in this file) and the interactive "new artifact" wizard
// (wizard.go), both built on the same internal/artifact, internal/project,
// and internal/scaffold packages the CLI uses -- so the CLI and TUI never
// disagree about what a project or an artifact is.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
)

// pane identifies which of the browser's three screens is active.
type pane int

const (
	paneKinds pane = iota
	paneArtifacts
	paneDetail
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	helpStyle  = lipgloss.NewStyle().Faint(true)
)

type kindItem struct {
	kind  artifact.Kind
	count int
}

func (i kindItem) Title() string       { return i.kind.DirName() }
func (i kindItem) Description() string { return fmt.Sprintf("%d artifact(s)", i.count) }
func (i kindItem) FilterValue() string { return string(i.kind) }

type artifactItem struct {
	a *artifact.Artifact
}

func (i artifactItem) Title() string       { return i.a.Name }
func (i artifactItem) Description() string { return i.a.Description }
func (i artifactItem) FilterValue() string { return i.a.Name }

// Model is the root Bubble Tea model for `agentworks tui`: a drill-down
// browser from artifact kinds, to that kind's artifacts, to one artifact's
// rendered frontmatter + body.
type Model struct {
	root string
	pane pane

	kindList     list.Model
	artifactList list.Model
	viewport     viewport.Model

	width, height int
	err           error
}

// New builds a browser Model rooted at an AgentWorks project directory.
func New(root string) (Model, error) {
	items := make([]list.Item, 0, len(artifact.Kinds()))
	for _, k := range artifact.Kinds() {
		found, errs := project.Discover(root, k)
		if len(errs) > 0 {
			return Model{}, errs[0]
		}
		items = append(items, kindItem{kind: k, count: len(found)})
	}

	kindList := list.New(items, list.NewDefaultDelegate(), 0, 0)
	kindList.Title = "AgentWorks"

	// artifactList starts empty and is populated in drillIn(); it still
	// needs to exist so an early WindowSizeMsg can size it safely.
	artifactList := list.New(nil, list.NewDefaultDelegate(), 0, 0)

	return Model{
		root:         root,
		pane:         paneKinds,
		kindList:     kindList,
		artifactList: artifactList,
		viewport:     viewport.New(0, 0),
	}, nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		listHeight := msg.Height - 2
		m.kindList.SetSize(msg.Width, listHeight)
		m.artifactList.SetSize(msg.Width, listHeight)
		m.viewport.Width = msg.Width
		m.viewport.Height = listHeight
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			return m.goBack()
		case "enter":
			return m.drillIn()
		}
	}

	var cmd tea.Cmd
	switch m.pane {
	case paneKinds:
		m.kindList, cmd = m.kindList.Update(msg)
	case paneArtifacts:
		m.artifactList, cmd = m.artifactList.Update(msg)
	case paneDetail:
		m.viewport, cmd = m.viewport.Update(msg)
	}
	return m, cmd
}

// goBack handles "q"/"esc": quit from the top-level pane, otherwise step up
// one level, the same convention as `less`/`man` pagers.
func (m Model) goBack() (tea.Model, tea.Cmd) {
	switch m.pane {
	case paneKinds:
		return m, tea.Quit
	case paneArtifacts:
		m.pane = paneKinds
	case paneDetail:
		m.pane = paneArtifacts
	}
	return m, nil
}

// drillIn handles "enter": kinds -> that kind's artifacts -> one artifact's
// detail view.
func (m Model) drillIn() (tea.Model, tea.Cmd) {
	switch m.pane {
	case paneKinds:
		item, ok := m.kindList.SelectedItem().(kindItem)
		if !ok {
			return m, nil
		}
		found, errs := project.Discover(m.root, item.kind)
		if len(errs) > 0 {
			m.err = errs[0]
			return m, nil
		}
		items := make([]list.Item, len(found))
		for i, a := range found {
			items[i] = artifactItem{a: a}
		}
		m.artifactList = list.New(items, list.NewDefaultDelegate(), m.width, m.height-2)
		m.artifactList.Title = item.Title()
		m.pane = paneArtifacts
		return m, nil

	case paneArtifacts:
		item, ok := m.artifactList.SelectedItem().(artifactItem)
		if !ok {
			return m, nil
		}
		m.viewport.SetContent(renderArtifact(item.a))
		m.viewport.GotoTop()
		m.pane = paneDetail
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	var body string
	switch m.pane {
	case paneKinds:
		body = m.kindList.View()
	case paneArtifacts:
		body = m.artifactList.View()
	case paneDetail:
		body = m.viewport.View()
	}
	help := helpStyle.Render("enter: open  •  q/esc: back  •  ctrl+c: quit")
	return body + "\n" + help
}

func renderArtifact(a *artifact.Artifact) string {
	header := titleStyle.Render(fmt.Sprintf("%s (%s)", a.Name, a.Kind))
	var meta strings.Builder
	fmt.Fprintf(&meta, "Description: %s\n", a.Description)
	if a.Version != "" {
		fmt.Fprintf(&meta, "Version:     %s\n", a.Version)
	}
	if len(a.Targets) > 0 {
		fmt.Fprintf(&meta, "Targets:     %s\n", strings.Join(a.Targets, ", "))
	}
	fmt.Fprintf(&meta, "Path:        %s\n", a.Dir)

	return header + "\n\n" + meta.String() + "\n" + a.Body
}
