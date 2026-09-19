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
	if err := WriteManifest(path, "myproject", Owner{}, Metadata{}, entries); err != nil {
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

func readRaw(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return raw
}

// Claude Code and Copilot CLI both reject a marketplace.json with no owner
// ("owner: Required"), so one is always written -- the project name when
// the project names no author -- and there is never a "$schema", since the
// Agent Plugins spec defines none for a marketplace.
func TestWriteManifestAlwaysWritesAnOwnerAndNoSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	if err := WriteManifest(path, "myproject", Owner{}, Metadata{}, nil); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	raw := readRaw(t, path)
	owner, ok := raw["owner"].(map[string]any)
	if !ok || owner["name"] != "myproject" {
		t.Errorf("owner = %v, want an object named for the project", raw["owner"])
	}
	if _, has := raw["$schema"]; has {
		t.Error("$schema must not be written: no marketplace schema exists")
	}
	if _, has := raw["metadata"]; has {
		t.Error("an empty metadata block should be omitted")
	}
	if plugins, ok := raw["plugins"].([]any); !ok || len(plugins) != 0 {
		t.Errorf("plugins = %v, want an empty list (not null)", raw["plugins"])
	}
}

func TestWriteManifestOwnerMetadataAndVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	err := WriteManifest(path, "kit",
		Owner{Name: "Ada", Email: "ada@example.com", URL: "https://example.com"},
		Metadata{Description: "A kit.", Version: "1.2.3"},
		[]Entry{{Name: "kit", Description: "d", Version: "1.2.3", Source: "./plugins/claude-code/kit"}})
	if err != nil {
		t.Fatal(err)
	}
	raw := readRaw(t, path)
	owner := raw["owner"].(map[string]any)
	if owner["name"] != "Ada" || owner["email"] != "ada@example.com" || owner["url"] != "https://example.com" {
		t.Errorf("owner = %v", owner)
	}
	meta := raw["metadata"].(map[string]any)
	if meta["description"] != "A kit." || meta["version"] != "1.2.3" {
		t.Errorf("metadata = %v", meta)
	}
	if plugin := raw["plugins"].([]any)[0].(map[string]any); plugin["version"] != "1.2.3" {
		t.Errorf("plugin entry = %v, want its version", plugin)
	}
}

func TestWriteManifestCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".github", "plugin", "marketplace.json")
	if err := WriteManifest(path, "myproject", Owner{}, Metadata{}, nil); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
}
