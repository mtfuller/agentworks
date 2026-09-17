package cmd

import (
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"

	_ "github.com/mtfuller/agentworks/internal/targets/chatgpt"
	_ "github.com/mtfuller/agentworks/internal/targets/claudecode"
)

func TestValidateExportFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		all     bool
		kind    string
		bundle  string
		wantErr bool
	}{
		{"single path, no flags", []string{"skills/demo"}, false, "", "", false},
		{"all with no path", nil, true, "", "", false},
		{"kind with no path", nil, false, "tool", "", false},
		{"all and kind together", nil, true, "tool", "", true},
		{"all with a path", []string{"skills/demo"}, true, "", "", true},
		{"kind with a path", []string{"skills/demo"}, false, "tool", "", true},
		{"multiple paths with bundle", []string{"a", "b"}, false, "", "kit", false},
		{"nothing at all", nil, false, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExportFlags(tt.args, tt.all, tt.kind, tt.bundle)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateExportFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBundleDescription(t *testing.T) {
	members := []*artifact.Artifact{
		{Frontmatter: artifact.Frontmatter{Name: "a"}},
		{Frontmatter: artifact.Frontmatter{Name: "b"}},
	}
	got := bundleDescription(members)
	want := "Bundle of 2 artifacts: a, b"
	if got != want {
		t.Errorf("bundleDescription() = %q, want %q", got, want)
	}
}

func TestResolveBulkKinds(t *testing.T) {
	if kinds, err := resolveBulkKinds(""); err != nil || kinds != nil {
		t.Errorf("resolveBulkKinds(\"\") = %v, %v, want nil, nil", kinds, err)
	}
	kinds, err := resolveBulkKinds("tool")
	if err != nil {
		t.Fatalf("resolveBulkKinds(tool) error = %v", err)
	}
	if len(kinds) != 1 || kinds[0] != artifact.KindTool {
		t.Errorf("resolveBulkKinds(tool) = %v, want [tool]", kinds)
	}
	if _, err := resolveBulkKinds("bogus"); err == nil {
		t.Fatal("resolveBulkKinds(bogus) expected error, got nil")
	}
}

func TestRunBundleExportRejectsUnsupportedTarget(t *testing.T) {
	exporter, err := targets.GetExporter("chatgpt")
	if err != nil {
		t.Fatalf("GetExporter(chatgpt) error = %v", err)
	}
	origBundle := exportBundle
	exportBundle = "kit"
	t.Cleanup(func() { exportBundle = origBundle })

	if err := runBundleExport(exporter, []string{"a", "b"}); err == nil {
		t.Fatal("runBundleExport() against a non-bundling target expected error, got nil")
	} else if !strings.Contains(err.Error(), "doesn't support bundling") {
		t.Errorf("runBundleExport() error = %v, want a bundling-not-supported message", err)
	}
}

func TestRunBundleExportRequiresName(t *testing.T) {
	exporter, err := targets.GetExporter("claude-code")
	if err != nil {
		t.Fatalf("GetExporter(claude-code) error = %v", err)
	}
	origBundle := exportBundle
	exportBundle = ""
	t.Cleanup(func() { exportBundle = origBundle })

	if err := runBundleExport(exporter, []string{"a", "b"}); err == nil {
		t.Fatal("runBundleExport() with no --bundle name expected error, got nil")
	}
}

// newBulkTestProject builds a project with one skill and one tool
// (export-ready) so bulk export has a supported and an unsupported kind to
// exercise against a skill-only target like chatgpt.
func newBulkTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	if _, err := scaffold.New(root, artifact.KindSkill, "s1", scaffold.Options{Description: "x"}); err != nil {
		t.Fatalf("scaffold.New(skill) error = %v", err)
	}
	tool, err := scaffold.New(root, artifact.KindTool, "t1", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New(tool) error = %v", err)
	}
	tool.Extra["command"] = "python3 src/main.py"
	if err := tool.Save(); err != nil {
		t.Fatalf("Save(tool) error = %v", err)
	}
	return root
}

func TestRunBulkExportSkipsUnsupportedKinds(t *testing.T) {
	root := newBulkTestProject(t)

	origProject, origTarget, origOut := projectFlag, exportTarget, exportOut
	projectFlag, exportTarget, exportOut = root, "chatgpt", t.TempDir()
	t.Cleanup(func() { projectFlag, exportTarget, exportOut = origProject, origTarget, origOut })

	exporter, err := targets.GetExporter(exportTarget)
	if err != nil {
		t.Fatalf("GetExporter(chatgpt) error = %v", err)
	}

	// chatgpt only supports skills, so the tool should be skipped, not
	// fail the run.
	if err := runBulkExport(exporter); err != nil {
		t.Fatalf("runBulkExport() error = %v, want nil (skips aren't failures)", err)
	}
}

func TestRunBulkExportAllArtifacts(t *testing.T) {
	root := newBulkTestProject(t)

	origProject, origTarget, origOut := projectFlag, exportTarget, exportOut
	projectFlag, exportTarget, exportOut = root, "claude-code", t.TempDir()
	t.Cleanup(func() { projectFlag, exportTarget, exportOut = origProject, origTarget, origOut })

	exporter, err := targets.GetExporter(exportTarget)
	if err != nil {
		t.Fatalf("GetExporter(claude-code) error = %v", err)
	}
	if err := runBulkExport(exporter); err != nil {
		t.Fatalf("runBulkExport() error = %v", err)
	}
}
