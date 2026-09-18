package tui

import (
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/importer"
	"github.com/mtfuller/agentworks/internal/marketplace"
)

func TestMarketplaceItems(t *testing.T) {
	results := []marketplace.Result{
		{Origin: "agentskills.codes", Name: "pdf", Description: "PDF toolkit", Source: importer.Source{Kind: importer.SourceArchive, URL: "https://agentskills.codes/api/skills/download/19"}},
		{Origin: "Claude Code (official)", Name: "csv-analyzer", Description: "Analyze CSVs", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "owner/repo"}},
	}

	items := marketplaceItems(results)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}

	item, ok := items[0].(marketplaceItem)
	if !ok {
		t.Fatalf("items[0] type = %T, want marketplaceItem", items[0])
	}
	if item.Title() != "pdf" {
		t.Errorf("Title() = %q, want pdf", item.Title())
	}
	if item.Description() != "agentskills.codes -- PDF toolkit" { // no license known -> no license segment
		t.Errorf("Description() = %q", item.Description())
	}
}

func TestMarketplaceItemFilterValueIncludesOrigin(t *testing.T) {
	item := marketplaceItem{r: marketplace.Result{Origin: "GitHub Copilot (awesome-copilot)", Name: "linter", Description: "Lints code"}}
	fv := item.FilterValue()
	for _, want := range []string{"linter", "Lints code", "GitHub Copilot (awesome-copilot)"} {
		if !strings.Contains(fv, want) {
			t.Errorf("FilterValue() = %q, want it to contain %q", fv, want)
		}
	}
}

func TestMarketplaceItemDescriptionShowsLicense(t *testing.T) {
	item := marketplaceItem{r: marketplace.Result{Origin: "Claude Code (official)", Description: "Lints code", License: "MIT"}}
	if got, want := item.Description(), "MIT · Claude Code (official) · Lints code"; got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

func TestRenderMarketplaceDetail(t *testing.T) {
	r := marketplace.Result{
		Name: "superpowers", Description: "Skills for coding agents", License: "MIT", Author: "obra",
		Source: importer.Source{Kind: importer.SourceGitHub, Repo: "obra/superpowers"},
	}

	loading := renderMarketplaceDetail(r, &previewState{pending: true}, 60)
	for _, want := range []string{"superpowers", "MIT", "obra", "@obra/<name>", "loading"} {
		if !strings.Contains(loading, want) {
			t.Errorf("loading detail missing %q:\n%s", want, loading)
		}
	}

	loaded := renderMarketplaceDetail(r, &previewState{loaded: true, preview: &marketplace.Preview{
		Items: []marketplace.PreviewItem{
			{Kind: artifact.KindSkill, Namespace: "obra", Name: "brainstorming", Description: "Explore ideas first. Then plan."},
			{Kind: artifact.KindAgent, Namespace: "obra", Name: "reviewer"},
		},
		Unsupported: []string{"hooks/hooks.json (hook import not supported yet)"},
	}}, 60)
	for _, want := range []string{"skills (1)", "@obra/brainstorming", "Explore ideas first.", "agents (1)", "@obra/reviewer", "hooks/hooks.json"} {
		if !strings.Contains(loaded, want) {
			t.Errorf("loaded detail missing %q:\n%s", want, loaded)
		}
	}
	if strings.Contains(loaded, "Then plan") {
		t.Error("description should be trimmed to its first sentence")
	}
}

func TestSyncMarketplaceDetailSchedulesOneDebouncedFetch(t *testing.T) {
	m := newTestModel(t)
	m.marketplaceList.SetItems(marketplaceItems([]marketplace.Result{
		{Name: "a", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "o/a"}},
		{Name: "b", Source: importer.Source{Kind: importer.SourceGitHub, Repo: "o/b"}},
	}))

	if cmd := m.syncMarketplaceDetail(); cmd == nil {
		t.Fatal("first highlight should schedule a preview fetch")
	}
	if cmd := m.syncMarketplaceDetail(); cmd != nil {
		t.Error("re-syncing the same selection must not schedule a second fetch")
	}

	// A tick for a plugin the user already scrolled past is dropped, not fetched.
	stale := previewTickMsg{key: "github.com/o/b@default branch"}
	m.mpPreviews[stale.key] = &previewState{pending: true}
	if _, cmd := m.handlePreviewTick(stale); cmd != nil {
		t.Error("a tick for a non-highlighted plugin should not start a fetch")
	}
	if _, ok := m.mpPreviews[stale.key]; ok {
		t.Error("the abandoned pending state should be cleared so a later visit retries")
	}
}
