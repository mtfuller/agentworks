package connectors

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mtfuller/agentworks/internal/spec"
)

// TestLiveConnectorAuthentication is intentionally opt-in and excluded from
// ordinary CI. It performs readiness requests only; it does not create or edit
// Jira or GitHub data.
func TestLiveConnectorAuthentication(t *testing.T) {
	if os.Getenv("AGENTWORKS_CONNECTOR_SMOKE") != "1" {
		t.Skip("set AGENTWORKS_CONNECTOR_SMOKE=1 for live connector checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, email, jiraToken := os.Getenv("JIRA_BASE_URL"), os.Getenv("JIRA_EMAIL"), os.Getenv("JIRA_API_TOKEN")
	repository, githubToken := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_TOKEN")
	if base == "" || email == "" || jiraToken == "" || repository == "" || githubToken == "" {
		t.Fatal("live connector conformance requires JIRA_BASE_URL, JIRA_EMAIL, JIRA_API_TOKEN, GITHUB_REPOSITORY, and GITHUB_TOKEN")
	}
	{
		connector, err := NewJira(spec.Source{Name: "live-jira", Kind: spec.SourceJira, Jira: spec.JiraSource{BaseURL: base}}, email, jiraToken, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.Test(ctx); err != nil {
			t.Fatalf("Jira: %v", err)
		}
	}
	{
		connector, err := NewGitHub(spec.Source{Name: "live-github", Kind: spec.SourceGitHub, GitHub: spec.GitHubSource{Repository: repository}}, githubToken, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.Test(ctx); err != nil {
			t.Fatalf("GitHub: %v", err)
		}
	}
}
