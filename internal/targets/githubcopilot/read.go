package githubcopilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mtfuller/agentworks/internal/targets/claudecode"
)

// This file is the reverse of the Agent Plugin exporters: it reads what a
// GitHub Copilot plugin declares, so `agentworks add` can import it. MCP
// servers live in mcp.json (read by claudecode.ReadPluginMCPServers, which
// accepts the same shape), skills in skills/<name>/SKILL.md (as for any
// plugin); what's Copilot-specific is the manifest location, the agent files,
// and the hooks file.

// manifestLocations are where an Agent Plugin's plugin.json may live, in
// preference order. A Claude Code plugin's .claude-plugin/plugin.json is
// deliberately not one: claudecode.IsPluginDir owns that layout.
var manifestLocations = []string{
	"plugin.json",
	filepath.Join(".github", "plugin", "plugin.json"),
}

func manifestPath(dir string) string {
	for _, rel := range manifestLocations {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// IsPluginDir reports whether dir looks like a GitHub Copilot (Agent Plugins)
// plugin: it has a plugin.json at its root or under .github/plugin/.
func IsPluginDir(dir string) bool { return manifestPath(dir) != "" }

// IsMarketplaceDir reports whether dir is a Copilot plugin *marketplace*
// rather than a single plugin.
func IsMarketplaceDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".github", "plugin", "marketplace.json"))
	return err == nil
}

// ReadPluginManifest reads the name and description from a plugin's
// plugin.json.
func ReadPluginManifest(dir string) (name, description string, err error) {
	path := manifestPath(dir)
	if path == "" {
		return "", "", fmt.Errorf("%s has no plugin.json", dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", path, err)
	}
	var m struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", "", fmt.Errorf("parsing %s: %w", path, err)
	}
	return m.Name, m.Description, nil
}

// AgentFiles lists the plugin's custom-agent files: <name>.agent.md under
// com.github.copilot/agents/ (where AgentWorks exports them) or agents/, and
// plain agents/<name>.md.
func AgentFiles(dir string) ([]string, error) {
	var out []string
	for _, pattern := range []string{
		filepath.Join("com.github.copilot", "agents", "*.agent.md"),
		filepath.Join("agents", "*.agent.md"),
		filepath.Join("agents", "*.md"),
	} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, err
		}
		out = append(out, matches...)
	}
	sort.Strings(out)
	// agents/*.md also matches agents/*.agent.md; keep each file once.
	uniq := out[:0]
	for i, p := range out {
		if i == 0 || p != out[i-1] {
			uniq = append(uniq, p)
		}
	}
	return uniq, nil
}

// hookFileLocations are where a plugin's hooks.json may live.
var hookFileLocations = []string{
	filepath.Join("com.github.copilot", "hooks", "hooks.json"),
	filepath.Join("hooks", "hooks.json"),
	"hooks.json",
}

// ReadPluginHooks returns the command hooks the plugin declares. Copilot's
// hooks file is flat: {"hooks": {Event: [{type, bash|command, timeoutSec,
// matcher}]}}. An entry with no shell command (only `powershell` or `exec`) is
// returned as unsupported rather than guessed at.
func ReadPluginHooks(dir string) ([]claudecode.PluginHook, error) {
	var out []claudecode.PluginHook
	for _, rel := range hookFileLocations {
		path := filepath.Join(dir, rel)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var doc struct {
			Hooks map[string][]map[string]any `json:"hooks"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		events := make([]string, 0, len(doc.Hooks))
		for e := range doc.Hooks {
			events = append(events, e)
		}
		sort.Strings(events)
		for _, event := range events {
			for _, entry := range doc.Hooks[event] {
				out = append(out, parseCopilotHook(event, entry))
			}
		}
	}
	return out, nil
}

func parseCopilotHook(event string, entry map[string]any) claudecode.PluginHook {
	h := claudecode.PluginHook{Event: event}
	h.Matcher, _ = entry["matcher"].(string)
	for _, key := range []string{"timeoutSec", "timeout"} {
		if n, ok := entry[key].(float64); ok && n > 0 {
			h.Timeout = int(n + 0.999999)
			break
		}
	}
	if typ, _ := entry["type"].(string); typ != "" && typ != "command" {
		h.Unsupported = fmt.Sprintf("%s handler for %s (only command handlers are supported)", typ, event)
		return h
	}
	for _, key := range []string{"bash", "command"} {
		if c, _ := entry[key].(string); strings.TrimSpace(c) != "" {
			h.Command = c
			return h
		}
	}
	h.Unsupported = fmt.Sprintf("%s handler has no bash or command entry (a powershell- or exec-only hook can't be imported)", event)
	return h
}
