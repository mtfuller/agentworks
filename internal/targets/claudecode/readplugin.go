package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file is the reverse of mcp/hook export: it reads the MCP servers and
// hook handlers a Claude Code plugin declares, wherever it declares them.
// Plugins may put either in a standalone file (.mcp.json, hooks/hooks.json)
// or inline in .claude-plugin/plugin.json, and plugin.json may instead point
// at a file by path -- all of which are merged, as Claude Code merges them.

// PluginMCPServer is one entry of a plugin's mcpServers map, reduced to what
// AgentWorks models. Ignored lists keys the entry had that AgentWorks doesn't
// model (e.g. "cwd"), so the importer can say what it dropped.
type PluginMCPServer struct {
	Name    string
	Type    string // "stdio", "http", or "sse"; inferred when the entry omits it
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	Ignored []string
}

// ReadPluginMCPServers returns every MCP server the plugin at dir declares,
// sorted by name. Sources, in merge order: .mcp.json, then plugin.json's
// "mcpServers" (a path, a list of paths, or an inline object). A later source
// overrides an earlier one of the same name.
func ReadPluginMCPServers(dir string) ([]PluginMCPServer, error) {
	servers := map[string]PluginMCPServer{}
	merge := func(raw map[string]any, from string) error {
		for name, v := range raw {
			entry, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: MCP server %q is not an object", from, name)
			}
			servers[name] = parseMCPServer(name, entry)
		}
		return nil
	}

	if raw, err := readJSONObject(filepath.Join(dir, ".mcp.json")); err != nil {
		return nil, err
	} else if raw != nil {
		if err := merge(mcpServersOf(raw), ".mcp.json"); err != nil {
			return nil, err
		}
	}

	manifest, err := readJSONObject(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, err
	}
	for _, source := range manifestSources(manifest["mcpServers"]) {
		raw, from, err := resolveSource(dir, source)
		if err != nil {
			return nil, err
		}
		if err := merge(mcpServersOf(raw), from); err != nil {
			return nil, err
		}
	}

	out := make([]PluginMCPServer, 0, len(servers))
	for _, s := range servers {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// mcpServersOf unwraps {"mcpServers": {...}}; a file with the servers at its
// root is also accepted, since plugin.json's path form and older plugins do
// that. It is treated as a bare map only if every value looks like a server.
func mcpServersOf(raw map[string]any) map[string]any {
	if inner, ok := raw["mcpServers"].(map[string]any); ok {
		return inner
	}
	for _, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		if _, hasCmd := entry["command"]; !hasCmd {
			if _, hasURL := entry["url"]; !hasURL {
				return nil
			}
		}
	}
	return raw
}

var knownMCPKeys = map[string]bool{"type": true, "command": true, "args": true, "env": true, "url": true, "headers": true}

func parseMCPServer(name string, entry map[string]any) PluginMCPServer {
	s := PluginMCPServer{
		Name:    name,
		Type:    stringOf(entry["type"]),
		Command: stringOf(entry["command"]),
		URL:     stringOf(entry["url"]),
		Args:    stringsOf(entry["args"]),
		Env:     stringMapOf(entry["env"]),
		Headers: stringMapOf(entry["headers"]),
	}
	switch {
	case s.Type == "streamable-http":
		s.Type = "http"
	case s.Type == "" && s.URL != "":
		s.Type = "http" // a bare url is a remote server
	case s.Type == "" || s.Type == "local":
		s.Type = "stdio"
	}
	for k := range entry {
		if !knownMCPKeys[k] {
			s.Ignored = append(s.Ignored, k)
		}
	}
	sort.Strings(s.Ignored)
	return s
}

// PluginHook is one hook handler declared by a plugin. Unsupported is
// non-empty for a handler AgentWorks can't represent faithfully (a non-command
// type, or a group with an `if` condition -- importing it without the
// condition would make it fire more broadly than the author intended); its
// text says why.
type PluginHook struct {
	Event       string
	Matcher     string
	Command     string
	Timeout     int // seconds; 0 = unset
	Unsupported string
}

// ReadPluginHooks returns every hook handler the plugin at dir declares, in
// file order. Sources, in merge order: hooks/hooks.json, then plugin.json's
// "hooks" (a path, a list of paths, or an inline event map).
func ReadPluginHooks(dir string) ([]PluginHook, error) {
	var out []PluginHook
	add := func(raw map[string]any, from string) error {
		hooks, err := parseHooks(raw, from)
		if err != nil {
			return err
		}
		out = append(out, hooks...)
		return nil
	}

	if raw, err := readJSONObject(filepath.Join(dir, "hooks", "hooks.json")); err != nil {
		return nil, err
	} else if raw != nil {
		if err := add(raw, "hooks/hooks.json"); err != nil {
			return nil, err
		}
	}

	manifest, err := readJSONObject(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, err
	}
	for _, source := range manifestSources(manifest["hooks"]) {
		raw, from, err := resolveSource(dir, source)
		if err != nil {
			return nil, err
		}
		if err := add(raw, from); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// parseHooks reads {"hooks": {Event: [{matcher, if, hooks: [...]}]}}; the
// event map at the root (plugin.json's inline form) is accepted too.
func parseHooks(raw map[string]any, from string) ([]PluginHook, error) {
	events, ok := raw["hooks"].(map[string]any)
	if !ok {
		events = raw
	}

	names := make([]string, 0, len(events))
	for e := range events {
		names = append(names, e)
	}
	sort.Strings(names)

	var out []PluginHook
	for _, event := range names {
		groups, ok := events[event].([]any)
		if !ok {
			continue // e.g. a top-level "description" string
		}
		for _, g := range groups {
			group, ok := g.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: %s has a hook group that is not an object", from, event)
			}
			matcher := stringOf(group["matcher"])
			_, hasIf := group["if"]
			handlers, _ := group["hooks"].([]any)
			for _, h := range handlers {
				handler, ok := h.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: %s has a handler that is not an object", from, event)
				}
				ph := PluginHook{
					Event:   event,
					Matcher: matcher,
					Command: stringOf(handler["command"]),
					Timeout: parseTimeoutSeconds(handler["timeout"]),
				}
				switch typ := stringOf(handler["type"]); {
				case typ != "" && typ != "command":
					ph.Unsupported = fmt.Sprintf("%s handler for %s (only command handlers are supported)", typ, event)
				case hasIf:
					ph.Unsupported = fmt.Sprintf("%s handler has an `if` condition, which AgentWorks can't express", event)
				case ph.Command == "":
					ph.Unsupported = fmt.Sprintf("%s handler has no command", event)
				}
				out = append(out, ph)
			}
		}
	}
	return out, nil
}

var timeoutPattern = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*(ms|s|m)?\s*$`)

// parseTimeoutSeconds accepts a number (seconds) or a string like "30s",
// "2m", or "1500ms", rounding up to whole seconds; anything else is 0.
func parseTimeoutSeconds(v any) int {
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return int(t + 0.999999)
		}
	case string:
		m := timeoutPattern.FindStringSubmatch(t)
		if m == nil {
			return 0
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0
		}
		d := time.Duration(n * float64(time.Second))
		switch m[2] {
		case "ms":
			d = time.Duration(n * float64(time.Millisecond))
		case "m":
			d = time.Duration(n * float64(time.Minute))
		}
		return int((d + time.Second - 1) / time.Second)
	}
	return 0
}

// manifestSources normalizes plugin.json's "mcpServers"/"hooks" value -- a
// path string, a list of them, or an inline object -- to a list of sources,
// each either a path (string) or an inline object (map).
func manifestSources(v any) []any {
	switch t := v.(type) {
	case string:
		return []any{t}
	case map[string]any:
		return []any{t}
	case []any:
		return t
	}
	return nil
}

// resolveSource loads one manifestSources entry: an inline object as-is, or a
// file path relative to the plugin root (never escaping it).
func resolveSource(dir string, source any) (map[string]any, string, error) {
	switch s := source.(type) {
	case map[string]any:
		return s, "plugin.json", nil
	case string:
		rel := filepath.Clean(strings.TrimPrefix(s, "./"))
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, "", fmt.Errorf("plugin.json points at %q, outside the plugin", s)
		}
		raw, err := readJSONObject(filepath.Join(dir, rel))
		if err != nil {
			return nil, "", err
		}
		if raw == nil {
			return nil, "", fmt.Errorf("plugin.json points at %q, which doesn't exist", s)
		}
		return raw, rel, nil
	}
	return nil, "", fmt.Errorf("plugin.json has an unrecognized source entry of type %T", source)
}

// readJSONObject reads path as a JSON object, or returns (nil, nil) if the
// file doesn't exist.
func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return out, nil
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func stringsOf(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func stringMapOf(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, item := range m {
		if s, ok := item.(string); ok {
			out[k] = s
		}
	}
	return out
}
