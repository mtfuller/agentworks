package geminicli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// settingsFragment is a .gemini/settings.json fragment containing only the
// "hooks" key (see geminicli.com/docs/hooks/reference/) -- a real
// settings.json holds many unrelated settings AgentWorks has no business
// generating, so this is meant to be merged in by hand, not dropped in as a
// complete file.
type settingsFragment struct {
	Hooks map[string][]hookGroup `json:"hooks"`
}

// hookGroup is one entry in an event's array: a set of hook configs that
// all run together. AgentWorks' hook model has no matcher concept, so
// "matcher" is left unset (matches every occurrence of the event).
type hookGroup struct {
	Hooks []hookAction `json:"hooks"`
}

type hookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// exportHook writes the outDir/.gemini/settings.json fragment described
// above. AgentWorks' hook artifact events are already vendor-shaped, not
// translated through a vendor-agnostic vocabulary (see
// internal/targets/claudecode/hook.go and AGENTS.md's note on why hook
// export isn't a shared package) -- a hook artifact targeting Gemini CLI is
// expected to use Gemini CLI's own event names (BeforeTool, AfterTool,
// SessionStart, ...), which are unrelated to Claude Code's or Cursor's.
func exportHook(a *artifact.Artifact, outDir string) (string, error) {
	events := a.ExtraStringSlice("events")
	command := a.ExtraString("command")
	if len(events) == 0 || command == "" {
		return "", fmt.Errorf("%s needs both \"events\" and \"command\" set in its frontmatter before exporting", a.Dir)
	}

	frag := settingsFragment{Hooks: map[string][]hookGroup{}}
	for _, event := range events {
		frag.Hooks[event] = append(frag.Hooks[event], hookGroup{
			Hooks: []hookAction{{Type: "command", Command: command}},
		})
	}

	data, err := json.MarshalIndent(frag, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding settings.json fragment: %w", err)
	}

	dir := filepath.Join(outDir, ".gemini")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
