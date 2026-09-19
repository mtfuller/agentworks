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

// copilotHookEntry's TimeoutSec is seconds, the same unit as AgentWorks';
// Matcher is a regex filter Copilot applies to the event.
type copilotHookEntry struct {
	Type       string `json:"type"`
	Bash       string `json:"bash"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
	Matcher    string `json:"matcher,omitempty"`
}

// exportHook bundles a hook into an Agent Plugin: a plugin.json plus
// com.github.copilot/hooks/hooks.json, one entry per declared event, each
// running the hook's declared command via bash.
func exportHook(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	doc, err := buildCopilotHooksDoc([]*artifact.Artifact{a})
	if err != nil {
		return "", err
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writePluginManifest(pluginDir, a, opts.Meta); err != nil {
		return "", err
	}
	if err := writeCopilotHooksDoc(pluginDir, doc); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}

// buildCopilotHooksDoc accumulates one or more hook artifacts into a single
// hooks.json document, appending an additional entry (not overwriting) when
// more than one handler targets the same event -- so bundling several hooks
// together doesn't silently drop all but the last one that happens to share
// an event name.
func buildCopilotHooksDoc(hooks []*artifact.Artifact) (copilotHooksDoc, error) {
	doc := copilotHooksDoc{Version: 1, Hooks: map[string][]copilotHookEntry{}}
	for _, h := range hooks {
		handlers, err := h.HookHandlers()
		if err != nil {
			return copilotHooksDoc{}, err
		}
		if len(handlers) == 0 {
			return copilotHooksDoc{}, fmt.Errorf("%s declares no hook handlers -- set \"handlers\", or \"events\" and \"command\", before exporting", h.Dir)
		}
		for _, hd := range handlers {
			doc.Hooks[hd.Event] = append(doc.Hooks[hd.Event], copilotHookEntry{
				Type: "command", Bash: hd.Command, TimeoutSec: hd.Timeout, Matcher: hd.Matcher,
			})
		}
	}
	return doc, nil
}

func writeCopilotHooksDoc(pluginDir string, doc copilotHooksDoc) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding hooks.json: %w", err)
	}
	path := filepath.Join(pluginDir, "com.github.copilot", "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
