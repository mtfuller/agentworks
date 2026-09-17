package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// IsPluginDir reports whether dir looks like a Claude Code plugin -- i.e.
// it has a .claude-plugin/plugin.json.
func IsPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

// IsMarketplaceDir reports whether dir looks like a Claude Code plugin
// *marketplace* rather than a single plugin -- i.e. it has a
// .claude-plugin/marketplace.json. A repo that's a marketplace isn't a
// single importable plugin, so callers check this before IsPluginDir.
func IsMarketplaceDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	return err == nil
}

// ReadPluginManifest reads the name/description out of dir's
// .claude-plugin/plugin.json.
func ReadPluginManifest(dir string) (name, description string, err error) {
	path := filepath.Join(dir, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", path, err)
	}
	var m pluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", "", fmt.Errorf("parsing %s: %w", path, err)
	}
	return m.Name, m.Description, nil
}

// ReadAgentFile is the mirror of writeClaudeAgentFile: it parses a Claude
// Code subagent file's frontmatter + body into an artifact. Dir is left
// unset for the caller to fill in. If the frontmatter has no name (some
// hand-written agent files omit it, relying on the filename), the base
// filename becomes the name instead of failing the import outright.
func ReadAgentFile(path string) (*artifact.Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	fm, body, err := artifact.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	fm.Kind = artifact.KindAgent
	if fm.Name == "" {
		fm.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return &artifact.Artifact{Frontmatter: fm, Body: body}, nil
}
