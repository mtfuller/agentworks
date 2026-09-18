package mcpconfig

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestServerForBuildsStdioEntry(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindMCP,
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
			Kind:  artifact.KindMCP,
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
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindMCP, Name: "demo"},
		Dir:         "tools/demo",
	}
	if _, err := ServerFor(a); err == nil {
		t.Fatal("ServerFor() with no command set expected error, got nil")
	}
}

func TestServerForDirWrapsCommandWithCD(t *testing.T) {
	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind:  artifact.KindMCP,
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
			Kind:  artifact.KindMCP,
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
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindMCP, Name: "demo"},
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

func newMCP(extra map[string]any) *artifact.Artifact {
	return &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindMCP, Name: "demo", Extra: extra},
		Dir:         "mcp/demo",
	}
}

func TestServerForArgsRunsCommandDirectly(t *testing.T) {
	s, err := ServerFor(newMCP(map[string]any{
		"command": "npx",
		"args":    []any{"-y", "some-server@1.2.3"},
	}))
	if err != nil {
		t.Fatalf("ServerFor() error = %v", err)
	}
	if s.Command != "npx" || len(s.Args) != 2 || s.Args[0] != "-y" || s.Args[1] != "some-server@1.2.3" {
		t.Errorf("got Command=%q Args=%v, want npx [-y some-server@1.2.3] (no sh -c wrapper)", s.Command, s.Args)
	}
}

func TestServerForRemote(t *testing.T) {
	for _, transport := range []string{"http", "sse"} {
		s, err := ServerFor(newMCP(map[string]any{
			"transport": transport,
			"url":       "https://example.com/mcp",
			"headers":   map[string]any{"Authorization": "Bearer ${TOKEN}"},
			"auth":      []any{"TOKEN"},
		}))
		if err != nil {
			t.Fatalf("%s: ServerFor() error = %v", transport, err)
		}
		if s.Type != transport || s.URL != "https://example.com/mcp" || s.Command != "" {
			t.Errorf("%s: got %+v, want a remote entry with no command", transport, s)
		}
		if s.Headers["Authorization"] != "Bearer ${TOKEN}" || s.Env["TOKEN"] != "${TOKEN}" {
			t.Errorf("%s: headers/env not carried through: %+v", transport, s)
		}
	}
}

func TestServerForEnvMergesLiteralsAndAuth(t *testing.T) {
	s, err := ServerFor(newMCP(map[string]any{
		"command": "./run.sh",
		"env":     map[string]any{"REGION": "us-east-1", "TOKEN": "literal-should-lose"},
		"auth":    []any{"TOKEN"},
	}))
	if err != nil {
		t.Fatalf("ServerFor() error = %v", err)
	}
	if s.Env["REGION"] != "us-east-1" {
		t.Errorf("Env[REGION] = %q, want the literal from env:", s.Env["REGION"])
	}
	if s.Env["TOKEN"] != "${TOKEN}" {
		t.Errorf("Env[TOKEN] = %q, want auth's ${TOKEN} reference to win over a literal", s.Env["TOKEN"])
	}
}

func TestServerForDirLeavesRemoteAlone(t *testing.T) {
	s, err := ServerForDir(newMCP(map[string]any{"transport": "http", "url": "https://example.com/mcp"}), "mcp/demo")
	if err != nil {
		t.Fatalf("ServerForDir() error = %v", err)
	}
	if s.Command != "" || len(s.Args) != 0 || s.URL == "" {
		t.Errorf("got %+v, want an unwrapped remote entry", s)
	}
}

func TestServerForDirWrapsArgs(t *testing.T) {
	s, err := ServerForDir(newMCP(map[string]any{
		"command": "npx",
		"args":    []any{"-y", "pkg", "with space"},
	}), "mcp/demo")
	if err != nil {
		t.Fatalf("ServerForDir() error = %v", err)
	}
	want := "cd 'mcp/demo' && npx -y pkg 'with space'"
	if len(s.Args) != 2 || s.Args[1] != want {
		t.Errorf("Args = %v, want [-c %q]", s.Args, want)
	}
}

func TestCommandLine(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{"none", map[string]any{}, ""},
		{"command only", map[string]any{"command": "python3 src/main.py"}, "python3 src/main.py"},
		{"args are quoted only when needed", map[string]any{"command": "npx", "args": []any{"-y", "a b", "it's"}}, `npx -y 'a b' 'it'\''s'`},
	}
	for _, tt := range tests {
		if got := CommandLine(newMCP(tt.extra)); got != tt.want {
			t.Errorf("%s: CommandLine() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		extra   map[string]any
		wantErr bool
	}{
		{"stdio with command", map[string]any{"command": "x"}, false},
		{"stdio with nothing at all", map[string]any{}, false},
		{"auth without command", map[string]any{"auth": []any{"A"}}, true},
		{"args without command", map[string]any{"args": []any{"a"}}, true},
		{"stdio with url", map[string]any{"command": "x", "url": "https://e.com"}, true},
		{"unknown transport", map[string]any{"transport": "carrier-pigeon"}, true},
		{"http without url", map[string]any{"transport": "http"}, true},
		{"http with command", map[string]any{"transport": "http", "url": "https://e.com", "command": "x"}, true},
		{"http ok", map[string]any{"transport": "http", "url": "https://e.com"}, false},
		{"literal bearer token", map[string]any{"transport": "http", "url": "https://e.com", "headers": map[string]any{"Authorization": "Bearer abc123"}}, true},
		{"literal api key header", map[string]any{"transport": "sse", "url": "https://e.com", "headers": map[string]any{"X-Api-Key": "abc123"}}, true},
		{"env-referenced token", map[string]any{"transport": "http", "url": "https://e.com", "headers": map[string]any{"Authorization": "Bearer ${T}"}}, false},
		{"literal non-sensitive header", map[string]any{"transport": "http", "url": "https://e.com", "headers": map[string]any{"X-Api-Version": "2"}}, false},
	}
	for _, tt := range tests {
		if err := Validate(newMCP(tt.extra)); (err != nil) != tt.wantErr {
			t.Errorf("%s: Validate() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}
