package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/lockfile"
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
	// securityWarnings are LintSecurity hits on the imported artifacts
	// (see internal/artifact.LintSecurity) -- surfaced after the fact
	// rather than as a blocking confirmation dialog, unlike `agentworks
	// add`'s CLI gate: this bubbletea flow has no synchronous prompt to
	// hook one into, and the marketplace is a curated, lighter-weight
	// entry point already.
	securityWarnings []string
}

// startMarketplace opens the search pane and kicks off an async fetch --
// network I/O must never block Update, so the actual marketplace.Search
// call happens inside the returned tea.Cmd, on Bubble Tea's own goroutine,
// not here.
func (m Model) startMarketplace() (tea.Model, tea.Cmd) {
	m.clearStatus()
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
		m.setStatus(statusWarn, "%d source(s) unavailable (%v) -- showing what did come back", len(msg.errs), msg.errs[0])
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
	m.setStatus(statusInfo, "Importing %s...", item.r.Name)

	return m, func() tea.Msg {
		plan, err := importer.Prepare(context.Background(), root, src, importer.Options{})
		if err != nil {
			return marketplaceImportedMsg{err: err}
		}
		defer plan.Close()
		if err := plan.Apply(); err != nil {
			return marketplaceImportedMsg{err: err}
		}

		var warnings []string
		for _, a := range plan.Artifacts {
			for _, w := range a.LintSecurity() {
				warnings = append(warnings, w.Message)
			}
		}

		if lf, err := lockfile.Load(root); err == nil {
			if err := plan.RecordLockEntries(root, lf); err == nil {
				_ = lf.Save(root)
			}
		}

		return marketplaceImportedMsg{plan: plan, securityWarnings: warnings}
	}
}

// handleMarketplaceImported reports the outcome and, on success, drills
// straight into the newly-imported artifacts' kind -- mirroring
// commitCreate's refreshAfterCreate so "what did I just get" is answered
// immediately instead of leaving the user in a stale marketplace list.
func (m Model) handleMarketplaceImported(msg marketplaceImportedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(statusError, "import failed: %v", msg.err)
		return m, nil
	}
	if len(msg.plan.Artifacts) == 0 {
		m.setStatus(statusWarn, "nothing importable was found there")
		return m, nil
	}

	kind := msg.plan.Artifacts[0].Kind
	level := statusSuccess
	text := fmt.Sprintf("Imported %d artifact(s)", len(msg.plan.Artifacts))
	for _, u := range msg.plan.Unsupported {
		text += "; " + u
	}
	if len(msg.securityWarnings) > 0 {
		level = statusWarn
		text += fmt.Sprintf("; ⚠ %s", strings.Join(msg.securityWarnings, "; "))
	}
	m.setStatus(level, "%s", text)
	return m.refreshAfterCreate(kind)
}
