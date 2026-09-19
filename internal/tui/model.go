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

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

// pane identifies which of the browser's screens is active.
type pane int

const (
	// paneBrowse is the top-level screen: a tab per artifact.Kind (see
	// tabs.go), each tab showing that kind's artifacts directly -- no
	// separate "pick a kind, then see its artifacts" step.
	paneBrowse pane = iota
	paneDetail
	paneForm        // a huh.Form (create or export) is active; see actions.go
	paneMarketplace // marketplace search/import is active; see marketplace.go
	paneTemplates   // browsing built-in starter templates; see templates.go
)

type artifactItem struct {
	a *artifact.Artifact
}

func (i artifactItem) Title() string       { return i.a.DisplayName() }
func (i artifactItem) Description() string { return i.a.Description }
func (i artifactItem) FilterValue() string { return i.a.DisplayName() }

// Model is the root Bubble Tea model for `agentworks tui`: a tabbed
// browser from artifact kind to that kind's artifacts, to one artifact's
// rendered frontmatter + body -- plus, from the browse pane, creating a
// new artifact ("n") or exporting/testing the current one ("e"/"t"). See
// actions.go for those.
type Model struct {
	root string
	pane pane

	// browseTabs/artifactLists back paneBrowse: one tab and one list.Model
	// per artifact.Kind (in artifact.Kinds() order, so browseTabs.active
	// doubles as an index into that slice -- see currentKind).
	browseTabs    tabBar
	artifactLists map[artifact.Kind]list.Model

	// templateTabs/templateLists are the same shape, backing paneTemplates.
	templateTabs  tabBar
	templateLists map[artifact.Kind]list.Model

	// marketplaceList/marketplaceDetail back paneMarketplace: the list of
	// plugins on the left, the highlighted one's details on the right.
	// mpPreviews caches what each plugin would import (fetched lazily, see
	// marketplace.go) keyed by its source, and mpShown is the key currently
	// highlighted.
	marketplaceList   list.Model
	marketplaceDetail viewport.Model
	mpPreviews        map[string]*previewState
	mpShown           string

	viewport viewport.Model

	// currentArtifact tracks what's in view in paneDetail, so "e"/"t" know
	// which artifact to act on without re-deriving it from a list widget.
	currentArtifact *artifact.Artifact

	// Form state for the active create/export action, if any -- see
	// actions.go's startCreateForm/startExportForm/updateForm/finishForm.
	activeForm     *huh.Form
	formPurpose    formPurpose
	formReturnPane pane
	newAnswers     *NewArtifactAnswers
	exportAnswers  *ExportAnswers

	// statusMsg/statusLevel report the outcome of the last create/export/
	// test/import action, shown in the footer (colored by level) until the
	// next one replaces it. See styles.go's setStatus/clearStatus.
	statusMsg   string
	statusLevel statusLevel

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
	kinds := artifact.Kinds()

	browseLabels := make([]string, len(kinds))
	templateLabels := make([]string, len(kinds))
	artifactLists := make(map[artifact.Kind]list.Model, len(kinds))
	templateLists := make(map[artifact.Kind]list.Model, len(kinds))

	for i, k := range kinds {
		found, errs := project.Discover(root, k)
		if len(errs) > 0 {
			return Model{}, errs[0]
		}
		browseLabels[i] = fmt.Sprintf("%s (%d)", k.DirName(), len(found))

		items := make([]list.Item, len(found))
		for j, a := range found {
			items[j] = artifactItem{a: a}
		}
		al := list.New(items, list.NewDefaultDelegate(), 0, 0)
		al.SetShowTitle(false)
		artifactLists[k] = al

		tmpls := scaffold.TemplatesForKind(k)
		templateLabels[i] = fmt.Sprintf("%s (%d)", k.DirName(), len(tmpls))
		tl := list.New(templateItems(tmpls), list.NewDefaultDelegate(), 0, 0)
		tl.SetShowTitle(false)
		templateLists[k] = tl
	}

	// marketplaceList starts empty and is populated by startMarketplace();
	// it still needs to exist so an early WindowSizeMsg can size it safely.
	marketplaceList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	marketplaceList.Title = "Browse plugins"
	marketplaceList.SetShowHelp(false)

	return Model{
		root:              root,
		pane:              paneBrowse,
		browseTabs:        newTabBar(browseLabels),
		artifactLists:     artifactLists,
		templateTabs:      newTabBar(templateLabels),
		templateLists:     templateLists,
		marketplaceList:   marketplaceList,
		marketplaceDetail: viewport.New(0, 0),
		mpPreviews:        map[string]*previewState{},
		viewport:          viewport.New(0, 0),
	}, nil
}

// currentKind is whichever kind's tab is active in paneBrowse.
func (m Model) currentKind() artifact.Kind {
	return artifact.Kinds()[m.browseTabs.active]
}

// currentTemplateKind is whichever kind's tab is active in paneTemplates.
func (m Model) currentTemplateKind() artifact.Kind {
	return artifact.Kinds()[m.templateTabs.active]
}

// kindIndex returns k's position in artifact.Kinds(), so a tab bar's
// active index can be set to match a specific kind (e.g. after creating an
// artifact, or when "b" carries the browse tab's kind into templates).
func kindIndex(k artifact.Kind) int {
	for i, kk := range artifact.Kinds() {
		if kk == k {
			return i
		}
	}
	return 0
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
		footerHeight := 2
		tabbedListHeight := msg.Height - footerHeight - tabBarHeight
		for _, k := range artifact.Kinds() {
			al := m.artifactLists[k]
			al.SetSize(msg.Width, tabbedListHeight)
			m.artifactLists[k] = al

			tl := m.templateLists[k]
			tl.SetSize(msg.Width, tabbedListHeight)
			m.templateLists[k] = tl
		}
		listW, detailW := marketplaceWidths(msg.Width)
		m.marketplaceList.SetSize(listW, msg.Height-footerHeight)
		m.marketplaceDetail.Width = detailW
		m.marketplaceDetail.Height = msg.Height - footerHeight
		m.refreshMarketplaceDetail()
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - footerHeight
		return m, nil

	case testFinishedMsg:
		return m.handleTestFinished(msg)

	case marketplaceResultsMsg:
		return m.handleMarketplaceResults(msg)

	case marketplaceImportedMsg:
		return m.handleMarketplaceImported(msg)

	case previewTickMsg:
		return m.handlePreviewTick(msg)

	case previewLoadedMsg:
		return m.handlePreviewLoaded(msg)

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
				if m.pane == paneBrowse {
					return m.startCreateForm()
				}
			case "e":
				if m.pane == paneBrowse || m.pane == paneDetail {
					return m.startExportForm()
				}
			case "t":
				if m.pane == paneBrowse || m.pane == paneDetail {
					return m.startTest()
				}
			case "p", "a":
				if m.pane == paneBrowse {
					return m.startMarketplace()
				}
			case "ctrl+d":
				if m.pane == paneMarketplace {
					m.marketplaceDetail.HalfPageDown()
					return m, nil
				}
			case "ctrl+u":
				if m.pane == paneMarketplace {
					m.marketplaceDetail.HalfPageUp()
					return m, nil
				}
			case "r":
				if m.pane == paneMarketplace {
					return m.startMarketplace()
				}
			case "b":
				if m.pane == paneBrowse {
					return m.startTemplates()
				}
			case "tab":
				switch m.pane {
				case paneBrowse:
					m.browseTabs.next()
					return m, nil
				case paneTemplates:
					m.templateTabs.next()
					return m, nil
				}
			case "shift+tab":
				switch m.pane {
				case paneBrowse:
					m.browseTabs.prev()
					return m, nil
				case paneTemplates:
					m.templateTabs.prev()
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	switch m.pane {
	case paneBrowse:
		k := m.currentKind()
		al := m.artifactLists[k]
		al, cmd = al.Update(msg)
		m.artifactLists[k] = al
	case paneDetail:
		m.viewport, cmd = m.viewport.Update(msg)
	case paneMarketplace:
		var syncCmd tea.Cmd
		m.marketplaceList, cmd = m.marketplaceList.Update(msg)
		syncCmd = m.syncMarketplaceDetail()
		cmd = tea.Batch(cmd, syncCmd)
	case paneTemplates:
		k := m.currentTemplateKind()
		tl := m.templateLists[k]
		tl, cmd = tl.Update(msg)
		m.templateLists[k] = tl
	}
	return m, cmd
}

func (m Model) isFiltering() bool {
	switch m.pane {
	case paneBrowse:
		return m.artifactLists[m.currentKind()].FilterState() == list.Filtering
	case paneMarketplace:
		return m.marketplaceList.FilterState() == list.Filtering
	case paneTemplates:
		return m.templateLists[m.currentTemplateKind()].FilterState() == list.Filtering
	default:
		return false
	}
}

// goBack handles "q"/"esc": quit from the top-level pane, otherwise step up
// one level, the same convention as `less`/`man` pagers.
func (m Model) goBack() (tea.Model, tea.Cmd) {
	switch m.pane {
	case paneBrowse:
		return m, tea.Quit
	case paneDetail, paneMarketplace, paneTemplates:
		m.pane = paneBrowse
	}
	return m, nil
}

// drillIn handles "enter": the current tab's artifact list -> its detail
// view, a marketplace result -> import, or a template -> the pre-filled
// create form.
func (m Model) drillIn() (tea.Model, tea.Cmd) {
	switch m.pane {
	case paneBrowse:
		item, ok := m.artifactLists[m.currentKind()].SelectedItem().(artifactItem)
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
	case paneBrowse:
		body = m.browseTabs.View(m.width) + "\n" + m.artifactLists[m.currentKind()].View()
	case paneDetail:
		body = m.viewport.View()
	case paneMarketplace:
		body = m.marketplaceView()
	case paneTemplates:
		body = m.templateTabs.View(m.width) + "\n" + m.templateLists[m.currentTemplateKind()].View()
	}

	out := body + "\n" + renderHelp(m.helpEntries())
	if m.statusMsg != "" {
		out += "\n" + m.statusLevel.style().Render(m.statusMsg)
	}
	return out
}

// helpEntries is the footer hint line's content, tailored to what's
// actually available from the current pane.
func (m Model) helpEntries() []helpEntry {
	switch m.pane {
	case paneBrowse:
		return []helpEntry{
			{"enter", "open"}, {"n", "new"}, {"e", "export"}, {"t", "test"},
			{"p", "browse plugins"}, {"b", "templates"}, {"tab/shift+tab", "switch kind"}, {"q", "quit"},
		}
	case paneDetail:
		return []helpEntry{{"e", "export"}, {"t", "test"}, {"esc", "back"}}
	case paneMarketplace:
		return []helpEntry{{"enter", "import"}, {"/", "filter"}, {"ctrl+d/u", "scroll details"}, {"r", "refresh"}, {"esc", "back"}}
	case paneTemplates:
		return []helpEntry{
			{"enter", "create from this template"}, {"/", "filter"},
			{"tab/shift+tab", "switch kind"}, {"esc", "back"},
		}
	default:
		return []helpEntry{{"esc", "back"}, {"ctrl+c", "quit"}}
	}
}

// metaLine renders one label/value row of renderArtifact's metadata block,
// the label muted and padded so values line up in a column.
func metaLine(label, value string) string {
	return metaLabelStyle.Render(fmt.Sprintf("%-13s", label)) + value + "\n"
}

func renderArtifact(a *artifact.Artifact) string {
	header := titleStyle.Render(fmt.Sprintf("%s (%s)", a.DisplayName(), a.Kind))

	var meta string
	meta += metaLine("Description:", a.Description)
	if a.Version != "" {
		meta += metaLine("Version:", a.Version)
	}
	if a.Namespace != "" {
		meta += metaLine("Namespace:", "@"+a.Namespace)
	}
	meta += metaLine("Path:", a.Dir)

	return header + "\n\n" + meta + "\n" + a.Body
}
