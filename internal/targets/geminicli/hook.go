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
// all run together under one matcher (a regex for tool events, an exact
// string for lifecycle events; empty matches every occurrence).
type hookGroup struct {
	Matcher string       `json:"matcher,omitempty"`
	Hooks   []hookAction `json:"hooks"`
}

// hookAction's Timeout is milliseconds -- Gemini CLI's unit, unlike
// AgentWorks' seconds (see toMillis).
type hookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// exportHook writes the outDir/.gemini/settings.json fragment described
// above. AgentWorks' hook artifact events are already vendor-shaped, not
// translated through a vendor-agnostic vocabulary (see
// internal/targets/claudecode/hook.go and AGENTS.md's note on why hook
// export isn't a shared package) -- a hook artifact targeting Gemini CLI is
// expected to use Gemini CLI's own event names (BeforeTool, AfterTool,
// SessionStart, ...), which are unrelated to Claude Code's or Cursor's.
func exportHook(a *artifact.Artifact, outDir string) (string, error) {
	handlers, err := a.HookHandlers()
	if err != nil {
		return "", err
	}
	if len(handlers) == 0 {
		return "", fmt.Errorf("%s declares no hook handlers -- set \"handlers\", or \"events\" and \"command\", before exporting", a.Dir)
	}

	// The fragment is one file for every hook, so merge into an existing one
	// rather than overwrite it, skipping an identical group so re-exporting
	// the same hook doesn't duplicate it.
	path := filepath.Join(outDir, ".gemini", "settings.json")
	frag := settingsFragment{Hooks: map[string][]hookGroup{}}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &frag); err != nil {
			return "", fmt.Errorf("reading existing %s: %w", path, err)
		}
		if frag.Hooks == nil {
			frag.Hooks = map[string][]hookGroup{}
		}
	}
	for _, h := range handlers {
		group := hookGroup{
			Matcher: h.Matcher,
			Hooks:   []hookAction{{Type: "command", Command: h.Command, Timeout: toMillis(h.Timeout)}},
		}
		if !containsGroup(frag.Hooks[h.Event], group) {
			frag.Hooks[h.Event] = append(frag.Hooks[h.Event], group)
		}
	}

	data, err := json.MarshalIndent(frag, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding settings.json fragment: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// toMillis converts AgentWorks' seconds to Gemini CLI's milliseconds; zero
// stays zero (omitted, so Gemini's own default applies).
func toMillis(seconds int) int { return seconds * 1000 }

func containsGroup(groups []hookGroup, want hookGroup) bool {
	for _, g := range groups {
		if g.Matcher == want.Matcher && len(g.Hooks) == len(want.Hooks) && len(g.Hooks) == 1 && g.Hooks[0] == want.Hooks[0] {
			return true
		}
	}
	return false
}
