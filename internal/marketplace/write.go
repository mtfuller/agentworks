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
	Version     string
	// Source is a plugin directory's path relative to the repo root (the
	// same convention manifestEntry.resolve reads back: a bare string is
	// resolved against the marketplace's own repo, not against
	// marketplace.json's own directory).
	Source string
}

// Owner is who publishes a marketplace. Both Claude Code and Copilot CLI
// reject a marketplace.json without one ("owner: Required"), so it is always
// written; Name is its one required field.
type Owner struct {
	Name  string
	Email string
	URL   string
}

// Metadata is the optional descriptive block of a marketplace.json.
type Metadata struct {
	Description string
	Version     string
}

// WriteManifest writes a marketplace.json at path listing entries under
// name -- the repo-publishing counterpart to this package's read side
// (Marketplace/WellKnown), used by `agentworks marketplace` to make a
// project's exported plugins installable straight from the repo the
// project lives in. The shape is the one Claude Code and Copilot CLI both
// require and read: name, owner, optional metadata, and plugins. There is no
// "$schema": the Agent Plugins spec defines none for a marketplace, and the
// URL an earlier version of this wrote there does not exist.
func WriteManifest(path, name string, owner Owner, meta Metadata, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if owner.Name == "" {
		owner.Name = name
	}

	out := writeDoc{
		Name:    name,
		Owner:   writeOwner{Name: owner.Name, Email: owner.Email, URL: owner.URL},
		Plugins: make([]writeEntry, len(entries)),
	}
	if meta.Description != "" || meta.Version != "" {
		out.Metadata = &writeMetadata{Description: meta.Description, Version: meta.Version}
	}
	for i, e := range entries {
		out.Plugins[i] = writeEntry{
			Name:        e.Name,
			DisplayName: e.DisplayName,
			Description: e.Description,
			Version:     e.Version,
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

type writeDoc struct {
	Name     string         `json:"name"`
	Owner    writeOwner     `json:"owner"`
	Metadata *writeMetadata `json:"metadata,omitempty"`
	Plugins  []writeEntry   `json:"plugins"`
}

type writeOwner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

type writeMetadata struct {
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
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
	Version     string `json:"version,omitempty"`
	Source      string `json:"source"`
}
