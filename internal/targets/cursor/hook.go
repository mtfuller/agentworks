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

type hookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// exportHook writes outDir/.cursor/hooks.json. AgentWorks' hook artifact
// events are already vendor-shaped, not translated through a
// vendor-agnostic vocabulary (see internal/targets/claudecode/hook.go and
// AGENTS.md's note on why hook export isn't a shared package) -- a hook
// artifact targeting Cursor is expected to use Cursor's own event names
// (beforeShellExecution, afterFileEdit, preToolUse, ...), which are
// unrelated to Claude Code's or Gemini CLI's.
func exportHook(a *artifact.Artifact, outDir string) (string, error) {
	events := a.ExtraStringSlice("events")
	command := a.ExtraString("command")
	if len(events) == 0 || command == "" {
		return "", fmt.Errorf("%s needs both \"events\" and \"command\" set in its frontmatter before exporting", a.Dir)
	}

	doc := hooksDoc{Version: 1, Hooks: map[string][]hookAction{}}
	for _, event := range events {
		doc.Hooks[event] = append(doc.Hooks[event], hookAction{Type: "command", Command: command})
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding hooks.json: %w", err)
	}

	dir := filepath.Join(outDir, ".cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
