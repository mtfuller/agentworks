package mcpconfig

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestServerForBuildsStdioEntry(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindTool,
			Name: "jira-fetch",
			Extra: map[string]any{
				"command": "python3 src/main.py",
				"auth":    []any{"JIRA_BASE_URL", "JIRA_API_TOKEN"},
			},
		},
	}

	s, err := ServerFor(a)
	if err != nil {
		t.Fatalf("ServerFor() error = %v", err)
	}
	if s.Type != "stdio" {
		t.Errorf("Type = %q, want stdio", s.Type)
	}
	if s.Command != "sh" {
		t.Errorf("Command = %q, want sh", s.Command)
	}
	if len(s.Args) != 2 || s.Args[0] != "-c" || s.Args[1] != "python3 src/main.py" {
		t.Errorf("Args = %v, want [-c \"python3 src/main.py\"]", s.Args)
	}
	if s.Env["JIRA_BASE_URL"] != "${JIRA_BASE_URL}" {
		t.Errorf("Env[JIRA_BASE_URL] = %q, want ${JIRA_BASE_URL} (a reference, not a literal secret)", s.Env["JIRA_BASE_URL"])
	}
	if s.Env["JIRA_API_TOKEN"] != "${JIRA_API_TOKEN}" {
		t.Errorf("Env[JIRA_API_TOKEN] = %q, want ${JIRA_API_TOKEN}", s.Env["JIRA_API_TOKEN"])
	}
}

func TestServerForNoAuthOmitsEnv(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind:  artifact.KindTool,
			Name:  "demo",
			Extra: map[string]any{"command": "./run.sh"},
		},
	}
	s, err := ServerFor(a)
	if err != nil {
		t.Fatalf("ServerFor() error = %v", err)
	}
	if s.Env != nil {
		t.Errorf("Env = %v, want nil when no auth is declared", s.Env)
	}
}

func TestServerForRequiresCommand(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindTool, Name: "demo"},
		Dir:         "tools/demo",
	}
	if _, err := ServerFor(a); err == nil {
		t.Fatal("ServerFor() with no command set expected error, got nil")
	}
}
