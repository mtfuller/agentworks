package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// claudeHooksDoc is a plugin's hooks/hooks.json: a map from event name
// (e.g. "PreToolUse", "SessionStart" -- Claude Code has ~30 documented
// lifecycle events) to the matchers that run on it.
type claudeHooksDoc struct {
	Hooks map[string][]claudeHookMatcher `json:"hooks"`
}

// claudeHookMatcher groups hook actions under an (optional, unused here --
// AgentWorks' hook model has no matcher concept yet) tool/file matcher.
type claudeHookMatcher struct {
	Hooks []claudeHookAction `json:"hooks"`
}

type claudeHookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// exportHook bundles a hook into a real Claude Code plugin: a plugin.json
// plus hooks/hooks.json, one matcher entry per declared event, each
// running the hook's declared command.
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
	if err := writeClaudePluginManifest(pluginDir, a); err != nil {
		return "", err
	}

	doc := claudeHooksDoc{Hooks: make(map[string][]claudeHookMatcher, len(events))}
	for _, event := range events {
		doc.Hooks[event] = []claudeHookMatcher{
			{Hooks: []claudeHookAction{{Type: "command", Command: command}}},
		}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding hooks.json: %w", err)
	}
	path := filepath.Join(pluginDir, "hooks", "hooks.json")
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
