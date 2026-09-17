// Package tui is AgentWorks' Bubble Tea front end: a project browser
// (Model in this file), the actions it can take (actions.go -- create and
// export, both embedding a huh.Form as a child Bubble Tea model rather
// than shelling out to a second tea.Program), and the interactive
// "new artifact" wizard (wizard.go) that also backs `agentworks new` on
// the CLI. All built on the same internal/artifact, internal/project, and
// internal/scaffold packages the CLI uses -- the CLI and TUI never
// disagree about what a project or an artifact is.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

// pane identifies which of the browser's screens is active.
type pane int

const (
	paneKinds pane = iota
	paneArtifacts
	paneDetail
	paneForm        // a huh.Form (create or export) is active; see actions.go
	paneMarketplace // marketplace search/import is active; see marketplace.go
	paneTemplates   // browsing built-in starter templates; see templates.go
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	helpStyle   = lipgloss.NewStyle().Faint(true)
	statusStyle = lipgloss.NewStyle().Bold(true)
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
// rendered frontmatter + body -- plus, from the right pane, creating a new
// artifact ("n") or exporting the current one ("e"). See actions.go for
// both of those.
type Model struct {
	root string
	pane pane

	kindList        list.Model
	artifactList    list.Model
	marketplaceList list.Model
	templateList    list.Model
	viewport        viewport.Model

	// currentKind/currentArtifact track what's in view in paneArtifacts/
	// paneDetail, so "n"/"e" know what kind to scaffold into or which
	// artifact to export without re-deriving it from the list widgets.
	currentKind     artifact.Kind
	currentArtifact *artifact.Artifact

	// Form state for the active create/export action, if any -- see
	// actions.go's startCreateForm/startExportForm/updateForm/finishForm.
	activeForm     *huh.Form
	formPurpose    formPurpose
	formReturnPane pane
	newAnswers     *NewArtifactAnswers
	exportAnswers  *ExportAnswers
	exportSubject  *artifact.Artifact

	// statusMsg reports the outcome of the last create/export action,
	// shown in the footer until the next one replaces it.
	statusMsg string

	width, height int
	err           error

	// initCmd is returned by Init() on program start. It exists so
	// RunMarketplace can pre-seed the model (see app.go): calling
	// startMarketplace() before the tea.Program is constructed yields both
	// the already-mutated Model (pane set, spinner started) and the Cmd
	// that must run once the program starts, and Init() has no other way
	// to receive that Cmd from outside the Bubble Tea event loop.
	initCmd tea.Cmd
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

	// artifactList and marketplaceList start empty and are populated by
	// drillIn() / startMarketplace() respectively; they still need to exist
	// so an early WindowSizeMsg can size them safely.
	artifactList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	marketplaceList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	marketplaceList.Title = "Search skills & plugins"

	// Unlike artifactList/marketplaceList, templateList's data is local and
	// static -- there's nothing to fetch, so it's populated immediately
	// rather than lazily on first entering the pane.
	templateList := list.New(templateItems(scaffold.Templates()), list.NewDefaultDelegate(), 0, 0)
	templateList.Title = "Browse templates"

	return Model{
		root:            root,
		pane:            paneKinds,
		kindList:        kindList,
		artifactList:    artifactList,
		marketplaceList: marketplaceList,
		templateList:    templateList,
		viewport:        viewport.New(0, 0),
	}, nil
}

func (m Model) Init() tea.Cmd {
	return m.initCmd
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, regardless of pane or an in-progress form.
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if m.pane == paneForm {
		return m.updateForm(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		listHeight := msg.Height - 2
		m.kindList.SetSize(msg.Width, listHeight)
		m.artifactList.SetSize(msg.Width, listHeight)
		m.marketplaceList.SetSize(msg.Width, listHeight)
		m.templateList.SetSize(msg.Width, listHeight)
		m.viewport.Width = msg.Width
		m.viewport.Height = listHeight
		return m, nil

	case testFinishedMsg:
		return m.handleTestFinished(msg)

	case marketplaceResultsMsg:
		return m.handleMarketplaceResults(msg)

	case marketplaceImportedMsg:
		return m.handleMarketplaceImported(msg)

	case tea.KeyMsg:
		// While the user is typing into a list's filter box, single-key
		// shortcuts must fall through to it like any other character --
		// otherwise typing "new" into a filter would trigger "n".
		if !m.isFiltering() {
			switch msg.String() {
			case "q", "esc":
				return m.goBack()
			case "enter":
				return m.drillIn()
			case "n":
				if m.pane == paneKinds || m.pane == paneArtifacts {
					return m.startCreateForm()
				}
			case "e":
				if m.pane == paneArtifacts || m.pane == paneDetail {
					return m.startExportForm()
				}
			case "t":
				if m.pane == paneArtifacts || m.pane == paneDetail {
					return m.startTest()
				}
			case "a":
				if m.pane == paneKinds || m.pane == paneArtifacts {
					return m.startMarketplace()
				}
			case "r":
				if m.pane == paneMarketplace {
					return m.startMarketplace()
				}
			case "b":
				if m.pane == paneKinds || m.pane == paneArtifacts {
					return m.startTemplates()
				}
			}
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
	case paneMarketplace:
		m.marketplaceList, cmd = m.marketplaceList.Update(msg)
	case paneTemplates:
		m.templateList, cmd = m.templateList.Update(msg)
	}
	return m, cmd
}

func (m Model) isFiltering() bool {
	switch m.pane {
	case paneKinds:
		return m.kindList.FilterState() == list.Filtering
	case paneArtifacts:
		return m.artifactList.FilterState() == list.Filtering
	case paneMarketplace:
		return m.marketplaceList.FilterState() == list.Filtering
	case paneTemplates:
		return m.templateList.FilterState() == list.Filtering
	default:
		return false
	}
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
	case paneMarketplace:
		m.pane = paneKinds
	case paneTemplates:
		m.pane = paneKinds
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
		m.currentKind = item.kind
		m.pane = paneArtifacts
		return m, nil

	case paneArtifacts:
		item, ok := m.artifactList.SelectedItem().(artifactItem)
		if !ok {
			return m, nil
		}
		m.currentArtifact = item.a
		m.viewport.SetContent(renderArtifact(item.a))
		m.viewport.GotoTop()
		m.pane = paneDetail
		return m, nil

	case paneMarketplace:
		return m.importSelected()

	case paneTemplates:
		return m.startCreateFromTemplate()
	}
	return m, nil
}

func (m Model) View() string {
	if m.pane == paneForm {
		return m.activeForm.View()
	}

	var body string
	switch m.pane {
	case paneKinds:
		body = m.kindList.View()
	case paneArtifacts:
		body = m.artifactList.View()
	case paneDetail:
		body = m.viewport.View()
	case paneMarketplace:
		body = m.marketplaceList.View()
	case paneTemplates:
		body = m.templateList.View()
	}

	out := body + "\n" + helpStyle.Render(m.helpText())
	if m.statusMsg != "" {
		out += "\n" + statusStyle.Render(m.statusMsg)
	}
	return out
}

// helpText is the footer hint line, tailored to what's actually available
// from the current pane.
func (m Model) helpText() string {
	switch m.pane {
	case paneKinds:
		return "enter: open  •  n: new  •  a: add  •  b: templates  •  q: quit"
	case paneArtifacts:
		return "enter: open  •  n: new  •  e: export  •  t: test  •  a: add  •  b: templates  •  esc: back"
	case paneDetail:
		return "e: export  •  t: test  •  esc: back"
	case paneMarketplace:
		return "enter: import  •  /: filter loaded results  •  r: refresh  •  esc: back"
	case paneTemplates:
		return "enter: create from this template  •  /: filter  •  esc: back"
	default:
		return "esc: back  •  ctrl+c: quit"
	}
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
