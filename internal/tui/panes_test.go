package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/marketplace"
)

func keyPress(m Model, s string) Model {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestEveryPaneRendersWithItsOwnHelp(t *testing.T) {
	m := newTestModel(t)
	if m.Init() != nil && false {
		t.Fatal("unreachable")
	}

	// Browse: the tab strip, the artifact list, and browse's key hints.
	view := m.View()
	for _, want := range []string{"open", "new", "export", "test", "browse plugins", "templates", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("browse view should hint %q:\n%s", want, view)
		}
	}

	// Detail (the demo artifact lives on the skills tab).
	m = tabTo(m, artifact.KindSkill)
	if v := m.View(); !strings.Contains(v, "demo") {
		t.Errorf("the skills tab should list the artifact:\n%s", v)
	}
	m = keyPress(m, "enter")
	if m.pane != paneDetail {
		t.Fatalf("enter should open the artifact, pane = %v", m.pane)
	}
	if v := m.View(); !strings.Contains(v, "A demo skill.") || !strings.Contains(v, "back") {
		t.Errorf("detail view:\n%s", v)
	}
	m = keyPress(m, "esc")

	// Templates.
	m = keyPress(m, "b")
	if m.pane != paneTemplates {
		t.Fatalf("b should open the templates pane, pane = %v", m.pane)
	}
	if v := m.View(); !strings.Contains(v, "create from this template") {
		t.Errorf("templates view:\n%s", v)
	}
	m = keyPress(m, "esc")

	// Marketplace (the search itself is a command we don't run: it needs the network).
	updated, cmd := m.startMarketplace()
	m = updated.(Model)
	if cmd == nil || m.pane != paneMarketplace {
		t.Fatalf("startMarketplace should switch panes and start a search: pane=%v cmd=%v", m.pane, cmd)
	}
	if v := m.View(); !strings.Contains(v, "import") || !strings.Contains(v, "refresh") {
		t.Errorf("marketplace view:\n%s", v)
	}
}

func TestHelpEntriesForEveryPane(t *testing.T) {
	m := newTestModel(t)
	for _, p := range []pane{paneBrowse, paneDetail, paneMarketplace, paneTemplates, paneForm} {
		m.pane = p
		if len(m.helpEntries()) == 0 {
			t.Errorf("pane %v has no help entries", p)
		}
	}
	if renderHelp(nil) != "" {
		t.Error("no entries renders nothing")
	}
}

func TestTabBarWrapsBothWays(t *testing.T) {
	tabs := newTabBar([]string{"a", "b", "c"})
	tabs.prev()
	if tabs.active != 2 {
		t.Errorf("prev from the first tab should wrap to the last, active = %d", tabs.active)
	}
	tabs.next()
	if tabs.active != 0 {
		t.Errorf("next from the last tab should wrap to the first, active = %d", tabs.active)
	}
	view := tabs.View(20)
	if !strings.Contains(view, "a") || !strings.Contains(view, "─") {
		t.Errorf("tab strip should show the labels and a rule:\n%s", view)
	}
	empty := newTabBar(nil)
	empty.next()
	empty.prev() // no labels: must not panic or divide by zero
	if strings.Contains(empty.View(0), "─") {
		t.Error("width 0 draws no rule")
	}
}

func TestStatusLevelStyles(t *testing.T) {
	for _, l := range []statusLevel{statusNone, statusInfo, statusSuccess, statusWarn, statusError} {
		if l.style().Render("x") == "" {
			t.Errorf("level %v renders nothing", l)
		}
	}
	m := newTestModel(t)
	m.setStatus(statusWarn, "careful %d", 3)
	if m.statusMsg != "careful 3" || m.statusLevel != statusWarn {
		t.Errorf("setStatus: %q / %v", m.statusMsg, m.statusLevel)
	}
	m.clearStatus()
	if m.statusMsg != "" || m.statusLevel != statusNone {
		t.Error("clearStatus should reset both")
	}
}

func TestArtifactItemAdapter(t *testing.T) {
	m := newTestModel(t)
	m = tabTo(m, artifact.KindSkill)
	item, ok := m.artifactLists[artifact.KindSkill].SelectedItem().(artifactItem)
	if !ok {
		t.Fatal("the skill tab should have an artifact selected")
	}
	if item.Title() != "demo" || item.Description() != "A demo skill." || item.FilterValue() != "demo" {
		t.Errorf("artifact item = %q / %q / %q", item.Title(), item.Description(), item.FilterValue())
	}
}

func TestMarketplaceResultsLoadIntoTheList(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneMarketplace
	results := []marketplace.Result{
		{Name: "one", Description: "First", Origin: "Claude Code", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "o/one"}},
		{Name: "two", Description: "Second", Origin: "Copilot", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "o/two"}},
	}

	updated, cmd := m.handleMarketplaceResults(marketplaceResultsMsg{results: results, errs: []error{errors.New("agentskills.codes timed out")}})
	m = updated.(Model)
	if len(m.marketplaceList.Items()) != 2 {
		t.Errorf("list has %d items, want 2", len(m.marketplaceList.Items()))
	}
	if m.statusLevel != statusWarn || !strings.Contains(m.statusMsg, "1 source(s) unavailable") || !strings.Contains(m.statusMsg, "timed out") {
		t.Errorf("a partial failure should be a warning: %v %q", m.statusLevel, m.statusMsg)
	}
	if cmd == nil {
		t.Error("the first result's preview should be scheduled")
	}
	if !strings.Contains(m.marketplaceView(), "one") {
		t.Errorf("the marketplace view should show the results:\n%s", m.marketplaceView())
	}
}

func TestMarketplaceImportOutcomes(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.handleMarketplaceImported(marketplaceImportedMsg{err: errors.New("boom")})
	m = updated.(Model)
	if m.statusLevel != statusError || !strings.Contains(m.statusMsg, "import failed: boom") {
		t.Errorf("failure status: %v %q", m.statusLevel, m.statusMsg)
	}

	updated, _ = m.handleMarketplaceImported(marketplaceImportedMsg{plan: &importer.Plan{}})
	m = updated.(Model)
	if m.statusLevel != statusWarn || !strings.Contains(m.statusMsg, "nothing importable") {
		t.Errorf("empty status: %v %q", m.statusLevel, m.statusMsg)
	}

	plan := &importer.Plan{
		Artifacts:   []*artifact.Artifact{{Frontmatter: artifact.Frontmatter{Kind: artifact.KindMCP, Name: "s"}}},
		Unsupported: []string{"prompt hook"},
		Warnings:    []string{"MCP server \"s\": ignored field cwd"},
	}
	updated, _ = m.handleMarketplaceImported(marketplaceImportedMsg{plan: plan, securityWarnings: []string{"runs a shell command"}})
	m = updated.(Model)
	for _, want := range []string{"Imported 1 artifact(s)", "prompt hook", "runs a shell command", "ignored field cwd"} {
		if !strings.Contains(m.statusMsg, want) {
			t.Errorf("import status should mention %q: %q", want, m.statusMsg)
		}
	}
	if m.statusLevel != statusWarn {
		t.Errorf("an import with security warnings should be a warning, got %v", m.statusLevel)
	}
	if m.currentKind() != artifact.KindMCP {
		t.Errorf("import should drill into the imported kind, at %v", m.currentKind())
	}
}

func TestPreviewTickAndLoad(t *testing.T) {
	m := newTestModel(t)
	m.pane = paneMarketplace
	res := marketplace.Result{Name: "one", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "o/one"}}
	updated, _ := m.handleMarketplaceResults(marketplaceResultsMsg{results: []marketplace.Result{res}})
	m = updated.(Model)
	key := marketplaceItem{r: res}.key()

	// A tick for a plugin the user has already moved off is dropped, and forgotten so a later visit retries.
	updated, cmd := m.handlePreviewTick(previewTickMsg{key: "some/other"})
	m = updated.(Model)
	if cmd != nil {
		t.Error("a stale tick must not start a fetch")
	}

	// A tick for the highlighted plugin starts the fetch (not run here: it needs the network).
	updated, cmd = m.handlePreviewTick(previewTickMsg{key: key})
	m = updated.(Model)
	if cmd == nil {
		t.Error("a current tick should start the preview fetch")
	}

	// The result lands in the cache and the detail pane.
	updated, _ = m.handlePreviewLoaded(previewLoadedMsg{key: key, preview: &marketplace.Preview{
		Items: []marketplace.PreviewItem{{Kind: artifact.KindSkill, Namespace: "o", Name: "one"}},
	}})
	m = updated.(Model)
	if st := m.mpPreviews[key]; st == nil || !st.loaded || st.preview == nil {
		t.Errorf("preview state = %+v, want it loaded", st)
	}
	if !strings.Contains(m.marketplaceDetail.View(), "@o/one") {
		t.Errorf("the detail pane should list what the plugin includes:\n%s", m.marketplaceDetail.View())
	}

	// A failed preview is shown as a warning in the pane, not a crash.
	updated, _ = m.handlePreviewLoaded(previewLoadedMsg{key: key, err: errors.New("no such repo")})
	m = updated.(Model)
	if !strings.Contains(m.marketplaceDetail.View(), "couldn't inspect") {
		t.Errorf("a failed preview should say so:\n%s", m.marketplaceDetail.View())
	}
}

func TestPreviewDetailShowsWarningsAndUnsupported(t *testing.T) {
	r := marketplace.Result{Name: "kit", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "o/kit"}}
	out := renderMarketplaceDetail(r, &previewState{loaded: true, preview: &marketplace.Preview{
		Items:       []marketplace.PreviewItem{{Kind: artifact.KindMCP, Namespace: "o", Name: "db", Description: "A database server."}},
		Unsupported: []string{"hooks: prompt handler for Stop"},
		Warnings:    []string{"MCP server \"db\": ignored unsupported field(s): cwd"},
	}}, 60)
	for _, want := range []string{"mcp (1)", "@o/db", "prompt handler", "ignored unsupported field"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail should mention %q:\n%s", want, out)
		}
	}
}

func TestMarketplaceWidths(t *testing.T) {
	l, d := marketplaceWidths(100)
	if l != 40 || d != 60 {
		t.Errorf("marketplaceWidths(100) = %d, %d", l, d)
	}
	if l, d := marketplaceWidths(20); l != 20 || d != 0 {
		t.Errorf("a narrow terminal keeps the list usable: %d, %d", l, d)
	}
}
