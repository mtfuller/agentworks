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

func TestServerForDirWrapsCommandWithCD(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind:  artifact.KindTool,
			Name:  "demo",
			Extra: map[string]any{"command": "python3 src/main.py"},
		},
	}

	s, err := ServerForDir(a, "tools/demo")
	if err != nil {
		t.Fatalf("ServerForDir() error = %v", err)
	}
	want := "cd 'tools/demo' && python3 src/main.py"
	if len(s.Args) != 2 || s.Args[0] != "-c" || s.Args[1] != want {
		t.Errorf("Args = %v, want [-c %q]", s.Args, want)
	}
}

func TestServerForDirEmptyOrDotLeavesCommandUnwrapped(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind:  artifact.KindTool,
			Name:  "demo",
			Extra: map[string]any{"command": "python3 src/main.py"},
		},
	}
	for _, dir := range []string{"", "."} {
		s, err := ServerForDir(a, dir)
		if err != nil {
			t.Fatalf("ServerForDir(%q) error = %v", dir, err)
		}
		if len(s.Args) != 2 || s.Args[1] != "python3 src/main.py" {
			t.Errorf("ServerForDir(%q).Args = %v, want the command unwrapped", dir, s.Args)
		}
	}
}

func TestServerForDirPropagatesMissingCommandError(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindTool, Name: "demo"},
		Dir:         "tools/demo",
	}
	if _, err := ServerForDir(a, "tools/demo"); err == nil {
		t.Fatal("ServerForDir() with no command set expected error, got nil")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("tools/demo"); got != "'tools/demo'" {
		t.Errorf("shellQuote(tools/demo) = %q, want 'tools/demo'", got)
	}
	if got := shellQuote("o'brien"); got != `'o'\''brien'` {
		t.Errorf("shellQuote(o'brien) = %q, want 'o'\\''brien'", got)
	}
}
