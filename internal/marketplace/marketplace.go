// Package marketplace searches for skills and plugins across agentskills.codes
// (a real public JSON API) and the well-known Claude Code and GitHub Copilot
// plugin marketplaces (a static marketplace.json file in a git repo -- no
// API, no auth). Every result resolves to an internal/importer.Source, so
// picking one hands off to the exact same Prepare/Apply path `agentworks
// add <url>` uses.
package marketplace

import "fmt"

// Marketplace is a known plugin marketplace: a git repo whose root
// publishes a marketplace.json manifest at Path.
type Marketplace struct {
	ID   string
	Name string
	Repo string // "owner/repo"
	Ref  string // branch to fetch the manifest and any relative-path plugin from
	Path string // path to marketplace.json within the repo -- this genuinely
	// varies (.claude-plugin/marketplace.json vs .github/plugin/marketplace.json),
	// so it can't be a shared constant.
}

// rawGitHubBase is a var, not a const, so tests can redirect it to an
// httptest.Server instead of the real raw.githubusercontent.com.
var rawGitHubBase = "https://raw.githubusercontent.com"

// ManifestURL is where Marketplace's marketplace.json is fetched from --
// a plain static file over HTTPS, same as any other file in the repo.
func (m Marketplace) ManifestURL() string {
	return fmt.Sprintf("%s/%s/%s/%s", rawGitHubBase, m.Repo, m.Ref, m.Path)
}

// WellKnown is the hardcoded set of marketplaces AgentWorks searches,
// mirroring the same "static list of known things" shape as
// internal/targets/registry.go's own `var registry = []Target{...}`.
var WellKnown = []Marketplace{
	{
		ID:   "claude-plugins-official",
		Name: "Claude Code (official)",
		Repo: "anthropics/claude-plugins-official",
		Ref:  "main",
		Path: ".claude-plugin/marketplace.json",
	},
	{
		ID:   "awesome-copilot",
		Name: "GitHub Copilot (awesome-copilot)",
		Repo: "github/awesome-copilot",
		Ref:  "main",
		Path: ".github/plugin/marketplace.json",
	},
}
