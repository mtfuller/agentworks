package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/marketplace"
)

// marketplaceItem adapts a marketplace.Result to bubbles/list's list.Item,
// the same shape as kindItem/artifactItem.
type marketplaceItem struct {
	r marketplace.Result
}

func (i marketplaceItem) Title() string { return i.r.Name }
func (i marketplaceItem) Description() string {
	return fmt.Sprintf("%s -- %s", i.r.Origin, i.r.Description)
}
func (i marketplaceItem) FilterValue() string {
	return i.r.Name + " " + i.r.Description + " " + i.r.Origin
}

// marketplaceItems converts search results into list.Items -- kept as its
// own function so the conversion is testable without a running program.
func marketplaceItems(results []marketplace.Result) []list.Item {
	items := make([]list.Item, len(results))
	for i, r := range results {
		items[i] = marketplaceItem{r: r}
	}
	return items
}

// marketplaceResultsMsg delivers a completed marketplace.Search.
type marketplaceResultsMsg struct {
	results []marketplace.Result
	errs    []error
}

// marketplaceImportedMsg delivers a completed import triggered from the
// search pane.
type marketplaceImportedMsg struct {
	plan *importer.Plan
	err  error
}

// startMarketplace opens the search pane and kicks off an async fetch --
// network I/O must never block Update, so the actual marketplace.Search
// call happens inside the returned tea.Cmd, on Bubble Tea's own goroutine,
// not here.
func (m Model) startMarketplace() (tea.Model, tea.Cmd) {
	m.statusMsg = ""
	m.pane = paneMarketplace
	spinnerCmd := m.marketplaceList.StartSpinner()
	return m, tea.Batch(spinnerCmd, searchMarketplaceCmd(""))
}

func searchMarketplaceCmd(query string) tea.Cmd {
	return func() tea.Msg {
		results, errs := marketplace.Search(context.Background(), query)
		return marketplaceResultsMsg{results: results, errs: errs}
	}
}

// handleMarketplaceResults stops the loading spinner and loads whatever
// came back into the list -- relying on list.Model's own built-in "/"
// filter to narrow it, the same convention kindList/artifactList already
// use, rather than a live requery per keystroke.
func (m Model) handleMarketplaceResults(msg marketplaceResultsMsg) (tea.Model, tea.Cmd) {
	m.marketplaceList.StopSpinner()
	m.marketplaceList.SetItems(marketplaceItems(msg.results))
	if len(msg.errs) > 0 {
		m.statusMsg = fmt.Sprintf("%d source(s) unavailable (%v) -- showing what did come back", len(msg.errs), msg.errs[0])
	}
	return m, nil
}

// importSelected runs Prepare+Apply for the highlighted result as an async
// tea.Cmd, the same "network I/O never blocks Update" reasoning as
// startMarketplace.
func (m Model) importSelected() (tea.Model, tea.Cmd) {
	item, ok := m.marketplaceList.SelectedItem().(marketplaceItem)
	if !ok {
		return m, nil
	}
	root, src := m.root, item.r.Source
	m.statusMsg = fmt.Sprintf("Importing %s...", item.r.Name)

	return m, func() tea.Msg {
		plan, err := importer.Prepare(context.Background(), root, src, importer.Options{})
		if err != nil {
			return marketplaceImportedMsg{err: err}
		}
		defer plan.Close()
		if err := plan.Apply(); err != nil {
			return marketplaceImportedMsg{err: err}
		}
		return marketplaceImportedMsg{plan: plan}
	}
}

// handleMarketplaceImported reports the outcome and, on success, drills
// straight into the newly-imported artifacts' kind -- mirroring
// commitCreate's refreshAfterCreate so "what did I just get" is answered
// immediately instead of leaving the user in a stale marketplace list.
func (m Model) handleMarketplaceImported(msg marketplaceImportedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("import failed: %v", msg.err)
		return m, nil
	}
	if len(msg.plan.Artifacts) == 0 {
		m.statusMsg = "nothing importable was found there"
		return m, nil
	}

	kind := msg.plan.Artifacts[0].Kind
	m.statusMsg = fmt.Sprintf("Imported %d artifact(s)", len(msg.plan.Artifacts))
	for _, u := range msg.plan.Unsupported {
		m.statusMsg += "; " + u
	}
	return m.refreshAfterCreate(kind)
}
