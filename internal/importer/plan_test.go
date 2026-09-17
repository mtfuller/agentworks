package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
)

func newTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	return root
}

func writeSkillFixture(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "main.py"), []byte("print(1)"), 0o644); err != nil {
		t.Fatalf("writing scripts/main.py: %v", err)
	}
}

func writePluginFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir .claude-plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"demo-kit","description":"A demo kit."}`), 0o644); err != nil {
		t.Fatalf("writing plugin.json: %v", err)
	}
	writeSkillFixture(t, filepath.Join(dir, "skills", "s1"), "s1", "First skill.")
	writeSkillFixture(t, filepath.Join(dir, "skills", "s2"), "s2", "Second skill.")
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatalf("mkdir agents: %v", err)
	}
	agentContent := "---\nname: a1\ndescription: An agent.\n---\n\nagent body\n"
	if err := os.WriteFile(filepath.Join(dir, "agents", "a1.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatalf("writing agents/a1.md: %v", err)
	}
	return dir
}

func TestPlanBareSkill(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if len(plan.Artifacts) != 1 {
		t.Fatalf("len(Artifacts) = %d, want 1", len(plan.Artifacts))
	}
	a := plan.Artifacts[0]
	if a.Kind != artifact.KindSkill || a.Name != "csv-analyzer" {
		t.Errorf("artifact = %s %q, want skill csv-analyzer", a.Kind, a.Name)
	}
	if a.Dir != filepath.Join(root, "skills", "csv-analyzer") {
		t.Errorf("Dir = %q", a.Dir)
	}

	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	// Note: "SKILL.md" and "skill.md" are the same file on a case-insensitive
	// filesystem (default on macOS), so this asserts by content -- Save()'s
	// rendered output, not the raw fetched SKILL.md -- rather than by a
	// second Stat, which would pass either way on such a filesystem.
	manifest, err := os.ReadFile(filepath.Join(a.Dir, "skill.md"))
	if err != nil {
		t.Fatalf("skill.md not written: %v", err)
	}
	if !strings.Contains(string(manifest), "kind: skill") {
		t.Errorf("skill.md content = %q, want AgentWorks-rendered frontmatter (not the raw fetched SKILL.md)", manifest)
	}
	if _, err := os.Stat(filepath.Join(a.Dir, "scripts", "main.py")); err != nil {
		t.Errorf("scripts/main.py not copied: %v", err)
	}
}

func TestPlanPluginDecomposes(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)

	src := Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if plan.PluginName != "demo-kit" {
		t.Errorf("PluginName = %q, want demo-kit", plan.PluginName)
	}
	if len(plan.Artifacts) != 3 {
		t.Fatalf("len(Artifacts) = %d, want 3 (2 skills + 1 agent)", len(plan.Artifacts))
	}

	kinds := map[artifact.Kind]int{}
	for _, a := range plan.Artifacts {
		kinds[a.Kind]++
	}
	if kinds[artifact.KindSkill] != 2 {
		t.Errorf("skill count = %d, want 2", kinds[artifact.KindSkill])
	}
	if kinds[artifact.KindAgent] != 1 {
		t.Errorf("agent count = %d, want 1", kinds[artifact.KindAgent])
	}

	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, want := range []string{
		filepath.Join(root, "skills", "s1", "skill.md"),
		filepath.Join(root, "skills", "s2", "skill.md"),
		filepath.Join(root, "agents", "a1", "agent.md"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to be written: %v", want, err)
		}
	}
}

func TestPlanReportsUnsupported(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)
	if err := os.MkdirAll(filepath.Join(fixture, "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "hooks", "hooks.json"), []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatalf("writing hooks.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatalf("writing .mcp.json: %v", err)
	}

	src := Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if len(plan.Artifacts) != 3 {
		t.Errorf("len(Artifacts) = %d, want 3 (skills/agents still planned)", len(plan.Artifacts))
	}
	if len(plan.Unsupported) != 2 {
		t.Fatalf("len(Unsupported) = %d, want 2 (hooks + tools), got %v", len(plan.Unsupported), plan.Unsupported)
	}
}

func TestPlanRejectsMarketplace(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir .claude-plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fixture, ".claude-plugin", "marketplace.json"), []byte(`{"name":"x","owner":{"name":"y"},"plugins":[]}`), 0o644); err != nil {
		t.Fatalf("writing marketplace.json: %v", err)
	}

	src := Source{Kind: SourceGitHub, Repo: "owner/some-marketplace"}
	if _, err := detect(root, src, fixture, Options{}); err == nil {
		t.Fatal("detect() of a marketplace root expected error, got nil")
	}
}

func TestPlanUnrecognizedRoot(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir() // empty: no SKILL.md, no plugin.json, no marketplace.json

	src := Source{Kind: SourceGitHub, Repo: "owner/nothing-here"}
	if _, err := detect(root, src, fixture, Options{}); err == nil {
		t.Fatal("detect() of an unrecognized root expected error, got nil")
	}
}

func TestPlanStampsProvenance(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer", Ref: "main", Path: "skills/csv-analyzer"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	source, ok := plan.Artifacts[0].Extra["source"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[source] = %#v, want a map", plan.Artifacts[0].Extra["source"])
	}
	if source["ref"] != "main" {
		t.Errorf("source.ref = %v, want main", source["ref"])
	}
	if source["path"] != "skills/csv-analyzer" {
		t.Errorf("source.path = %v, want skills/csv-analyzer", source["path"])
	}
	if source["url"] == "" || source["url"] == nil {
		t.Error("source.url should be set")
	}
}

func TestApplyRejectsCollisionAtomically(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)

	src := Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}

	// Pre-create the destination for one of the three artifacts.
	collided := plan.Artifacts[1].Dir
	if err := os.MkdirAll(collided, 0o755); err != nil {
		t.Fatalf("pre-creating %s: %v", collided, err)
	}

	if err := plan.Apply(); err == nil {
		t.Fatal("Apply() with a colliding destination expected error, got nil")
	}
	for i, a := range plan.Artifacts {
		if i == 1 {
			continue
		}
		if _, err := os.Stat(a.Dir); err == nil {
			t.Errorf("Apply() wrote %s despite a collision elsewhere in the same plan -- not atomic", a.Dir)
		}
	}
}

func TestApplyCopiesSupportingFiles(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	dir := plan.Artifacts[0].Dir
	if _, err := os.Stat(filepath.Join(dir, "scripts", "main.py")); err != nil {
		t.Errorf("scripts/main.py not copied: %v", err)
	}
	// "SKILL.md"/"skill.md" collide on a case-insensitive filesystem
	// (macOS default), so assert by content -- see the matching note in
	// TestPlanBareSkill.
	manifest, err := os.ReadFile(filepath.Join(dir, "skill.md"))
	if err != nil {
		t.Fatalf("skill.md should have been written: %v", err)
	}
	if !strings.Contains(string(manifest), "kind: skill") {
		t.Errorf("skill.md content = %q, want AgentWorks-rendered frontmatter", manifest)
	}
}

func TestApplySlugifiesInvalidName(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "Azure DevOps", "Talk to Azure DevOps.")

	src := Source{Kind: SourceGitHub, Repo: "owner/azure-devops-skill"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	a := plan.Artifacts[0]
	if a.Name != "azure-devops" {
		t.Errorf("Name = %q, want azure-devops", a.Name)
	}
	source := a.Extra["source"].(map[string]any)
	if source["name"] != "Azure DevOps" {
		t.Errorf("source.name = %v, want the original %q preserved", source["name"], "Azure DevOps")
	}
}

func TestApplyFallsBackWhenDescriptionMissing(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "SKILL.md"), []byte("---\nname: demo\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}

	src := Source{Kind: SourceGitHub, Repo: "owner/demo"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if plan.Artifacts[0].Description == "" {
		t.Error("Description should fall back to a generated value, not stay empty")
	}
}

func TestApplyRespectsNameOverride(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer"}
	plan, err := detect(root, src, fixture, Options{Name: "custom-name"})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if plan.Artifacts[0].Name != "custom-name" {
		t.Errorf("Name = %q, want custom-name", plan.Artifacts[0].Name)
	}
}

func TestPlanPluginRejectsNameOverride(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)

	src := Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}
	if _, err := detect(root, src, fixture, Options{Name: "custom-name"}); err == nil {
		t.Fatal("detect() of a plugin with --name expected error, got nil")
	}
}
