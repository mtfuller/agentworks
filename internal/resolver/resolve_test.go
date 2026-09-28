package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/spec"
)

func TestResolveTeamSharesComponentsAndIsDeterministic(t *testing.T) {
	root := newResolvedProject(t)

	first, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatalf("ResolveTeam() error = %v", err)
	}
	second, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatalf("ResolveTeam() second error = %v", err)
	}
	if first.Digest != second.Digest {
		t.Errorf("digest changed without content change: %q != %q", first.Digest, second.Digest)
	}
	if first.DefinitionDigest == "" || first.Memory != "memory/team.md" {
		t.Errorf("team metadata = %#v", first)
	}
	if len(first.Agents) != 2 {
		t.Fatalf("agents = %d, want 2", len(first.Agents))
	}
	if len(first.Skills) != 5 {
		t.Fatalf("shared skills = %d, want 5 unique components", len(first.Skills))
	}
	if len(first.Tools) != 1 {
		t.Fatalf("shared tools = %d, want 1 unique component", len(first.Tools))
	}
	if first.Tools[0].SelectedProvider != spec.ProviderHost {
		t.Errorf("selected provider = %q, want host", first.Tools[0].SelectedProvider)
	}
	if first.Agents[0].Name != "implementer" || first.Agents[1].Name != "reviewer" {
		t.Errorf("agents are not sorted: %#v", first.Agents)
	}
	for _, agent := range first.Agents {
		if agent.Skills == nil || agent.Tools == nil || agent.Delegates == nil {
			t.Errorf("agent slices must encode as arrays, got %#v", agent)
		}
	}
	for _, skill := range first.Skills {
		if skill.Origin != OriginLocal || skill.Digest == "" {
			t.Errorf("skill = %#v, want local origin and digest", skill)
		}
	}
}

func TestResolveTeamDigestChangesWithComponentContent(t *testing.T) {
	root := newResolvedProject(t)
	before, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "skills", "testing", "notes.txt"), "new guidance")
	after, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest == after.Digest {
		t.Fatal("plan digest did not change after skill content changed")
	}
}

func TestResolveTeamRejectsMissingComponent(t *testing.T) {
	root := newResolvedProject(t)
	writeAgent(t, root, "reviewer", "skills: [missing]\ntools: [jira]")

	_, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err == nil || !strings.Contains(err.Error(), `agent skill "missing"`) || !strings.Contains(err.Error(), "no local or installed") {
		t.Fatalf("ResolveTeam() error = %v, want actionable missing-skill error", err)
	}
}

func TestResolveTeamRejectsDelegationCycle(t *testing.T) {
	root := newResolvedProject(t)
	writeAgent(t, root, "implementer", "skills: [planning]\ntools: [jira]\ndelegates: [reviewer]")
	writeAgent(t, root, "reviewer", "skills: [review]\ntools: [jira]\ndelegates: [implementer]")

	_, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err == nil || !strings.Contains(err.Error(), "implementer -> reviewer -> implementer") {
		t.Fatalf("ResolveTeam() error = %v, want delegation cycle", err)
	}
}

func TestResolveTeamRejectsDelegateOutsideTeam(t *testing.T) {
	root := newResolvedProject(t)
	writeAgent(t, root, "implementer", "skills: [planning]\ntools: [jira]\ndelegates: [researcher]")

	_, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err == nil || !strings.Contains(err.Error(), "not a member of the team") {
		t.Fatalf("ResolveTeam() error = %v, want team-membership error", err)
	}
}

func TestResolveTeamRejectsSilentDependencyShadowing(t *testing.T) {
	root := newResolvedProject(t)
	writeFile(t, filepath.Join(root, spec.ProjectFile), `
format: 2
name: engineering-agents
teams: [engineering]
workspaces: [product]
dependencies:
  skills:
    planning:
      source: github:example/skills
`)

	_, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err == nil || !strings.Contains(err.Error(), "conflicts with declared or installed dependency") {
		t.Fatalf("ResolveTeam() error = %v, want shadowing error", err)
	}
}

func TestResolveTeamUsesDeclaredInstalledSkill(t *testing.T) {
	root := newResolvedProject(t)
	writeFile(t, filepath.Join(root, spec.ProjectFile), `
format: 2
name: engineering-agents
teams: [engineering]
workspaces: [product]
dependencies:
  skills:
    obra/code-review:
      source: github:obra/agent-skills
      path: skills/code-review
      ref: v1.4.0
`)
	writeAgent(t, root, "implementer", "skills: [obra/code-review]\ntools: [jira]")
	writeAgent(t, root, "reviewer", "skills: [obra/code-review]\ntools: [jira]")
	writeSkill(t, filepath.Join(root, ".agentworks", "deps", "skills", "obra", "code-review"), "code-review")

	plan, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatalf("ResolveTeam() error = %v", err)
	}
	if len(plan.Skills) != 1 || plan.Skills[0].Name != "obra/code-review" || plan.Skills[0].Origin != OriginInstalled {
		t.Fatalf("skills = %#v, want one installed skill", plan.Skills)
	}
}

func TestResolveTeamRequiresCompatibleToolProvider(t *testing.T) {
	root := newResolvedProject(t)

	_, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderRemote}})
	if err == nil || !strings.Contains(err.Error(), "no compatible runtime variant") {
		t.Fatalf("ResolveTeam() error = %v, want provider error", err)
	}
}

func TestCheckedInAgentTeamExampleResolves(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "agent-team")
	plan, err := ResolveTeam(root, "engineering", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatalf("ResolveTeam(example) error = %v", err)
	}
	if len(plan.Agents) != 2 || len(plan.Skills) != 5 || len(plan.Tools) != 1 {
		t.Fatalf("example closure = %d agents, %d skills, %d tools", len(plan.Agents), len(plan.Skills), len(plan.Tools))
	}
}

func TestCheckedInNonSoftwareExampleResolvesWithoutGitTooling(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "research-team")
	plan, err := ResolveTeam(root, "research", Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatalf("ResolveTeam(non-software example) error = %v", err)
	}
	if len(plan.Agents) != 2 || len(plan.Skills) != 2 || len(plan.Tools) != 0 {
		t.Fatalf("non-software closure = %d agents, %d skills, %d tools", len(plan.Agents), len(plan.Skills), len(plan.Tools))
	}
	workspace := t.TempDir()
	if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
		t.Fatalf("test workspace unexpectedly has Git metadata: %v", err)
	}
}

func newResolvedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, spec.ProjectFile), `
format: 2
name: engineering-agents
teams: [engineering]
workspaces: [product]
runtime:
  storage_limit: 1GB
  shutdown_grace: 8s
`)
	writeFile(t, filepath.Join(root, "teams", "engineering", spec.TeamFile), `
name: engineering
agents: [reviewer, implementer]
default_agent: implementer
memory:
  team: memory/team.md
`)
	writeAgent(t, root, "implementer", "skills: [planning, testing, code-search, review]\ntools: [jira]\ndelegates: [reviewer]")
	writeAgent(t, root, "reviewer", "skills: [review, security, testing]\ntools: [jira]")
	for _, skill := range []string{"planning", "testing", "code-search", "review", "security"} {
		writeSkill(t, filepath.Join(root, "skills", skill), skill)
	}
	writeFile(t, filepath.Join(root, "tools", "jira", spec.ToolFile), `
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
	return root
}

func writeAgent(t *testing.T, root, name, frontmatter string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "agents", name, spec.AgentFile), "---\nname: "+name+"\ndescription: Agent "+name+"\n"+frontmatter+"\nmax_permission: collaborate\n---\n\n# "+name+"\n\nFollow the team instructions.\n")
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\ndescription: Guidance for "+name+" work.\n---\n\n# "+name+"\n\nUse this guidance.\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
