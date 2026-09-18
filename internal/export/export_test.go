package export

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

// newProject builds a project with two of the user's own artifacts and two
// skills/agents under the "obra" namespace (as `agentworks add obra/...`
// would file them).
func newProject(t *testing.T, targets []string) string {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", targets); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		kind artifact.Kind
		name string
	}{
		{artifact.KindSkill, "mine"},
		{artifact.KindAgent, "helper"},
		{artifact.KindSkill, "obra/brainstorm"},
		{artifact.KindSkill, "obra/plan"},
	} {
		if _, err := scaffold.New(root, spec.kind, spec.name, scaffold.Options{Description: "does " + spec.name}); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func run(t *testing.T, root string, req Request) (Result, error) {
	t.Helper()
	req.Root = root
	req.ProjectName = "proj"
	req.OutDir = filepath.Join(root, "dist")
	lf, err := lockfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return Run(req, lf)
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

func zipNames(t *testing.T, path string) []string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func TestResolveTargets(t *testing.T) {
	root := newProject(t, []string{"claude-code"})
	if got, err := ResolveTargets(root, nil); err != nil || len(got) != 1 || got[0] != "claude-code" {
		t.Errorf("ResolveTargets(nil) = %v, %v; want the manifest's targets", got, err)
	}
	if got, _ := ResolveTargets(root, []string{"cursor"}); len(got) != 1 || got[0] != "cursor" {
		t.Errorf("explicit targets should win, got %v", got)
	}

	bare := newProject(t, nil)
	if _, err := ResolveTargets(bare, nil); err == nil || !strings.Contains(err.Error(), "targets:") {
		t.Errorf("ResolveTargets with none configured = %v, want an error explaining how to set them", err)
	}
}

func TestRunEverythingInOnePlugin(t *testing.T) {
	root := newProject(t, nil)
	res, err := run(t, root, Request{Targets: []string{"claude-code"}, Format: FormatPlugin})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 1 || res.Outputs[0].Name != "proj" || res.Outputs[0].Members != 4 {
		t.Fatalf("Outputs = %+v, want one 4-member plugin named after the project", res.Outputs)
	}
	plugin := filepath.Join(root, "dist", "claude-code", "proj")
	for _, want := range []string{"skills/mine/SKILL.md", "skills/brainstorm/SKILL.md", "skills/plan/SKILL.md", "agents/helper.md"} {
		if !exists(t, filepath.Join(plugin, want)) {
			t.Errorf("plugin is missing %s", want)
		}
	}
}

func TestRunPerNamespacePlugins(t *testing.T) {
	root := newProject(t, nil)
	res, err := run(t, root, Request{Targets: []string{"claude-code"}, Namespaces: []string{"@obra", UnnamespacedToken}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 2 {
		t.Fatalf("Outputs = %+v, want two plugins", res.Outputs)
	}
	out := filepath.Join(root, "dist", "claude-code")
	if !exists(t, filepath.Join(out, "obra", "skills", "brainstorm", "SKILL.md")) {
		t.Error("obra plugin is missing its skill")
	}
	if exists(t, filepath.Join(out, "obra", "skills", "mine")) {
		t.Error("obra plugin leaked an un-namespaced skill")
	}
	if !exists(t, filepath.Join(out, "proj", "agents", "helper.md")) {
		t.Error("un-namespaced artifacts should bundle under the project name")
	}
}

func TestRunOnlyListedNamespacesAreBundled(t *testing.T) {
	root := newProject(t, nil)
	if _, err := run(t, root, Request{Targets: []string{"claude-code"}, Namespaces: []string{"obra"}}); err != nil {
		t.Fatal(err)
	}
	if exists(t, filepath.Join(root, "dist", "claude-code", "proj")) {
		t.Error("the un-namespaced plugin was written though only obra was requested")
	}
}

func TestRunUnknownNamespaceErrors(t *testing.T) {
	root := newProject(t, nil)
	_, err := run(t, root, Request{Targets: []string{"claude-code"}, Namespaces: []string{"nope"}})
	if err == nil || !strings.Contains(err.Error(), "obra") {
		t.Errorf("err = %v, want one naming the namespaces that do exist", err)
	}
}

func TestRunSameNameInTwoNamespacesDoesNotCollide(t *testing.T) {
	root := newProject(t, nil)
	if _, err := scaffold.New(root, artifact.KindSkill, "acme/plan", scaffold.Options{Description: "acme plan"}); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, root, Request{Targets: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(root, "dist", "claude-code", "proj", "skills")
	for _, want := range []string{"obra-plan", "acme-plan", "mine"} {
		if !exists(t, filepath.Join(skills, want, "SKILL.md")) {
			t.Errorf("missing skills/%s -- same-named skills must both survive", want)
		}
	}
}

func TestRunMultipleTargetsGetOwnDirectories(t *testing.T) {
	root := newProject(t, nil)
	res, err := run(t, root, Request{Targets: []string{"claude-code", "github-copilot"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 2 {
		t.Fatalf("Outputs = %+v, want one plugin per target", res.Outputs)
	}
	for _, target := range []string{"claude-code", "github-copilot"} {
		if !exists(t, filepath.Join(root, "dist", target, "proj")) {
			t.Errorf("no plugin under dist/%s", target)
		}
	}
}

func TestRunTargetWithoutPluginFormatExportsEachArtifact(t *testing.T) {
	root := newProject(t, nil)
	res, err := run(t, root, Request{Targets: []string{"chatgpt"}})
	if err != nil {
		t.Fatal(err)
	}
	// chatgpt takes skills only: the agent is skipped with a warning, each
	// of the three skills is exported alone.
	if len(res.Outputs) != 3 {
		t.Errorf("Outputs = %+v, want 3 individual skill exports", res.Outputs)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "no plugin format") {
		t.Errorf("Warnings = %v, want a note that chatgpt has no plugin format", res.Warnings)
	}
}

func TestRunSkillsZip(t *testing.T) {
	root := newProject(t, nil)
	res, err := run(t, root, Request{Format: FormatSkillsZip})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 1 {
		t.Fatalf("Outputs = %+v, want one zip", res.Outputs)
	}
	got := zipNames(t, res.Outputs[0].Path)
	want := []string{"brainstorm/SKILL.md", "mine/SKILL.md", "plan/SKILL.md"}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("zip entries %v missing %s", got, w)
		}
	}
	if filepath.Base(res.Outputs[0].Path) != "proj-skills.zip" {
		t.Errorf("zip path = %s", res.Outputs[0].Path)
	}
}

func TestRunSkillFiles(t *testing.T) {
	root := newProject(t, nil)
	res, err := run(t, root, Request{Format: FormatSkillFiles})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 3 {
		t.Fatalf("Outputs = %+v, want one .skill per skill", res.Outputs)
	}
	path := filepath.Join(root, "dist", "skills", "brainstorm.skill")
	names := zipNames(t, path)
	if len(names) == 0 || names[0] != "brainstorm/SKILL.md" {
		t.Errorf(".skill entries = %v, want brainstorm/SKILL.md inside a brainstorm/ folder", names)
	}
}

func TestRunSkillFormatsRejectNamespacesAndNeedSkills(t *testing.T) {
	root := newProject(t, nil)
	if _, err := run(t, root, Request{Format: FormatSkillsZip, Namespaces: []string{"obra"}}); err == nil {
		t.Error("namespaces with a skill format should error")
	}

	noSkills := t.TempDir()
	if _, err := project.Init(noSkills, "p", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffold.New(noSkills, artifact.KindAgent, "a", scaffold.Options{Description: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, noSkills, Request{Format: FormatSkillsZip}); err == nil || !strings.Contains(err.Error(), "no skills") {
		t.Errorf("err = %v, want 'no skills'", err)
	}
}

func TestRunRecordsLockfileAndWarnsOnHandEdit(t *testing.T) {
	root := newProject(t, nil)
	req := Request{Root: root, ProjectName: "proj", OutDir: filepath.Join(root, "dist"), Targets: []string{"claude-code"}}
	lf, _ := lockfile.Load(root)
	if _, err := Run(req, lf); err != nil {
		t.Fatal(err)
	}
	if _, ok := lf.Exports[lockfile.ExportKey("claude-code", "bundle:proj")]; !ok {
		t.Fatalf("no lockfile entry recorded, have %v", lf.Exports)
	}

	edited := filepath.Join(root, "dist", "claude-code", "proj", "skills", "mine", "SKILL.md")
	if err := os.WriteFile(edited, []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(req, lf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "hand-edited") {
		t.Errorf("Warnings = %v, want a hand-edit warning", res.Warnings)
	}
}

func TestNamespaces(t *testing.T) {
	arts := []*artifact.Artifact{
		{Frontmatter: artifact.Frontmatter{Name: "a"}},
		{Frontmatter: artifact.Frontmatter{Name: "b", Namespace: "obra"}},
		{Frontmatter: artifact.Frontmatter{Name: "c", Namespace: "obra"}},
	}
	got := Namespaces(arts)
	if len(got) != 2 || got[0] != "." || got[1] != "obra" {
		t.Errorf("Namespaces() = %v, want [. obra]", got)
	}
}
