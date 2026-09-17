package tui

import (
	"strings"
	"testing"

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
	if item.Description() != "agentskills.codes -- PDF toolkit" {
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
