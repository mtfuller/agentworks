package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Entry is one plugin listing to write into a marketplace.json this project
// publishes -- the write-side counterpart to manifestEntry, which only
// reads other projects' listings.
type Entry struct {
	Name        string
	DisplayName string
	Description string
	// Source is a plugin directory's path relative to the repo root (the
	// same convention manifestEntry.resolve reads back: a bare string is
	// resolved against the marketplace's own repo, not against
	// marketplace.json's own directory).
	Source string
}

// WriteManifest writes a marketplace.json at path listing entries under
// name -- the repo-publishing counterpart to this package's read side
// (Marketplace/WellKnown), used by `agentworks marketplace` to make a
// project's exported plugins installable straight from the repo the
// project lives in. schema, if non-empty, is written as the doc's
// top-level "$schema" field.
func WriteManifest(path, schema, name string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}

	out := struct {
		Schema  string       `json:"$schema,omitempty"`
		Name    string       `json:"name"`
		Plugins []writeEntry `json:"plugins"`
	}{
		Schema:  schema,
		Name:    name,
		Plugins: make([]writeEntry, len(entries)),
	}
	for i, e := range entries {
		out.Plugins[i] = writeEntry{
			Name:        e.Name,
			DisplayName: e.DisplayName,
			Description: e.Description,
			Source:      e.Source,
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// writeEntry is manifestEntry's write-side mirror: a plain string Source
// instead of manifestEntry's read-side rawSource (which also accepts a
// {"source": "github"|"url"|..., ...} object) -- WriteManifest only ever
// produces the relative-path form, since every plugin it lists lives in
// this same repo.
type writeEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}
