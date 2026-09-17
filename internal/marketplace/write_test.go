package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteManifestRoundTripsAsReadableMarketplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude-plugin", "marketplace.json")

	entries := []Entry{
		{Name: "myproject", Description: "Bundle of 2 artifacts: a, b", Source: "./plugins/claude-code/myproject"},
		{Name: "team-a", DisplayName: "Team A", Description: "Bundle of 1 artifact: c", Source: "./plugins/claude-code/team-a"},
	}
	if err := WriteManifest(path, "", "myproject", entries); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written manifest: %v", err)
	}

	var doc manifestDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written manifest doesn't parse as manifestDoc: %v", err)
	}
	if doc.Name != "myproject" {
		t.Errorf("Name = %q, want %q", doc.Name, "myproject")
	}
	if len(doc.Plugins) != 2 {
		t.Fatalf("Plugins = %d entries, want 2", len(doc.Plugins))
	}
	if !doc.Plugins[0].Source.isString || doc.Plugins[0].Source.str != "./plugins/claude-code/myproject" {
		t.Errorf("Plugins[0].Source = %+v, want a string form of ./plugins/claude-code/myproject", doc.Plugins[0].Source)
	}
	if doc.Plugins[1].displayName() != "Team A" {
		t.Errorf("Plugins[1].displayName() = %q, want %q", doc.Plugins[1].displayName(), "Team A")
	}

	// The written source should resolve exactly like any other marketplace's
	// relative-path entry (see manifest_test.go's
	// TestResolveRelativePathAgainstMarketplaceRepo) -- this is the whole
	// point: a plugin this command exports has to be installable the same
	// way any other marketplace.json's plugin is.
	m := Marketplace{Repo: "acme/myproject", Ref: "main"}
	src, err := doc.Plugins[0].resolve(m)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if src.Path != "plugins/claude-code/myproject" {
		t.Errorf("resolve() Path = %q, want %q", src.Path, "plugins/claude-code/myproject")
	}
}

func TestWriteManifestOmitsEmptySchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace.json")
	if err := WriteManifest(path, "", "myproject", nil); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if _, ok := raw["$schema"]; ok {
		t.Error("$schema present in output, want omitted when schema arg is empty")
	}
}

func TestWriteManifestIncludesSchemaWhenGiven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace.json")
	const schema = "https://agent-plugins.org/schemas/1.0.0/marketplace.schema.json"
	if err := WriteManifest(path, schema, "myproject", nil); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if raw["$schema"] != schema {
		t.Errorf("$schema = %v, want %q", raw["$schema"], schema)
	}
}

func TestWriteManifestCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".github", "plugin", "marketplace.json")
	if err := WriteManifest(path, "", "myproject", nil); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
}
