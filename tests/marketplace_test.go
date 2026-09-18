package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runAgentworks(t *testing.T, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"run", "../main.go"}, args...)
	cmd := exec.Command("go", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("agentworks %v failed: %v\nOutput: %s", args, err, out.String())
	}
	return out.String()
}

// setUpMarketplaceProject scaffolds a project with two unnamespaced
// artifacts and two "team-a"-namespaced artifacts. The mcp scaffold is a
// runnable server, so it's export-ready as generated.
func setUpMarketplaceProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runAgentworks(t, "init", dir)
	runAgentworks(t, "new", "skill", "csv-analyzer", "--description", "Analyze a CSV file and flag rows that stand out.", "--project", dir)
	runAgentworks(t, "new", "mcp", "jira-fetch", "--description", "Fetch a Jira ticket by key over the REST API.", "--project", dir)
	runAgentworks(t, "new", "skill", "team-a/reviewer", "--description", "Review a pull request diff before a human looks at it.", "--project", dir)
	runAgentworks(t, "new", "agent", "team-a/triager", "--description", "Triage an incoming support ticket and assign it a priority.", "--project", dir)

	return dir
}

type marketplaceDoc struct {
	Name    string `json:"name"`
	Schema  string `json:"$schema"`
	Plugins []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
	} `json:"plugins"`
}

// TestMarketplaceGroupsByNamespace checks the default behavior: unnamespaced
// artifacts bundle into one plugin named after the project, and each
// namespace becomes its own plugin -- for both claude-code and
// github-copilot, since neither --target nor --single was passed.
func TestMarketplaceGroupsByNamespace(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	runAgentworks(t, "marketplace", "--project", dir)

	claudePath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("reading %s: %v", claudePath, err)
	}
	var doc marketplaceDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", claudePath, err)
	}
	if doc.Schema != "" {
		t.Errorf("claude-code marketplace.json $schema = %q, want empty", doc.Schema)
	}
	if len(doc.Plugins) != 2 {
		t.Fatalf("claude-code plugins = %d, want 2 (project bundle + team-a)", len(doc.Plugins))
	}

	names := map[string]string{}
	for _, p := range doc.Plugins {
		names[p.Name] = p.Source
	}
	projectSrc, ok := names[filepath.Base(dir)]
	if !ok {
		t.Fatalf("no plugin named after the project in %+v", names)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(projectSrc[2:]), ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("project bundle plugin.json missing at referenced source %s: %v", projectSrc, err)
	}
	teamSrc, ok := names["team-a"]
	if !ok {
		t.Fatalf("no \"team-a\" plugin in %+v", names)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(teamSrc[2:]), ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("team-a bundle plugin.json missing at referenced source %s: %v", teamSrc, err)
	}

	ghPath := filepath.Join(dir, ".github", "plugin", "marketplace.json")
	ghData, err := os.ReadFile(ghPath)
	if err != nil {
		t.Fatalf("reading %s: %v", ghPath, err)
	}
	var ghDoc marketplaceDoc
	if err := json.Unmarshal(ghData, &ghDoc); err != nil {
		t.Fatalf("parsing %s: %v", ghPath, err)
	}
	if ghDoc.Schema == "" {
		t.Error("github-copilot marketplace.json $schema is empty, want the agent-plugins.org schema URL")
	}
	if len(ghDoc.Plugins) != 2 {
		t.Fatalf("github-copilot plugins = %d, want 2", len(ghDoc.Plugins))
	}
}

// TestMarketplaceSingleCollapsesNamespaces checks --single: every artifact,
// namespaced or not, lands in one plugin.
func TestMarketplaceSingleCollapsesNamespaces(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	runAgentworks(t, "marketplace", "--project", dir, "--single", "--target", "claude-code")

	claudePath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("reading %s: %v", claudePath, err)
	}
	var doc marketplaceDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", claudePath, err)
	}
	if len(doc.Plugins) != 1 {
		t.Fatalf("plugins = %d, want 1 with --single, got %+v", len(doc.Plugins), doc.Plugins)
	}

	ghPath := filepath.Join(dir, ".github", "plugin", "marketplace.json")
	if _, err := os.Stat(ghPath); !os.IsNotExist(err) {
		t.Errorf("github-copilot marketplace.json should not exist when --target claude-code was passed, stat err = %v", err)
	}
}

// TestMarketplaceRegenerationClearsStaleGroups checks that switching from
// namespace grouping to --single removes the now-orphaned per-namespace
// plugin directory from a previous run, rather than leaving stale output
// behind that the new marketplace.json no longer references.
func TestMarketplaceRegenerationClearsStaleGroups(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	runAgentworks(t, "marketplace", "--project", dir, "--target", "claude-code")

	teamADir := filepath.Join(dir, "plugins", "claude-code", "team-a")
	if _, err := os.Stat(teamADir); err != nil {
		t.Fatalf("expected %s to exist after the first run: %v", teamADir, err)
	}

	runAgentworks(t, "marketplace", "--project", dir, "--target", "claude-code", "--single")
	if _, err := os.Stat(teamADir); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after switching to --single, stat err = %v", teamADir, err)
	}
}

// TestMarketplaceUnknownTarget checks the --target flag is validated
// against the two vendors that actually share the marketplace.json
// convention, rather than accepting any registered export target.
func TestMarketplaceUnknownTarget(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	cmd := exec.Command("go", "run", "../main.go", "marketplace", "--project", dir, "--target", "chatgpt")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err == nil {
		t.Fatal("marketplace --target chatgpt expected a non-zero exit, got none")
	}
	if want := "unknown marketplace target"; !bytes.Contains(out.Bytes(), []byte(want)) {
		t.Errorf("error output should contain %q, got: %s", want, out.String())
	}
}

func TestMarketplaceCheckDetectsStaleAndMissing(t *testing.T) {
	dir := setUpMarketplaceProject(t)

	if _, _, exit := runJSON(t, "marketplace", "--check", "--project", dir); exit == 0 {
		t.Fatal("--check should fail before anything has been published")
	}

	runAgentworks(t, "marketplace", "--project", dir)
	if out, err := runAgentworksErr("marketplace", "--check", "--project", dir); err != nil {
		t.Fatalf("--check on a freshly published marketplace failed: %v\n%s", err, out)
	}

	// --check must not write anything: the committed tree is unchanged.
	if _, err := os.Stat(filepath.Join(dir, "agentworks.lock")); err == nil {
		before, _ := os.ReadFile(filepath.Join(dir, "agentworks.lock"))
		runAgentworks(t, "marketplace", "--check", "--project", dir)
		after, _ := os.ReadFile(filepath.Join(dir, "agentworks.lock"))
		if !bytes.Equal(before, after) {
			t.Error("--check modified agentworks.lock")
		}
	}

	skill := filepath.Join(dir, "skills", "csv-analyzer", "skill.md")
	f, err := os.OpenFile(skill, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nA source change that was never republished.\n")
	f.Close()

	out, err := runAgentworksErr("marketplace", "--check", "--project", dir)
	if err == nil {
		t.Fatalf("--check should fail once a source artifact changed without republishing:\n%s", out)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("--check output should say what is stale, got:\n%s", out)
	}

	runAgentworks(t, "marketplace", "--project", dir)
	if out, err := runAgentworksErr("marketplace", "--check", "--project", dir); err != nil {
		t.Fatalf("--check after republishing should pass: %v\n%s", err, out)
	}
}
