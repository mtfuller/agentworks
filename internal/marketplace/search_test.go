package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testMarketplaceJSON = `{
  "name": "test-marketplace",
  "plugins": [
    {"name": "pdf-tools", "description": "PDF manipulation toolkit", "source": "./plugins/pdf-tools"},
    {"name": "csv-analyzer", "description": "Analyze CSV files", "source": {"source": "github", "repo": "owner/csv-analyzer", "ref": "main"}}
  ]
}`

const testAgentSkillsJSON = `{"skills":[{"id":19,"slug":"pdf","name":"pdf","author":"anthropics","description":"PDF toolkit","category":"Productivity","views":178,"installs":64}],"total":1,"limit":50,"offset":0}`

// withMarketplaceTestServers points httpClient, agentSkillsBaseURL, and
// WellKnown's manifest URLs at local httptest servers instead of the real
// internet, restoring everything afterward.
func withMarketplaceTestServers(t *testing.T, marketplaceBody, agentSkillsBody string, marketplaceStatus, agentSkillsStatus int) (*httptest.Server, *httptest.Server) {
	t.Helper()
	mts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(marketplaceStatus)
		w.Write([]byte(marketplaceBody))
	}))
	ats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(agentSkillsStatus)
		w.Write([]byte(agentSkillsBody))
	}))

	origClient, origBase, origWellKnown, origRawBase := httpClient, agentSkillsBaseURL, WellKnown, rawGitHubBase
	httpClient = mts.Client()
	agentSkillsBaseURL = ats.URL
	rawGitHubBase = mts.URL
	WellKnown = []Marketplace{{ID: "test", Name: "Test Marketplace", Repo: "owner/repo", Ref: "main", Path: "marketplace.json"}}

	t.Cleanup(func() {
		httpClient = origClient
		agentSkillsBaseURL = origBase
		rawGitHubBase = origRawBase
		WellKnown = origWellKnown
		mts.Close()
		ats.Close()
	})
	return mts, ats
}

func TestSearchFiltersByNameAndDescription(t *testing.T) {
	withMarketplaceTestServers(t, testMarketplaceJSON, testAgentSkillsJSON, http.StatusOK, http.StatusOK)

	results, errs := Search(context.Background(), "pdf")
	if len(errs) != 0 {
		t.Fatalf("Search() errs = %v, want none", errs)
	}
	var names []string
	for _, r := range results {
		names = append(names, r.Name)
	}
	if len(results) != 2 { // pdf-tools from the marketplace + pdf from agentskills.codes
		t.Fatalf("Search(pdf) results = %v, want 2 matches", names)
	}

	noMatch, errs := Search(context.Background(), "nonexistent-query-xyz")
	if len(errs) != 0 {
		t.Fatalf("Search() errs = %v, want none", errs)
	}
	// agentskills.codes' server-side search always returns whatever the
	// (test) server sends regardless of query, but the marketplace's own
	// entries are client-side filtered and should find nothing.
	for _, r := range noMatch {
		if r.Origin == "Test Marketplace" {
			t.Errorf("Search(nonexistent) matched a marketplace entry: %+v", r)
		}
	}
}

func TestSearchSetsOrigin(t *testing.T) {
	withMarketplaceTestServers(t, testMarketplaceJSON, testAgentSkillsJSON, http.StatusOK, http.StatusOK)

	results, _ := Search(context.Background(), "")
	origins := map[string]bool{}
	for _, r := range results {
		origins[r.Origin] = true
	}
	if !origins["Test Marketplace"] {
		t.Error("expected a result with Origin = Test Marketplace")
	}
	if !origins["agentskills.codes"] {
		t.Error("expected a result with Origin = agentskills.codes")
	}
}

func TestSearchReturnsPartialResultsWhenASourceFails(t *testing.T) {
	withMarketplaceTestServers(t, testMarketplaceJSON, "", http.StatusOK, http.StatusInternalServerError)

	results, errs := Search(context.Background(), "")
	if len(errs) != 1 {
		t.Fatalf("Search() errs = %v, want exactly 1 (agentskills.codes down)", errs)
	}
	if !strings.Contains(errs[0].Error(), "agentskills.codes") {
		t.Errorf("errs[0] = %v, want it to name agentskills.codes", errs[0])
	}
	found := false
	for _, r := range results {
		if r.Origin == "Test Marketplace" {
			found = true
		}
	}
	if !found {
		t.Error("Search() dropped the working marketplace's results because a different source failed")
	}
}

func TestSearchSkipsUnresolvableEntries(t *testing.T) {
	body := `{"name":"x","plugins":[
		{"name":"good","description":"a fine plugin","source":"./plugins/good"},
		{"name":"bad","description":"uses npm","source":{"source":"npm","package":"@acme/x"}}
	]}`
	withMarketplaceTestServers(t, body, testAgentSkillsJSON, http.StatusOK, http.StatusOK)

	results, errs := Search(context.Background(), "")
	if len(errs) != 0 {
		t.Fatalf("Search() errs = %v, want none (an unresolvable entry is skipped, not an error)", errs)
	}
	for _, r := range results {
		if r.Name == "bad" {
			t.Error("Search() included an entry with an unsupported source type")
		}
	}
}
