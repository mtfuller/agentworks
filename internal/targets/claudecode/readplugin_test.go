package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadPluginMCPServersMergesEverySource(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".mcp.json", `{"mcpServers":{"a":{"command":"one"},"shared":{"command":"from-mcp-json"}}}`)
	write(t, dir, "mcp.json", `{"$schema":"x","mcpServers":{"b":{"type":"http","url":"https://x.example/mcp"}}}`)
	write(t, dir, ".claude-plugin/plugin.json", `{"name":"p","mcpServers":["./extra.json",{"inline":{"command":"inl","args":["-y","pkg"],"env":{"K":"v"}}}]}`)
	write(t, dir, "extra.json", `{"shared":{"command":"from-extra"},"cwdy":{"command":"c","cwd":"/tmp"}}`)

	servers, err := ReadPluginMCPServers(dir)
	if err != nil {
		t.Fatalf("ReadPluginMCPServers() error = %v", err)
	}
	byName := map[string]PluginMCPServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	for _, name := range []string{"a", "b", "shared", "inline", "cwdy"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing server %q; got %v", name, byName)
		}
	}
	if byName["shared"].Command != "from-extra" {
		t.Errorf("a later source should override an earlier one of the same name, got %q", byName["shared"].Command)
	}
	if b := byName["b"]; b.Type != "http" || b.URL != "https://x.example/mcp" {
		t.Errorf("remote server = %+v", b)
	}
	if inl := byName["inline"]; inl.Type != "stdio" || len(inl.Args) != 2 || inl.Env["K"] != "v" {
		t.Errorf("inline server = %+v (a missing type is stdio)", inl)
	}
	if got := byName["cwdy"].Ignored; len(got) != 1 || got[0] != "cwd" {
		t.Errorf("Ignored = %v, want the unmodelled cwd field", got)
	}
	// Sorted by name.
	for i := 1; i < len(servers); i++ {
		if servers[i-1].Name > servers[i].Name {
			t.Errorf("servers not sorted: %v", servers)
		}
	}
}

func TestReadPluginMCPServersTypeInference(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".mcp.json", `{"mcpServers":{
		"bare-url":{"url":"https://a.example/mcp"},
		"streamable":{"type":"streamable-http","url":"https://b.example/mcp"},
		"local":{"type":"local","command":"x"},
		"sse":{"type":"sse","url":"https://c.example/sse"}
	}}`)
	servers, _ := ReadPluginMCPServers(dir)
	got := map[string]string{}
	for _, s := range servers {
		got[s.Name] = s.Type
	}
	want := map[string]string{"bare-url": "http", "streamable": "http", "local": "stdio", "sse": "sse"}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("%s type = %q, want %q", name, got[name], typ)
		}
	}
}

func TestReadPluginMCPServersAcceptsServersAtTheRoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".mcp.json", `{"only":{"command":"x"},"other":{"url":"https://y.example"}}`)
	servers, err := ReadPluginMCPServers(dir)
	if err != nil || len(servers) != 2 {
		t.Errorf("a file with servers at its root = %v, %v", servers, err)
	}
	// A root that isn't servers at all is not mistaken for them.
	write(t, dir, ".mcp.json", `{"version":1,"note":"hi"}`)
	if servers, _ := ReadPluginMCPServers(dir); len(servers) != 0 {
		t.Errorf("unrelated root keys became servers: %v", servers)
	}
}

func TestReadPluginMCPServersErrors(t *testing.T) {
	tests := map[string]struct {
		files   map[string]string
		wantErr string
	}{
		"invalid json":         {map[string]string{".mcp.json": `{nope`}, "parsing"},
		"server not an object": {map[string]string{".mcp.json": `{"mcpServers":{"a":"str"}}`}, "not an object"},
		"path escapes plugin":  {map[string]string{".claude-plugin/plugin.json": `{"mcpServers":"../../etc/x.json"}`}, "outside the plugin"},
		"absolute path":        {map[string]string{".claude-plugin/plugin.json": `{"mcpServers":"/etc/x.json"}`}, "outside the plugin"},
		"missing file":         {map[string]string{".claude-plugin/plugin.json": `{"mcpServers":"./nope.json"}`}, "doesn't exist"},
		"bad source type":      {map[string]string{".claude-plugin/plugin.json": `{"mcpServers":[7]}`}, "unrecognized source"},
	}
	for name, tt := range tests {
		dir := t.TempDir()
		for rel, content := range tt.files {
			write(t, dir, rel, content)
		}
		if _, err := ReadPluginMCPServers(dir); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: error = %v, want it to mention %q", name, err, tt.wantErr)
		}
	}
}

func TestReadPluginHooksSourcesAndShapes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "hooks/hooks.json", `{"description":"ignored string","hooks":{
		"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"a.sh","timeout":"45s"}]}],
		"Stop":[{"hooks":[{"type":"agent","prompt":"p"}]}],
		"PreToolUse":[{"if":"x","hooks":[{"command":"guarded.sh"}]}],
		"SessionStart":[{"hooks":[{"type":"command"}]}]
	}}`)
	write(t, dir, ".claude-plugin/plugin.json", `{"name":"p","hooks":"./more/hooks.json"}`)
	write(t, dir, "more/hooks.json", `{"SessionEnd":[{"hooks":[{"command":"bye.sh"}]}]}`)

	hooks, err := ReadPluginHooks(dir)
	if err != nil {
		t.Fatalf("ReadPluginHooks() error = %v", err)
	}
	byEvent := map[string]PluginHook{}
	for _, h := range hooks {
		byEvent[h.Event] = h
	}
	if h := byEvent["PostToolUse"]; h.Command != "a.sh" || h.Matcher != "Write" || h.Timeout != 45 || h.Unsupported != "" {
		t.Errorf("PostToolUse = %+v", h)
	}
	if h := byEvent["Stop"]; !strings.Contains(h.Unsupported, "agent handler") {
		t.Errorf("Stop = %+v, want the agent handler flagged unsupported", h)
	}
	if h := byEvent["PreToolUse"]; !strings.Contains(h.Unsupported, "`if`") {
		t.Errorf("PreToolUse = %+v, want the if-guarded group flagged unsupported", h)
	}
	if h := byEvent["SessionStart"]; !strings.Contains(h.Unsupported, "no command") {
		t.Errorf("SessionStart = %+v, want the command-less handler flagged", h)
	}
	if h := byEvent["SessionEnd"]; h.Command != "bye.sh" || h.Unsupported != "" {
		t.Errorf("a hook from a plugin.json path = %+v", h)
	}
}

func TestReadPluginHooksErrors(t *testing.T) {
	for name, content := range map[string]string{
		"group not an object":   `{"hooks":{"A":["x"]}}`,
		"handler not an object": `{"hooks":{"A":[{"hooks":["x"]}]}}`,
		"invalid json":          `{`,
	} {
		dir := t.TempDir()
		write(t, dir, "hooks/hooks.json", content)
		if _, err := ReadPluginHooks(dir); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if hooks, err := ReadPluginHooks(t.TempDir()); err != nil || len(hooks) != 0 {
		t.Errorf("a plugin with no hooks = %v, %v", hooks, err)
	}
}

func TestParseTimeoutSeconds(t *testing.T) {
	tests := []struct {
		in   any
		want int
	}{
		{float64(30), 30}, {float64(0.2), 1}, {float64(0), 0}, {float64(-5), 0},
		{"30s", 30}, {"2m", 120}, {"1500ms", 2}, {"1500", 1500}, {" 7 s ", 7}, {"1.5s", 2},
		{"soon", 0}, {"", 0}, {nil, 0}, {true, 0},
	}
	for _, tt := range tests {
		if got := parseTimeoutSeconds(tt.in); got != tt.want {
			t.Errorf("parseTimeoutSeconds(%#v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
