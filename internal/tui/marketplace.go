package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mtfuller/agentworks/internal/artifact"
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
	if i.r.License != "" {
		return fmt.Sprintf("%s · %s · %s", i.r.License, i.r.Origin, i.r.Description)
	}
	return fmt.Sprintf("%s -- %s", i.r.Origin, i.r.Description)
}
func (i marketplaceItem) FilterValue() string {
	return i.r.Name + " " + i.r.Description + " " + i.r.Origin + " " + i.r.Category + " " + strings.Join(i.r.Keywords, " ")
}

// key identifies the item for the preview cache: two results are the same
// plugin exactly when they import from the same place.
func (i marketplaceItem) key() string { return i.r.Source.String() }

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

// startMarketplace opens the plugin browser and kicks off an async fetch --
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
		// Only plugins with a verifiably permissive license are offered:
		// this pane is for pulling code into your own project.
		results, errs := marketplace.Search(context.Background(), query, marketplace.Options{CommercialOnly: true})
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
	m.mpShown = "" // force the detail pane to pick up the new first item
	cmd := m.syncMarketplaceDetail()
	return m, cmd
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
	if len(msg.plan.Warnings) > 0 {
		level = statusWarn
		text += fmt.Sprintf("; %s", strings.Join(msg.plan.Warnings, "; "))
	}
	if len(msg.securityWarnings) > 0 {
		level = statusWarn
		text += fmt.Sprintf("; ⚠ %s", strings.Join(msg.securityWarnings, "; "))
	}
	m.setStatus(level, "%s", text)
	return m.refreshAfterCreate(kind)
}

// previewState is the cached "what's included" lookup for one plugin.
type previewState struct {
	pending bool // waiting out the debounce, or fetching
	loaded  bool
	preview *marketplace.Preview
	err     error
}

// previewDebounce is how long a plugin must stay highlighted before its
// contents are fetched -- scrolling through the list shouldn't download a
// repository per row it passes over.
const previewDebounce = 400 * time.Millisecond

type previewTickMsg struct{ key string }

type previewLoadedMsg struct {
	key     string
	preview *marketplace.Preview
	err     error
}

// marketplaceWidths splits the terminal between the list (left) and the
// detail pane (right).
func marketplaceWidths(total int) (listW, detailW int) {
	listW = total * 2 / 5
	if listW < 32 {
		listW = min(32, total)
	}
	return listW, max(total-listW, 0)
}

// syncMarketplaceDetail keeps the detail pane on the highlighted plugin,
// and schedules a debounced fetch of what it contains the first time it's
// highlighted. Call it after anything that can change the selection.
func (m *Model) syncMarketplaceDetail() tea.Cmd {
	item, ok := m.marketplaceList.SelectedItem().(marketplaceItem)
	if !ok {
		m.mpShown = ""
		m.refreshMarketplaceDetail()
		return nil
	}
	if key := item.key(); key != m.mpShown {
		m.mpShown = key
		m.marketplaceDetail.GotoTop()
	}

	var cmd tea.Cmd
	if _, cached := m.mpPreviews[m.mpShown]; !cached {
		m.mpPreviews[m.mpShown] = &previewState{pending: true}
		key := m.mpShown
		cmd = tea.Tick(previewDebounce, func(time.Time) tea.Msg { return previewTickMsg{key: key} })
	}
	m.refreshMarketplaceDetail()
	return cmd
}

// handlePreviewTick starts the fetch once the debounce elapses, unless the
// user has already moved on to a different plugin.
func (m Model) handlePreviewTick(msg previewTickMsg) (tea.Model, tea.Cmd) {
	if msg.key != m.mpShown {
		delete(m.mpPreviews, msg.key) // never fetched; let a later visit retry
		return m, nil
	}
	item, ok := m.marketplaceList.SelectedItem().(marketplaceItem)
	if !ok || item.key() != msg.key {
		return m, nil
	}
	root, src := m.root, item.r.Source
	return m, func() tea.Msg {
		p, err := marketplace.FetchPreview(context.Background(), root, src)
		return previewLoadedMsg{key: msg.key, preview: p, err: err}
	}
}

func (m Model) handlePreviewLoaded(msg previewLoadedMsg) (tea.Model, tea.Cmd) {
	m.mpPreviews[msg.key] = &previewState{loaded: true, preview: msg.preview, err: msg.err}
	if msg.key == m.mpShown {
		m.refreshMarketplaceDetail()
	}
	return m, nil
}

func (m *Model) refreshMarketplaceDetail() {
	item, ok := m.marketplaceList.SelectedItem().(marketplaceItem)
	if !ok {
		m.marketplaceDetail.SetContent(metaLabelStyle.Render("Nothing selected."))
		return
	}
	m.marketplaceDetail.SetContent(renderMarketplaceDetail(item.r, m.mpPreviews[item.key()], m.marketplaceDetail.Width-4))
}

// marketplaceView lays the list and its detail pane side by side.
func (m Model) marketplaceView() string {
	detail := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(mutedColor).
		PaddingLeft(1).
		Render(m.marketplaceDetail.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, m.marketplaceList.View(), detail)
}

// renderMarketplaceDetail is the right-hand pane: the plugin's metadata,
// where it would land, and -- once fetched -- exactly which artifacts it
// would add.
func renderMarketplaceDetail(r marketplace.Result, st *previewState, width int) string {
	if width < 20 {
		width = 20
	}
	wrap := lipgloss.NewStyle().Width(width)

	var b strings.Builder
	b.WriteString(titleStyle.Padding(0).Render(r.Name) + "\n\n")
	if r.Description != "" {
		b.WriteString(wrap.Render(r.Description) + "\n\n")
	}

	row := func(label, value string) {
		if value != "" {
			b.WriteString(wrap.Render(metaLabelStyle.Render(fmt.Sprintf("%-12s", label))+value) + "\n")
		}
	}
	row("License:", r.License)
	row("Origin:", r.Origin)
	row("Author:", r.Author)
	row("Category:", r.Category)
	row("Version:", r.Version)
	row("Homepage:", r.Homepage)
	row("Source:", r.Source.String())
	row("Keywords:", strings.Join(r.Keywords, ", "))
	row("Imports as:", "@"+r.Source.DefaultNamespace()+"/<name>")

	b.WriteString("\n" + titleStyle.Padding(0).Render("Includes") + "\n")
	switch {
	case st == nil || st.pending:
		b.WriteString(metaLabelStyle.Render("loading...") + "\n")
	case st.err != nil:
		b.WriteString(wrap.Render(statusWarnStyle.Render("couldn't inspect: "+st.err.Error())) + "\n")
	default:
		b.WriteString(renderPreview(st.preview, wrap))
	}
	return b.String()
}

func renderPreview(p *marketplace.Preview, wrap lipgloss.Style) string {
	var b strings.Builder
	for _, k := range artifact.Kinds() {
		var items []marketplace.PreviewItem
		for _, it := range p.Items {
			if it.Kind == k {
				items = append(items, it)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", metaLabelStyle.Render(fmt.Sprintf("%s (%d)", k.DirName(), len(items))))
		for _, it := range items {
			line := "  @" + it.Namespace + "/" + it.Name
			if it.Description != "" {
				line += " -- " + firstSentence(it.Description)
			}
			b.WriteString(wrap.Render(line) + "\n")
		}
	}
	if len(p.Items) == 0 {
		b.WriteString(metaLabelStyle.Render("no importable skills or agents") + "\n")
	}
	for _, u := range p.Unsupported {
		b.WriteString("\n" + wrap.Render(statusWarnStyle.Render("not imported: ")+u) + "\n")
	}
	for _, w := range p.Warnings {
		b.WriteString("\n" + wrap.Render(statusWarnStyle.Render("note: ")+w) + "\n")
	}
	return b.String()
}

// firstSentence trims a description to its first sentence, capped, so a
// plugin with dozens of skills stays scannable.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if r := []rune(s); len(r) > 110 {
		s = string(r[:109]) + "…"
	}
	return s
}
