package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestInitAndLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproject")

	m, err := Init(dir, "myproject", []string{"claude-code"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.Name != "myproject" {
		t.Errorf("Name = %q, want myproject", m.Name)
	}

	for _, k := range artifact.Kinds() {
		if info, err := os.Stat(filepath.Join(dir, k.DirName())); err != nil || !info.IsDir() {
			t.Errorf("expected directory %s to exist", k.DirName())
		}
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != "myproject" || len(loaded.Targets) != 1 || loaded.Targets[0] != "claude-code" {
		t.Errorf("Load() = %+v, want name=myproject targets=[claude-code]", loaded)
	}
}

func TestInitWritesAgentDocs(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "myproject", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	agentsMD, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsMD), "myproject") {
		t.Errorf("AGENTS.md should mention the project name, got: %s", agentsMD)
	}
	if !strings.Contains(string(agentsMD), ".agents/skills/agentworks-cli/SKILL.md") {
		t.Errorf("AGENTS.md should point at the agentworks-cli skill, got: %s", agentsMD)
	}

	skillPath := filepath.Join(dir, ".agents", "skills", "agentworks-cli", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("reading %s: %v", skillPath, err)
	}
	if !strings.Contains(string(skill), "name: agentworks-cli") {
		t.Errorf("SKILL.md should have an agentworks-cli frontmatter name, got: %s", skill)
	}
	if !strings.Contains(string(skill), "agentworks new") || !strings.Contains(string(skill), "agentworks export") {
		t.Errorf("SKILL.md should document core commands, got: %s", skill)
	}
}

func TestWriteAgentDocsDoesNotClobberHandEdits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("custom content"), 0o644); err != nil {
		t.Fatalf("writing AGENTS.md: %v", err)
	}

	if err := writeAgentDocs(dir, "myproject"); err != nil {
		t.Fatalf("writeAgentDocs() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("reading AGENTS.md: %v", err)
	}
	if string(got) != "custom content" {
		t.Errorf("AGENTS.md = %q, want hand-edited content preserved", got)
	}
}

func TestPublisherIsNilByDefaultAndLoadsWhenHandEdited(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "proj", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Publisher != nil {
		t.Fatalf("Publisher = %+v, want nil for a project that never set one", loaded.Publisher)
	}

	// Simulate a human hand-editing agentworks.yaml to add a publisher
	// block, the way it's actually meant to be set (Init doesn't prompt
	// for it).
	yaml := `name: proj
publisher:
  name: Jane Doe
  website: https://example.org
  privacy_url: https://example.org/privacy
  terms_url: https://example.org/terms
  accent_color: "#123456"
`
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing %s: %v", ManifestFile, err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Publisher == nil {
		t.Fatal("Publisher = nil, want the hand-edited block")
	}
	want := Publisher{
		Name:        "Jane Doe",
		Website:     "https://example.org",
		PrivacyURL:  "https://example.org/privacy",
		TermsURL:    "https://example.org/terms",
		AccentColor: "#123456",
	}
	if *reloaded.Publisher != want {
		t.Errorf("Publisher = %+v, want %+v", *reloaded.Publisher, want)
	}
}

func TestInitRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "proj", nil); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	if _, err := Init(dir, "proj", nil); err == nil {
		t.Fatal("second Init() expected error, got nil")
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "proj", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	nested := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	found, err := FindRoot(nested)
	if err != nil {
		t.Fatalf("FindRoot() error = %v", err)
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	realFound, _ := filepath.EvalSymlinks(found)
	if realFound != realRoot {
		t.Errorf("FindRoot() = %q, want %q", realFound, realRoot)
	}

	if _, err := FindRoot(t.TempDir()); err != ErrNotFound {
		t.Errorf("FindRoot() in empty dir error = %v, want ErrNotFound", err)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "proj", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindSkill, Name: "demo", Description: "A demo."},
		Dir:         filepath.Join(root, "skills", "demo"),
	}
	if err := a.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// A stray subdirectory with no manifest file should be skipped, not error.
	if err := os.MkdirAll(filepath.Join(root, "skills", "not-an-artifact"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	found, errs := Discover(root)
	if len(errs) != 0 {
		t.Fatalf("Discover() errs = %v", errs)
	}
	if len(found) != 1 || found[0].Name != "demo" {
		t.Fatalf("Discover() = %+v, want one artifact named demo", found)
	}

	foundSkillsOnly, errs := Discover(root, artifact.KindSkill)
	if len(errs) != 0 || len(foundSkillsOnly) != 1 {
		t.Fatalf("Discover(KindSkill) = %+v, errs=%v", foundSkillsOnly, errs)
	}
	if none, errs := Discover(root, artifact.KindAgent); len(none) != 0 || len(errs) != 0 {
		t.Fatalf("Discover(KindAgent) = %+v, errs=%v, want empty", none, errs)
	}
}

func TestDiscoverFindsNamespacedArtifacts(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "proj", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	unnamespaced := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindSkill, Name: "demo", Description: "A demo."},
		Dir:         filepath.Join(root, "skills", "demo"),
	}
	if err := unnamespaced.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	namespaced := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindSkill, Name: "demo", Namespace: "team-a", Description: "Team A's demo."},
		Dir:         filepath.Join(root, "skills", "team-a", "demo"),
	}
	if err := namespaced.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	found, errs := Discover(root, artifact.KindSkill)
	if len(errs) != 0 {
		t.Fatalf("Discover() errs = %v", errs)
	}
	if len(found) != 2 {
		t.Fatalf("Discover() = %+v, want 2 artifacts (bare + namespaced)", found)
	}

	var gotQualified []string
	for _, a := range found {
		gotQualified = append(gotQualified, a.QualifiedName())
	}
	sort.Strings(gotQualified)
	want := []string{"demo", "team-a/demo"}
	if len(gotQualified) != len(want) || gotQualified[0] != want[0] || gotQualified[1] != want[1] {
		t.Errorf("QualifiedName()s = %v, want %v", gotQualified, want)
	}
}

func TestResolveArtifactDir(t *testing.T) {
	root := "/proj"
	if got, want := ResolveArtifactDir(root, artifact.KindSkill, "demo"), filepath.Join(root, "skills", "demo"); got != want {
		t.Errorf("ResolveArtifactDir(bare) = %q, want %q", got, want)
	}
	if got, want := ResolveArtifactDir(root, artifact.KindSkill, "team-a/demo"), filepath.Join(root, "skills", "team-a", "demo"); got != want {
		t.Errorf("ResolveArtifactDir(qualified) = %q, want %q", got, want)
	}
}

func TestInitWritesGitignoreWithoutClobbering(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "p", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dist/", "node_modules/", "__pycache__/", ".venv/", ".DS_Store"} {
		if !strings.Contains(string(data), want) {
			t.Errorf(".gitignore missing %s", want)
		}
	}
	if strings.Contains(string(data), "agentworks.lock") {
		t.Error("agentworks.lock must stay tracked")
	}

	existing := t.TempDir()
	if err := os.WriteFile(filepath.Join(existing, ".gitignore"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(existing, "p", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(existing, ".gitignore")); string(got) != "mine\n" {
		t.Errorf("existing .gitignore was overwritten: %q", got)
	}
}
