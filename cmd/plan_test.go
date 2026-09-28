package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCommandResolvesFormat2Project(t *testing.T) {
	root := newPlanProject(t)
	result := runCLI(t, root, "plan", "engineering")
	if result.err != nil {
		t.Fatalf("plan failed: %v\n%s", result.err, result.combined())
	}
	for _, want := range []string{"Resolved example/engineering", "implementer", "planning (local)", "jira (stdio via host)"} {
		if !strings.Contains(result.stdout, want) {
			t.Errorf("stdout = %q, want %q", result.stdout, want)
		}
	}
}

func TestPlanCommandJSON(t *testing.T) {
	root := newPlanProject(t)
	result := runCLI(t, root, "plan", "engineering", "--json")
	if result.err != nil {
		t.Fatalf("plan --json failed: %v\n%s", result.err, result.combined())
	}
	var doc planDoc
	if err := json.Unmarshal([]byte(result.stdout), &doc); err != nil {
		t.Fatalf("decode output: %v\n%s", err, result.stdout)
	}
	if doc.Command != "plan" || !doc.OK || doc.Plan.Digest == "" {
		t.Fatalf("document = %#v", doc)
	}
	if len(doc.Plan.Skills) != 1 || doc.Plan.Skills[0].Name != "planning" {
		t.Fatalf("skills = %#v", doc.Plan.Skills)
	}
}

func TestPlanCommandRejectsUnsupportedProvider(t *testing.T) {
	root := newPlanProject(t)
	result := runCLI(t, root, "plan", "engineering", "--provider", "managed")
	if result.err == nil || !strings.Contains(result.combined(), `unknown provider "managed"`) {
		t.Fatalf("result = %#v, want provider error", result)
	}
}

func newPlanProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePlanFile(t, filepath.Join(root, "agentworks.yaml"), "format: 2\nname: example\nteams: [engineering]\n")
	writePlanFile(t, filepath.Join(root, "teams", "engineering", "team.yaml"), "name: engineering\nagents: [implementer]\ndefault_agent: implementer\n")
	writePlanFile(t, filepath.Join(root, "agents", "implementer", "AGENT.md"), "---\nname: implementer\nskills: [planning]\ntools: [jira]\nmax_permission: readwrite\n---\nImplement the requested change.\n")
	writePlanFile(t, filepath.Join(root, "skills", "planning", "SKILL.md"), "---\nname: planning\ndescription: Plan work before implementation.\n---\nPlan the change.\n")
	writePlanFile(t, filepath.Join(root, "tools", "jira", "tool.yaml"), "name: jira\ntransport: stdio\nruntime:\n  variants:\n    - provider: host\n      command: node\n      args: [server.js]\n")
	return root
}

func writePlanFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
