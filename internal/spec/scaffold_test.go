package spec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
)

func TestInitCreatesResolvableRuntimeProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "notes-agents")
	if err := spec.Init(root, "notes-agents"); err != nil {
		t.Fatal(err)
	}
	plan, err := resolver.ResolveTeam(root, "default", resolver.Options{AvailableProviders: []spec.Provider{spec.ProviderHost}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Project != "notes-agents" || len(plan.Agents) != 1 || len(plan.Skills) != 1 || len(plan.Tools) != 0 {
		t.Fatalf("plan=%#v", plan)
	}
	if _, err := os.Stat(filepath.Join(root, spec.LocalFile)); !os.IsNotExist(err) {
		t.Fatalf("machine-local binding should not be created: %v", err)
	}
	if err := spec.Init(root, "again"); err == nil {
		t.Fatal("second initialization overwrote the project")
	}
}

func TestInitRequiresName(t *testing.T) {
	if err := spec.Init(t.TempDir(), "  "); err == nil {
		t.Fatal("blank project name accepted")
	}
}

func TestInitRejectsInvalidNameBeforeWriting(t *testing.T) {
	root := t.TempDir()
	if err := spec.Init(root, "Not YAML:\nunsafe"); err == nil {
		t.Fatal("invalid project name accepted")
	}
	if _, err := os.Stat(filepath.Join(root, spec.ProjectFile)); !os.IsNotExist(err) {
		t.Fatalf("invalid project wrote a manifest: %v", err)
	}
}
