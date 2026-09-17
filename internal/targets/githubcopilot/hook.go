package githubcopilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// copilotHooksDoc is a plugin's com.github.copilot/hooks/hooks.json.
// Event names accept both PascalCase ("PreToolUse") and camelCase
// ("preToolUse") per GitHub's own docs, so AgentWorks' hook `events:`
// values (written PascalCase, matching Claude Code's native casing) are
// passed straight through -- no per-vendor event-name translation needed.
type copilotHooksDoc struct {
	Version int                           `json:"version"`
	Hooks   map[string][]copilotHookEntry `json:"hooks"`
}

type copilotHookEntry struct {
	Type string `json:"type"`
	Bash string `json:"bash"`
}

// exportHook bundles a hook into an Agent Plugin: a plugin.json plus
// com.github.copilot/hooks/hooks.json, one entry per declared event, each
// running the hook's declared command via bash.
func exportHook(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	events := a.ExtraStringSlice("events")
	command := a.ExtraString("command")
	if len(events) == 0 || command == "" {
		return "", fmt.Errorf("%s needs both \"events\" and \"command\" set in its frontmatter before exporting", a.Dir)
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writePluginManifest(pluginDir, a); err != nil {
		return "", err
	}

	doc := copilotHooksDoc{Version: 1, Hooks: make(map[string][]copilotHookEntry, len(events))}
	for _, event := range events {
		doc.Hooks[event] = []copilotHookEntry{{Type: "command", Bash: command}}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding hooks.json: %w", err)
	}
	path := filepath.Join(pluginDir, "com.github.copilot", "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}
