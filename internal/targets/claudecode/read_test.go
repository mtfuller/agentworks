package claudecode

import (
	"github.com/mtfuller/agentworks/internal/targets"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/scaffold"
)

func TestReadAgentFileRoundTrip(t *testing.T) {
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindAgent, "researcher", scaffold.Options{Description: "Researches things."})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "researcher.md")
	if err := writeClaudeAgentFile(path, a); err != nil {
		t.Fatalf("writeClaudeAgentFile() error = %v", err)
	}

	got, err := ReadAgentFile(path)
	if err != nil {
		t.Fatalf("ReadAgentFile() error = %v", err)
	}
	if got.Kind != artifact.KindAgent {
		t.Errorf("Kind = %q, want agent", got.Kind)
	}
	if got.Name != a.Name {
		t.Errorf("Name = %q, want %q", got.Name, a.Name)
	}
	if got.Description != a.Description {
		t.Errorf("Description = %q, want %q", got.Description, a.Description)
	}
	if got.Body != a.Body {
		t.Errorf("Body = %q, want %q", got.Body, a.Body)
	}
}

func TestReadAgentFileDerivesNameFromFilename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviewer.md")
	content := "---\ndescription: Reviews things.\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing agent file: %v", err)
	}

	got, err := ReadAgentFile(path)
	if err != nil {
		t.Fatalf("ReadAgentFile() error = %v", err)
	}
	if got.Name != "reviewer" {
		t.Errorf("Name = %q, want reviewer (derived from filename)", got.Name)
	}
}

func TestReadAgentFileMissing(t *testing.T) {
	if _, err := ReadAgentFile(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Fatal("ReadAgentFile() of a missing file expected error, got nil")
	}
}

func TestReadPluginManifest(t *testing.T) {
	pluginDir := t.TempDir()
	if err := writeClaudePluginManifestNamed(pluginDir, "demo-kit", "A demo kit.", "0.1.0", targets.PluginMeta{}); err != nil {
		t.Fatalf("writeClaudePluginManifestNamed() error = %v", err)
	}

	name, description, err := ReadPluginManifest(pluginDir)
	if err != nil {
		t.Fatalf("ReadPluginManifest() error = %v", err)
	}
	if name != "demo-kit" {
		t.Errorf("name = %q, want demo-kit", name)
	}
	if description != "A demo kit." {
		t.Errorf("description = %q, want %q", description, "A demo kit.")
	}
}

func TestReadPluginManifestMissing(t *testing.T) {
	if _, _, err := ReadPluginManifest(t.TempDir()); err == nil {
		t.Fatal("ReadPluginManifest() of a directory with no plugin.json expected error, got nil")
	}
}

func TestIsPluginDir(t *testing.T) {
	dir := t.TempDir()
	if IsPluginDir(dir) {
		t.Error("IsPluginDir() = true for an empty directory, want false")
	}
	if err := writeClaudePluginManifestNamed(dir, "demo", "x", "0.1.0", targets.PluginMeta{}); err != nil {
		t.Fatalf("writeClaudePluginManifestNamed() error = %v", err)
	}
	if !IsPluginDir(dir) {
		t.Error("IsPluginDir() = false for a directory with plugin.json, want true")
	}
}

func TestIsMarketplaceDir(t *testing.T) {
	dir := t.TempDir()
	if IsMarketplaceDir(dir) {
		t.Error("IsMarketplaceDir() = true for an empty directory, want false")
	}
	marketplacePath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	if err := os.MkdirAll(filepath.Dir(marketplacePath), 0o755); err != nil {
		t.Fatalf("mkdir .claude-plugin: %v", err)
	}
	if err := os.WriteFile(marketplacePath, []byte(`{"name":"x","owner":{"name":"y"},"plugins":[]}`), 0o644); err != nil {
		t.Fatalf("writing marketplace.json: %v", err)
	}
	if !IsMarketplaceDir(dir) {
		t.Error("IsMarketplaceDir() = false for a directory with marketplace.json, want true")
	}
	// A plugin dir is not also a marketplace dir, and vice versa.
	if IsPluginDir(dir) {
		t.Error("IsPluginDir() = true for a marketplace-only directory, want false")
	}
}
