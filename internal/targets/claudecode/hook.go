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

// claudeHookMatcher groups hook actions under an optional matcher (a tool
// name or regex; empty matches everything).
type claudeHookMatcher struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []claudeHookAction `json:"hooks"`
}

// claudeHookAction's Timeout is in seconds, the same unit as AgentWorks'.
type claudeHookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// exportHook bundles a hook into a real Claude Code plugin: a plugin.json
// plus hooks/hooks.json, one matcher entry per declared event, each
// running the hook's declared command.
func exportHook(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	doc, err := buildHooksDoc([]*artifact.Artifact{a})
	if err != nil {
		return "", err
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writeClaudePluginManifest(pluginDir, a); err != nil {
		return "", err
	}
	if err := writeHooksDoc(pluginDir, doc); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}

// buildHooksDoc accumulates one or more hook artifacts into a single
// hooks.json document. Handlers for the same event and matcher share one
// matcher block (appended, never overwritten), so bundling several hooks
// together doesn't drop all but the last one that happens to share an event.
func buildHooksDoc(hooks []*artifact.Artifact) (claudeHooksDoc, error) {
	doc := claudeHooksDoc{Hooks: map[string][]claudeHookMatcher{}}
	for _, h := range hooks {
		handlers, err := h.HookHandlers()
		if err != nil {
			return claudeHooksDoc{}, err
		}
		if len(handlers) == 0 {
			return claudeHooksDoc{}, fmt.Errorf("%s declares no hook handlers -- set \"handlers\", or \"events\" and \"command\", before exporting", h.Dir)
		}
		for _, hd := range handlers {
			action := claudeHookAction{Type: "command", Command: hd.Command, Timeout: hd.Timeout}
			blocks := doc.Hooks[hd.Event]
			placed := false
			for i := range blocks {
				if blocks[i].Matcher == hd.Matcher {
					blocks[i].Hooks = append(blocks[i].Hooks, action)
					placed = true
					break
				}
			}
			if !placed {
				blocks = append(blocks, claudeHookMatcher{Matcher: hd.Matcher, Hooks: []claudeHookAction{action}})
			}
			doc.Hooks[hd.Event] = blocks
		}
	}
	return doc, nil
}

func writeHooksDoc(pluginDir string, doc claudeHooksDoc) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding hooks.json: %w", err)
	}
	path := filepath.Join(pluginDir, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
