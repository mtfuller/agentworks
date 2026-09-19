package importer

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/lockfile"
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
	if a.Dir != filepath.Join(root, "skills", "owner", "csv-analyzer") || a.Namespace != "owner" {
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
		filepath.Join(root, "skills", "owner", "s1", "skill.md"),
		filepath.Join(root, "skills", "owner", "s2", "skill.md"),
		filepath.Join(root, "agents", "owner", "a1", "agent.md"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to be written: %v", want, err)
		}
	}
}

func TestPlanReportsUnsupported(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)
	writeTestFile(t, filepath.Join(fixture, "hooks", "hooks.json"), `{"hooks":{"Stop":[{"hooks":[{"type":"prompt","prompt":"Is it done?"}]}]}}`, 0o644)
	writeTestFile(t, filepath.Join(fixture, ".mcp.json"), `{"mcpServers":{"broken":{"type":"stdio"}}}`, 0o644)

	src := Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	defer plan.Close()
	if len(plan.Artifacts) != 3 {
		t.Errorf("len(Artifacts) = %d, want 3 (skills/agents still planned)", len(plan.Artifacts))
	}
	if len(plan.Unsupported) != 2 {
		t.Fatalf("len(Unsupported) = %d, want 2 (the prompt hook and the command-less server), got %v", len(plan.Unsupported), plan.Unsupported)
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

func TestPlanTracksHashSourcesAndSubpaths(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	a := plan.Artifacts[0]
	if plan.HashSources[a.Dir] != fixture {
		t.Errorf("HashSources[%s] = %q, want %q (the whole fetched root for a bare skill)", a.Dir, plan.HashSources[a.Dir], fixture)
	}
	if plan.Subpaths[a.Dir] != "" {
		t.Errorf("Subpaths[%s] = %q, want \"\" for a bare skill import", a.Dir, plan.Subpaths[a.Dir])
	}
}

func TestPlanPluginTracksHashSourcesAndSubpathsPerArtifact(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)

	src := Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}

	for _, a := range plan.Artifacts {
		hashSrc, ok := plan.HashSources[a.Dir]
		if !ok || hashSrc == "" {
			t.Errorf("HashSources[%s] missing", a.Dir)
		}
		subpath, ok := plan.Subpaths[a.Dir]
		if !ok {
			t.Errorf("Subpaths[%s] missing", a.Dir)
			continue
		}
		switch a.Kind {
		case artifact.KindSkill:
			if !strings.HasPrefix(subpath, "skills/") {
				t.Errorf("Subpaths[%s] = %q, want it to start with skills/", a.Dir, subpath)
			}
		case artifact.KindAgent:
			if subpath != "agents/a1.md" {
				t.Errorf("Subpaths[%s] = %q, want agents/a1.md", a.Dir, subpath)
			}
		}
	}
}

func TestPlanRecordLockEntries(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer", Ref: "main"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	lf, err := lockfile.Load(root)
	if err != nil {
		t.Fatalf("lockfile.Load() error = %v", err)
	}
	if err := plan.RecordLockEntries(root, lf); err != nil {
		t.Fatalf("RecordLockEntries() error = %v", err)
	}

	entry, ok := lf.Imports["skills/owner/csv-analyzer"]
	if !ok {
		t.Fatalf("Imports missing skills/owner/csv-analyzer: %+v", lf.Imports)
	}
	if entry.Source.Repo != "owner/csv-analyzer" || entry.Source.Ref != "main" {
		t.Errorf("entry.Source = %+v, want repo=owner/csv-analyzer ref=main", entry.Source)
	}
	if entry.ContentSHA256 == "" {
		t.Error("entry.ContentSHA256 should be set")
	}

	// The hash should be reproducible from the same fetched content, so a
	// no-op "update" against unchanged content would report no drift.
	wantHash, err := lockfile.HashDir(fixture)
	if err != nil {
		t.Fatalf("HashDir() error = %v", err)
	}
	if entry.ContentSHA256 != wantHash {
		t.Errorf("entry.ContentSHA256 = %q, want %q (hash of the raw fetched content, not the finalized artifact)", entry.ContentSHA256, wantHash)
	}
}

func TestPlanOverwriteKeepsExistingDirAndRefreshesContent(t *testing.T) {
	root := newTestProject(t)
	fixture := t.TempDir()
	writeSkillFixture(t, fixture, "csv-analyzer", "Analyze a CSV.")

	src := Source{Kind: SourceGitHub, Repo: "owner/csv-analyzer"}
	plan, err := detect(root, src, fixture, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	a := plan.Artifacts[0]

	// existingDir simulates a local artifact whose directory doesn't
	// exactly match what a fresh detect() would compute (e.g. --name was
	// used at the original import time) -- Overwrite must still find the
	// right copy source via a's *original* Dir, not the explicit dir.
	existingDir := filepath.Join(root, "skills", "my-custom-name")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatalf("mkdir existingDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingDir, "skill.md"), []byte("stale content"), 0o644); err != nil {
		t.Fatalf("seeding stale content: %v", err)
	}
	a.Name = "my-custom-name" // Validate() requires Name to match dir basename

	if err := plan.Overwrite(a, existingDir); err != nil {
		t.Fatalf("Overwrite() error = %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(existingDir, "skill.md"))
	if err != nil {
		t.Fatalf("skill.md missing after Overwrite: %v", err)
	}
	if strings.Contains(string(manifest), "stale content") {
		t.Error("Overwrite() left the old stale content in place")
	}
	if !strings.Contains(string(manifest), "kind: skill") {
		t.Errorf("skill.md content = %q, want freshly-rendered frontmatter", manifest)
	}
	if _, err := os.Stat(filepath.Join(existingDir, "scripts", "main.py")); err != nil {
		t.Errorf("scripts/main.py not copied by Overwrite: %v", err)
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

func TestDefaultNamespace(t *testing.T) {
	tests := []struct {
		src  Source
		want string
	}{
		{Source{Kind: SourceGitHub, Repo: "obra/superpowers"}, "obra"},
		{Source{Kind: SourceGitHub, Repo: "My_Org/x"}, "my-org"},
		{Source{Kind: SourceArchive, URL: "https://www.agentskills.codes/api/skills/download/19"}, "agentskills"},
		{Source{Kind: SourceArchive, URL: "%%%"}, "imported"},
	}
	for _, tt := range tests {
		if got := tt.src.DefaultNamespace(); got != tt.want {
			t.Errorf("DefaultNamespace(%+v) = %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestPlanNamespaceOverride(t *testing.T) {
	root := newTestProject(t)
	fixture := writePluginFixture(t)

	plan, err := detect(root, Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}, fixture, Options{Namespace: "@Acme Corp"})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	for _, a := range plan.Artifacts {
		if a.Namespace != "acme-corp" {
			t.Errorf("%s namespace = %q, want acme-corp", a.Name, a.Namespace)
		}
		if got := filepath.Base(filepath.Dir(a.Dir)); got != "acme-corp" {
			t.Errorf("%s dir = %s, want it under acme-corp/", a.Name, a.Dir)
		}
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestImportedArtifactsDiscoverAsNamespaced is the point of namespacing:
// what an import writes must load back as "@owner/name", distinct from a
// same-named artifact the user wrote themselves.
func TestImportedArtifactsDiscoverAsNamespaced(t *testing.T) {
	root := newTestProject(t)
	mine := filepath.Join(root, "skills", "s1")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "skill.md"), []byte("---\nkind: skill\nname: s1\ndescription: My own s1.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := detect(root, Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}, writePluginFixture(t), Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v (a same-named local artifact must not collide)", err)
	}

	found, errs := project.Discover(root, artifact.KindSkill)
	if len(errs) != 0 {
		t.Fatalf("Discover() errs = %v", errs)
	}
	var names []string
	for _, a := range found {
		names = append(names, a.DisplayName())
	}
	sort.Strings(names)
	if want := []string{"@owner/s1", "@owner/s2", "s1"}; !reflect.DeepEqual(names, want) {
		t.Errorf("discovered %v, want %v", names, want)
	}
}

// TestOverwriteKeepsLegacyFlatImportUnnamespaced covers `agentworks update`
// on an artifact imported before imports were namespaced: it stays where
// it is, un-namespaced, rather than failing validation.
func TestOverwriteKeepsLegacyFlatImportUnnamespaced(t *testing.T) {
	root := newTestProject(t)
	plan, err := detect(root, Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}, writePluginFixture(t), Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	legacy := filepath.Join(root, "skills", "s1")
	if err := plan.Overwrite(plan.Artifacts[0], legacy); err != nil {
		t.Fatalf("Overwrite() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacy, "skill.md")); err != nil {
		t.Errorf("legacy dir not rewritten in place: %v", err)
	}
}
