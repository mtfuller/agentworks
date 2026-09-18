package cursor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// hooksDoc is a .cursor/hooks.json document (see cursor.com/docs/hooks).
type hooksDoc struct {
	Version int                     `json:"version"`
	Hooks   map[string][]hookAction `json:"hooks"`
}

// hookAction's Timeout is seconds (Cursor's unit and AgentWorks' alike);
// Matcher is a regex filter, which Cursor honors on only some events -- see
// Cursor's hooks docs -- and ignores elsewhere.
type hookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Matcher string `json:"matcher,omitempty"`
}

// exportHook writes outDir/.cursor/hooks.json. AgentWorks' hook artifact
// events are already vendor-shaped, not translated through a
// vendor-agnostic vocabulary (see internal/targets/claudecode/hook.go and
// AGENTS.md's note on why hook export isn't a shared package) -- a hook
// artifact targeting Cursor is expected to use Cursor's own event names
// (beforeShellExecution, afterFileEdit, preToolUse, ...), which are
// unrelated to Claude Code's or Gemini CLI's.
func exportHook(a *artifact.Artifact, outDir string) (string, error) {
	handlers, err := a.HookHandlers()
	if err != nil {
		return "", err
	}
	if len(handlers) == 0 {
		return "", fmt.Errorf("%s declares no hook handlers -- set \"handlers\", or \"events\" and \"command\", before exporting", a.Dir)
	}

	// .cursor/hooks.json is one file for every hook, so merge into an
	// existing one rather than overwrite it, skipping an identical action so
	// re-exporting the same hook doesn't duplicate it.
	path := filepath.Join(outDir, ".cursor", "hooks.json")
	doc := hooksDoc{Version: 1, Hooks: map[string][]hookAction{}}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return "", fmt.Errorf("reading existing %s: %w", path, err)
		}
		if doc.Hooks == nil {
			doc.Hooks = map[string][]hookAction{}
		}
	}
	for _, h := range handlers {
		action := hookAction{Type: "command", Command: h.Command, Timeout: h.Timeout, Matcher: h.Matcher}
		if !containsAction(doc.Hooks[h.Event], action) {
			doc.Hooks[h.Event] = append(doc.Hooks[h.Event], action)
		}
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding hooks.json: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

func containsAction(actions []hookAction, want hookAction) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}
