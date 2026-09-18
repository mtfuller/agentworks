package geminicli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

// manifest is a gemini-extension.json manifest (see
// github.com/google-gemini/gemini-cli/blob/main/docs/extensions/reference.md).
// Only the fields AgentWorks has a real value for are populated; the rest
// (contextFileName, excludeTools, settings, themes, ...) are left for a
// project to hand-edit if it needs them.
type manifest struct {
	Name        string                      `json:"name"`
	Version     string                      `json:"version,omitempty"`
	Description string                      `json:"description,omitempty"`
	MCPServers  map[string]mcpconfig.Server `json:"mcpServers,omitempty"`
}

// writeManifest writes extDir/gemini-extension.json.
func writeManifest(extDir string, m manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding gemini-extension.json: %w", err)
	}
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", extDir, err)
	}
	path := filepath.Join(extDir, "gemini-extension.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
