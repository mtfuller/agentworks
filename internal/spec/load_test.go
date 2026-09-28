package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadProject(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ProjectFile), `
format: 2
name: software-factory
teams: [delivery]
workspaces: [product]
dependencies:
  skills:
    obra/code-review:
      source: github:obra/agent-skills
      path: skills/code-review
      ref: v1.4.0
  tools: {}
`)

	project, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}
	if project.Runtime.StorageLimit != DefaultStorageLimit {
		t.Errorf("StorageLimit = %q, want %q", project.Runtime.StorageLimit, DefaultStorageLimit)
	}
	if project.Runtime.ShutdownGrace != DefaultShutdownGrace {
		t.Errorf("ShutdownGrace = %q, want %q", project.Runtime.ShutdownGrace, DefaultShutdownGrace)
	}
}

func TestLoadProjectRejectsUnknownField(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ProjectFile), "format: 2\nname: example\ntargtes: [claude-code]\n")

	_, err := LoadProject(root)
	if err == nil || !strings.Contains(err.Error(), "field targtes not found") {
		t.Fatalf("LoadProject() error = %v, want unknown-field error", err)
	}
}

func TestLoadProjectRejectsWrongFormatAndDuplicateRefs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ProjectFile), "format: 1\nname: example\nteams: [delivery, delivery]\n")

	_, err := LoadProject(root)
	if err == nil {
		t.Fatal("LoadProject() error = nil, want validation error")
	}
	for _, want := range []string{"format must be 2", `teams contains duplicate "delivery"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("LoadProject() error = %v, want %q", err, want)
		}
	}
}

func TestLoadLocal(t *testing.T) {
	root := t.TempDir()

	missing, err := LoadLocal(root)
	if err != nil {
		t.Fatalf("LoadLocal() missing file error = %v", err)
	}
	if missing.Workspaces == nil || len(missing.Workspaces) != 0 {
		t.Fatalf("missing Workspaces = %#v, want empty map", missing.Workspaces)
	}

	workspace := filepath.Join(root, "checkout")
	writeTestFile(t, filepath.Join(root, LocalFile), "workspaces:\n  product:\n    path: "+workspace+"\n")
	local, err := LoadLocal(root)
	if err != nil {
		t.Fatalf("LoadLocal() error = %v", err)
	}
	if got := local.Workspaces["product"].Path; got != workspace {
		t.Errorf("workspace path = %q, want %q", got, workspace)
	}
}

func TestLoadLocalRejectsRelativePath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, LocalFile), "workspaces:\n  product:\n    path: ../checkout\n")

	_, err := LoadLocal(root)
	if err == nil || !strings.Contains(err.Error(), "path must be absolute") {
		t.Fatalf("LoadLocal() error = %v, want absolute-path error", err)
	}
}

func TestDiscoverRoutesAndSourcesKeepsValidDefinitionsVisible(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "sources", "nightly", SourceFile), "name: nightly\nkind: schedule\nschedule:\n  every: 1h\n  catch_up: latest\nevent:\n  type: maintenance.requested\n")
	writeTestFile(t, filepath.Join(root, "routes", "valid", RouteFile), "name: valid\nwhen:\n  source: nightly\ninvoke:\n  team: engineering\n  workspace: product\ntests:\n  - name: nightly\n    event:\n      source: nightly\n      type: maintenance.requested\n    expect: valid\n")
	writeTestFile(t, filepath.Join(root, "routes", "broken", RouteFile), "name: broken\nunknown: true\n")

	sources, sourceIssues := DiscoverSources(root)
	routes, routeIssues := DiscoverRoutes(root)
	if len(sources) != 1 || len(sourceIssues) != 0 {
		t.Fatalf("sources=%#v issues=%#v", sources, sourceIssues)
	}
	if len(routes) != 1 || len(routes[0].Tests) != 1 || len(routeIssues) != 1 {
		t.Fatalf("routes=%#v issues=%#v", routes, routeIssues)
	}
}

func TestEventDefinitionValidation(t *testing.T) {
	disabled := false
	if (Source{Enabled: &disabled}).IsEnabled() || (Route{Enabled: &disabled}).IsEnabled() {
		t.Fatal("explicitly disabled definition reported enabled")
	}
	if !(Source{}).IsEnabled() || !(Route{}).IsEnabled() {
		t.Fatal("definition did not default enabled")
	}
	badSource := Source{Name: "bad name", Kind: "webhook", Schedule: SourceSchedule{Every: "nope", CatchUp: "forever"}}
	if err := badSource.Validate(); err == nil {
		t.Fatal("invalid source accepted")
	}
	badRoute := Route{
		Name: "bad name", Invoke: RouteInvoke{Team: "../team", Workspace: "", Harness: "bad harness", Permission: "root"},
		When:  RouteWhen{Fields: map[string]any{"": []string{"not scalar"}}},
		Tests: []RouteTest{{Name: "duplicate", Event: RouteTestEvent{}}, {Name: "duplicate", Event: RouteTestEvent{}, Expect: ""}},
	}
	if err := badRoute.Validate(); err == nil {
		t.Fatal("invalid route accepted")
	}
}

func TestConnectorSourceValidation(t *testing.T) {
	jira := Source{Name: "jira-main", Kind: SourceJira, Poll: SourcePoll{Every: "1m", InitialLookback: "24h"}, Jira: JiraSource{BaseURL: "https://example.atlassian.net", JQL: "assignee = currentUser()", EmailEnv: "JIRA_EMAIL", TokenEnv: "JIRA_TOKEN"}}
	github := Source{Name: "github-main", Kind: SourceGitHub, Poll: SourcePoll{Every: "1m"}, GitHub: GitHubSource{Repository: "acme/widgets.go", TokenEnv: "GITHUB_TOKEN", WorkItemPattern: `[A-Z]+-[0-9]+`}}
	if err := jira.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := github.Validate(); err != nil {
		t.Fatal(err)
	}
	jira.Jira.BaseURL = "http://not-loopback.example"
	if err := jira.Validate(); err == nil {
		t.Fatal("insecure Jira URL accepted")
	}
	github.GitHub.Repository = "missing-slash"
	github.GitHub.WorkItemPattern = "["
	if err := github.Validate(); err == nil {
		t.Fatal("invalid GitHub source accepted")
	}
}

func TestLoadAgent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agents", "implementer", AgentFile)
	writeTestFile(t, path, `---
name: implementer
description: Implements scoped changes
skills: [obra/code-review]
tools: [jira]
delegates: [reviewer]
max_permission: collaborate
memory: memory/agents/implementer.md
---
# Implementer

Make the smallest safe change.
`)

	agent, err := LoadAgent(root, "implementer")
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}
	if agent.MaxPermission != PermissionCollaborate {
		t.Errorf("MaxPermission = %q, want %q", agent.MaxPermission, PermissionCollaborate)
	}
	if !strings.Contains(agent.Instructions, "smallest safe change") {
		t.Errorf("Instructions = %q", agent.Instructions)
	}
	if agent.Path != path {
		t.Errorf("Path = %q, want %q", agent.Path, path)
	}
}

func TestLoadAgentDefaultsToReadonly(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "agents", "researcher", AgentFile), `---
name: researcher
---
Research the question.
`)

	agent, err := LoadAgent(root, "researcher")
	if err != nil {
		t.Fatalf("LoadAgent() error = %v", err)
	}
	if agent.MaxPermission != PermissionReadonly {
		t.Errorf("MaxPermission = %q, want readonly", agent.MaxPermission)
	}
}

func TestLoadAgentRejectsInvalidDocuments(t *testing.T) {
	tests := map[string]struct {
		name string
		doc  string
		want string
	}{
		"unknown field": {
			name: "researcher",
			doc:  "---\nname: researcher\npermission: readwrite\n---\nResearch.\n",
			want: "field permission not found",
		},
		"directory mismatch": {
			name: "researcher",
			doc:  "---\nname: writer\n---\nResearch.\n",
			want: "does not match directory",
		},
		"empty body": {
			name: "researcher",
			doc:  "---\nname: researcher\n---\n",
			want: "instructions must not be empty",
		},
		"escaping memory": {
			name: "researcher",
			doc:  "---\nname: researcher\nmemory: ../../outside.md\n---\nResearch.\n",
			want: "must not escape its root",
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "agents", test.name, AgentFile), test.doc)
			_, err := LoadAgent(root, test.name)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadAgent() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadersRejectTraversalReferences(t *testing.T) {
	root := t.TempDir()
	for name, load := range map[string]func() error{
		"agent": func() error { _, err := LoadAgent(root, "../secret"); return err },
		"team":  func() error { _, err := LoadTeam(root, "../secret"); return err },
		"tool":  func() error { _, err := LoadTool(root, "../secret"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := load(); err == nil || !strings.Contains(err.Error(), "lowercase kebab-case") {
				t.Fatalf("loader error = %v, want invalid-reference error", err)
			}
		})
	}
}

func TestLoadTeam(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "teams", "delivery", TeamFile), `
name: delivery
description: Delivers scoped work
agents: [implementer, reviewer]
default_agent: implementer
memory:
  team: memory/team.md
`)

	team, err := LoadTeam(root, "delivery")
	if err != nil {
		t.Fatalf("LoadTeam() error = %v", err)
	}
	if team.DefaultAgent != "implementer" {
		t.Errorf("DefaultAgent = %q, want implementer", team.DefaultAgent)
	}
}

func TestLoadTeamRejectsDefaultOutsideTeam(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "teams", "delivery", TeamFile), "name: delivery\nagents: [implementer]\ndefault_agent: reviewer\n")

	_, err := LoadTeam(root, "delivery")
	if err == nil || !strings.Contains(err.Error(), "is not listed in agents") {
		t.Fatalf("LoadTeam() error = %v, want membership error", err)
	}
}

func TestLoadToolNormalizesRuntimeCommands(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "tools", "jira", ToolFile), `
name: jira
description: Read and update Jira work items.
transport: stdio
runtime:
  variants:
    - provider: host
      command: node
      args: [dist/server.js]
      requires: [node>=22]
    - provider: container
      image: ghcr.io/example/jira-mcp@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
      command: [/app/server]
auth: [JIRA_TOKEN]
`)

	tool, err := LoadTool(root, "jira")
	if err != nil {
		t.Fatalf("LoadTool() error = %v", err)
	}
	if got := strings.Join(tool.Runtime.Variants[0].Argv(), " "); got != "node dist/server.js" {
		t.Errorf("host argv = %q, want %q", got, "node dist/server.js")
	}
	if got := strings.Join(tool.Runtime.Variants[1].Argv(), " "); got != "/app/server" {
		t.Errorf("container argv = %q, want /app/server", got)
	}
}

func TestLoadToolRejectsUnpinnedContainer(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "tools", "jira", ToolFile), `
name: jira
transport: stdio
runtime:
  variants:
    - provider: container
      image: ghcr.io/example/jira-mcp:latest
      command: [/app/server]
`)

	_, err := LoadTool(root, "jira")
	if err == nil || !strings.Contains(err.Error(), "pinned sha256 digest") {
		t.Fatalf("LoadTool() error = %v, want pinned-image error", err)
	}
}

func TestLoadToolRejectsUnknownField(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "tools", "jira", ToolFile), `
name: jira
transport: stdio
runtime:
  variants:
    - provider: host
      executable: node
`)

	_, err := LoadTool(root, "jira")
	if err == nil || !strings.Contains(err.Error(), "field executable not found") {
		t.Fatalf("LoadTool() error = %v, want unknown-field error", err)
	}
}

func TestLoadInstalledRemoteTool(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ".agentworks", "deps", "tools", "github", ToolFile), `
name: github
transport: http
runtime:
  variants:
    - provider: remote
      url: https://mcp.example.test/github
auth: [GITHUB_TOKEN]
`)

	tool, err := LoadInstalledTool(root, "github")
	if err != nil {
		t.Fatalf("LoadInstalledTool() error = %v", err)
	}
	if tool.Runtime.Variants[0].Provider != ProviderRemote {
		t.Errorf("provider = %q, want remote", tool.Runtime.Variants[0].Provider)
	}
}

func TestToolValidationRejectsUnsafeOrConflictingFields(t *testing.T) {
	tool := Tool{
		Name:      "jira",
		Transport: TransportStdio,
		Runtime: ToolRuntime{Variants: []RuntimeVariant{
			{Provider: ProviderRemote, URL: "file:///tmp/socket", Command: Command{"run"}, Image: "image"},
			{Provider: ProviderRemote, URL: "https://example.test"},
		}},
		Auth: []string{"not-valid!", "not-valid!"},
	}
	err := tool.Validate()
	if err == nil {
		t.Fatal("Tool.Validate() error = nil")
	}
	for _, want := range []string{"remote provider requires http or sse", "absolute http or https", "cannot set command or image", "duplicate provider", "valid environment variable", "contains duplicate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Tool.Validate() error = %v, want %q", err, want)
		}
	}
}

func TestCommandYAMLRoundTrip(t *testing.T) {
	for _, command := range []Command{{"node"}, {"node", "server.js"}} {
		data, err := yaml.Marshal(command)
		if err != nil {
			t.Fatalf("yaml.Marshal(%#v): %v", command, err)
		}
		var decoded Command
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("yaml.Unmarshal(%q): %v", data, err)
		}
		if strings.Join(decoded, "\x00") != strings.Join(command, "\x00") {
			t.Errorf("round trip = %#v, want %#v", decoded, command)
		}
	}
}

func TestStrictDecodeRejectsMultipleDocuments(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ProjectFile), "format: 2\nname: example\n---\nname: second\n")
	_, err := LoadProject(root)
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("LoadProject() error = %v, want multiple-document error", err)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
