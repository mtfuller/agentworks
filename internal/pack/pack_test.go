package pack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
)

func TestCreateIsDeterministicAndSharesDependencies(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	firstPath := filepath.Join(t.TempDir(), "first.agentworks")
	secondPath := filepath.Join(t.TempDir(), "second.agentworks")
	first, err := Create(root, "engineering", firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Create(root, "engineering", secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.ArchiveDigest != second.ArchiveDigest || first.ContentDigest != second.ContentDigest {
		t.Fatalf("pack changed without source changes: %#v %#v", first, second)
	}
	firstBytes, _ := os.ReadFile(firstPath)
	secondBytes, _ := os.ReadFile(secondPath)
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("archives are not byte-for-byte deterministic")
	}
	reader, err := zip.OpenReader(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	counts := map[string]int{}
	for _, file := range reader.File {
		counts[file.Name]++
	}
	if counts["skills/testing/SKILL.md"] != 1 {
		t.Fatalf("shared skill entries=%d", counts["skills/testing/SKILL.md"])
	}
	if counts["agentworks.local.yaml"] != 0 {
		t.Fatal("machine-local workspace bindings entered pack")
	}
}

func TestInstallVerifiesAndReresolvesProvider(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	archive := filepath.Join(t.TempDir(), "team.agentworks")
	created, err := Create(root, "engineering", archive)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "installed")
	manifest, err := Install(archive, destination)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ContentDigest != created.ContentDigest {
		t.Fatalf("installed digest=%q want %q", manifest.ContentDigest, created.ContentDigest)
	}
	plan, err := resolver.ResolveTeam(destination, "engineering", resolver.Options{AvailableProviders: []spec.Provider{spec.ProviderContainer}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tools) != 1 || plan.Tools[0].SelectedProvider != spec.ProviderContainer {
		t.Fatalf("container resolution=%#v", plan.Tools)
	}
	if _, err := os.Stat(filepath.Join(destination, "routes", "jira-assigned", "route.yaml")); err != nil {
		t.Fatalf("team route not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "sources", "jira-main", "source.yaml")); err != nil {
		t.Fatalf("route source not installed: %v", err)
	}
	if _, err := Install(archive, destination); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second install error=%v", err)
	}
}

func TestVerifyRejectsModifiedContent(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	archive := filepath.Join(t.TempDir(), "team.agentworks")
	if _, err := Create(root, "engineering", archive); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "installed")
	if _, err := Install(archive, destination); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination, "skills", "testing", "SKILL.md")
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(destination); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("Verify() error=%v", err)
	}
}

func TestVerifyRejectsSymlinkedContent(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "examples", "agent-team"))
	archive := filepath.Join(t.TempDir(), "team.agentworks")
	if _, err := Create(root, "engineering", archive); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "installed")
	if _, err := Install(archive, destination); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(destination, "skills", "testing", "SKILL.md")
	outside := filepath.Join(t.TempDir(), "outside.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Verify(destination); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("Verify() error=%v", err)
	}
}
