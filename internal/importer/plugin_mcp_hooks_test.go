package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // WriteFile is subject to umask
		t.Fatal(err)
	}
}

// pluginWith returns a plugin directory (a valid manifest and nothing else) to
// which a test adds .mcp.json, hooks, and referenced files.
func pluginWith(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name":"demo-kit","description":"A demo kit."}`, 0o644)
	return dir
}

func planFor(t *testing.T, plugin string) *Plan {
	t.Helper()
	plan, err := detect(newTestProject(t), Source{Kind: SourceGitHub, Repo: "owner/demo-kit"}, plugin, Options{})
	if err != nil {
		t.Fatalf("detect() error = %v", err)
	}
	t.Cleanup(func() { plan.Close() })
	return plan
}

func find(t *testing.T, plan *Plan, kind artifact.Kind, name string) *artifact.Artifact {
	t.Helper()
	for _, a := range plan.Artifacts {
		if a.Kind == kind && a.Name == name {
			return a
		}
	}
	t.Fatalf("plan has no %s %q; has: %s", kind, name, plan.Describe())
	return nil
}

func TestImportMCPServersFromMCPJSON(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, ".mcp.json"), `{
	  "mcpServers": {
	    "plugin-database": {
	      "command": "${CLAUDE_PLUGIN_ROOT}/servers/db-server",
	      "args": ["--config", "${CLAUDE_PLUGIN_ROOT}/config.json"],
	      "env": {"DB_PATH": "${CLAUDE_PLUGIN_ROOT}/data", "DB_TOKEN": "${DB_TOKEN}", "REGION": "us-east-1"}
	    },
	    "remote-api": {
	      "type": "http",
	      "url": "https://api.example.com/mcp",
	      "headers": {"Authorization": "Bearer ${API_TOKEN}", "X-Version": "2"}
	    }
	  }
	}`, 0o644)
	writeTestFile(t, filepath.Join(plugin, "servers", "db-server"), "#!/bin/sh\necho hi\n", 0o755)
	writeTestFile(t, filepath.Join(plugin, "config.json"), `{}`, 0o644)
	writeTestFile(t, filepath.Join(plugin, "data", "seed.txt"), "seed", 0o644)
	writeTestFile(t, filepath.Join(plugin, "unrelated", "big.bin"), "not referenced", 0o644)

	plan := planFor(t, plugin)

	db := find(t, plan, artifact.KindMCP, "plugin-database")
	if got := db.ExtraString("command"); got != "./servers/db-server" {
		t.Errorf("command = %q, want the plugin root rewritten to the artifact directory", got)
	}
	if got := db.ExtraStringSlice("args"); len(got) != 2 || got[1] != "./config.json" {
		t.Errorf("args = %v, want [--config ./config.json]", got)
	}
	env := db.ExtraStringMap("env")
	if env["REGION"] != "us-east-1" || env["DB_PATH"] != "./data" {
		t.Errorf("env = %v, want literals kept and DB_PATH rewritten", env)
	}
	if _, leaked := env["DB_TOKEN"]; leaked {
		t.Errorf("DB_TOKEN passthrough belongs in auth, not env: %v", env)
	}
	if auth := db.ExtraStringSlice("auth"); len(auth) != 1 || auth[0] != "DB_TOKEN" {
		t.Errorf("auth = %v, want [DB_TOKEN]", auth)
	}

	remote := find(t, plan, artifact.KindMCP, "remote-api")
	if remote.ExtraString("transport") != "http" || remote.ExtraString("url") != "https://api.example.com/mcp" {
		t.Errorf("remote transport/url not carried: %+v", remote.Extra)
	}
	if auth := remote.ExtraStringSlice("auth"); len(auth) != 1 || auth[0] != "API_TOKEN" {
		t.Errorf("remote auth = %v, want the ${API_TOKEN} referenced in a header", auth)
	}
	if remote.ExtraString("command") != "" {
		t.Error("a remote server must not get a command")
	}

	// Apply, then check what landed on disk and that it's a valid artifact.
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for _, a := range []*artifact.Artifact{db, remote} {
		loaded, err := artifact.Load(a.Dir, artifact.KindMCP)
		if err != nil {
			t.Fatalf("Load(%s) error = %v", a.Dir, err)
		}
		if err := mcpconfig.Validate(loaded); err != nil {
			t.Errorf("imported %s doesn't validate: %v", a.Name, err)
		}
		if err := loaded.ValidateFieldTypes(); err != nil {
			t.Errorf("imported %s has a wrongly typed field: %v", a.Name, err)
		}
		for _, w := range loaded.LintFields() {
			t.Errorf("imported %s uses an unregistered field: %s", a.Name, w.Message)
		}
	}
	info, err := os.Stat(filepath.Join(db.Dir, "servers", "db-server"))
	if err != nil {
		t.Fatalf("server script not copied: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("server script lost its executable bit (mode %v) -- it is exec'd directly", info.Mode())
	}
	for _, rel := range []string{"config.json", "data/seed.txt"} {
		if _, err := os.Stat(filepath.Join(db.Dir, rel)); err != nil {
			t.Errorf("referenced %s not copied: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(db.Dir, "unrelated")); err == nil {
		t.Error("a file nothing references was copied")
	}
	if _, err := os.Stat(filepath.Join(db.Dir, importMarker)); err == nil {
		t.Errorf("the %s staging marker leaked into the project", importMarker)
	}
}

func TestImportNeverWritesALiteralCredential(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, ".mcp.json"), `{"mcpServers":{
	  "leaky-env": {"command": "run.sh", "env": {"API_KEY": "sk-live-SECRET123"}},
	  "leaky-header": {"type": "http", "url": "https://x.example/mcp", "headers": {"Authorization": "Bearer SECRET456"}},
	  "fine": {"command": "run.sh", "env": {"MODE": "fast"}}
	}}`, 0o644)

	plan := planFor(t, plugin)
	if len(plan.Artifacts) != 1 || plan.Artifacts[0].Name != "fine" {
		t.Fatalf("only the server without a literal credential should import, got: %s", plan.Describe())
	}
	if len(plan.Unsupported) != 2 {
		t.Fatalf("Unsupported = %v, want both leaky servers reported", plan.Unsupported)
	}
	everything := plan.Describe() + strings.Join(plan.Unsupported, "\n") + strings.Join(plan.Warnings, "\n")
	for _, secret := range []string{"SECRET123", "SECRET456"} {
		if strings.Contains(everything, secret) {
			t.Errorf("the credential %q appears in the plan's output: %s", secret, everything)
		}
	}
	for _, name := range []string{"API_KEY", "Authorization"} {
		if !strings.Contains(everything, name) {
			t.Errorf("the report should name the offending field %q", name)
		}
	}
}

func TestImportMCPServersFromPluginJSONInlineAndPath(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{
	  "name": "demo-kit",
	  "mcpServers": {"inline-one": {"command": "npx", "args": ["-y", "@co/server"]}}
	}`, 0o644)
	plan := planFor(t, plugin)
	inline := find(t, plan, artifact.KindMCP, "inline-one")
	if inline.ExtraString("command") != "npx" || len(inline.ExtraStringSlice("args")) != 2 {
		t.Errorf("inline server not carried: %+v", inline.Extra)
	}

	// The path form, merged with a standalone .mcp.json.
	plugin2 := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin2, ".claude-plugin", "plugin.json"), `{"name":"demo-kit","mcpServers":"./config/servers.json"}`, 0o644)
	writeTestFile(t, filepath.Join(plugin2, "config", "servers.json"), `{"mcpServers":{"from-path":{"command":"a"}}}`, 0o644)
	writeTestFile(t, filepath.Join(plugin2, ".mcp.json"), `{"mcpServers":{"from-file":{"command":"b"}}}`, 0o644)
	plan2 := planFor(t, plugin2)
	find(t, plan2, artifact.KindMCP, "from-path")
	find(t, plan2, artifact.KindMCP, "from-file")
}

func TestImportMCPKeepsAPathWithSpacesIntact(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, ".mcp.json"), `{"mcpServers":{"spaced":{"command":"${CLAUDE_PLUGIN_ROOT}/my server/run"}}}`, 0o644)
	writeTestFile(t, filepath.Join(plugin, "my server", "run"), "#!/bin/sh\n", 0o755)
	plan := planFor(t, plugin)
	// Run through `sh -c`, an unquoted path with a space would be two words.
	if got := find(t, plan, artifact.KindMCP, "spaced").ExtraString("command"); got != "'./my server/run'" {
		t.Errorf("command = %q, want the path single-quoted", got)
	}
}

func TestImportHooksGroupByScriptAndRewriteThePluginRoot(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, "hooks", "hooks.json"), `{
	  "hooks": {
	    "PostToolUse": [
	      {"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/scripts/format.sh", "timeout": "60s"}]}
	    ],
	    "PreToolUse": [
	      {"matcher": "Bash", "hooks": [{"type": "command", "command": "\"${CLAUDE_PLUGIN_ROOT}\"/scripts/format.sh --check", "timeout": 10}]}
	    ],
	    "SessionStart": [
	      {"hooks": [{"type": "command", "command": "echo hello"}]}
	    ]
	  }
	}`, 0o644)
	writeTestFile(t, filepath.Join(plugin, "scripts", "format.sh"), "#!/bin/sh\n", 0o755)

	plan := planFor(t, plugin)

	format := find(t, plan, artifact.KindHook, "format")
	handlers, err := format.HookHandlers()
	if err != nil {
		t.Fatalf("HookHandlers() error = %v", err)
	}
	if len(handlers) != 2 {
		t.Fatalf("format has %d handlers, want the two that run format.sh grouped together: %+v", len(handlers), handlers)
	}
	byEvent := map[string]artifact.HookHandler{}
	for _, h := range handlers {
		byEvent[h.Event] = h
	}
	post := byEvent["PostToolUse"]
	if post.Matcher != "Write|Edit" || post.Timeout != 60 || post.Command != `"${ARTIFACT_DIR}"/scripts/format.sh` {
		t.Errorf("PostToolUse handler = %+v, want matcher, a 60s timeout (from \"60s\"), and ${ARTIFACT_DIR}", post)
	}
	if pre := byEvent["PreToolUse"]; pre.Timeout != 10 || !strings.HasSuffix(pre.Command, "format.sh --check") {
		t.Errorf("PreToolUse handler = %+v, want its own args and timeout kept", pre)
	}

	inline := find(t, plan, artifact.KindHook, "sessionstart-all")
	if hs, _ := inline.HookHandlers(); len(hs) != 1 || hs[0].Command != "echo hello" {
		t.Errorf("inline hook handlers = %+v", hs)
	}
	if inline.UsesArtifactDir() {
		t.Error("an inline command has no bundled files and must not use ${ARTIFACT_DIR}")
	}

	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(format.Dir, "scripts", "format.sh"))
	if err != nil {
		t.Fatalf("hook script not copied: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("hook script lost its executable bit (mode %v)", info.Mode())
	}
	loaded, err := artifact.Load(format.Dir, artifact.KindHook)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.ValidateFieldTypes(); err != nil {
		t.Error(err)
	}
	for _, w := range loaded.LintFields() {
		t.Errorf("imported hook uses an unregistered field: %s", w.Message)
	}
}

func TestImportHooksReportWhatTheyCannotRepresent(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{
	  "Stop": [{"hooks": [{"type": "prompt", "prompt": "Is the task done?"}]}],
	  "PreToolUse": [{"matcher": "Bash", "if": "tool_input.command contains 'rm'", "hooks": [{"type": "command", "command": "guard.sh"}]}],
	  "PostToolUse": [{"hooks": [{"type": "command", "command": "ok.sh"}]}]
	}}`, 0o644)

	plan := planFor(t, plugin)
	if len(plan.Artifacts) != 1 {
		t.Fatalf("only the plain command hook should import, got: %s", plan.Describe())
	}
	joined := strings.Join(plan.Unsupported, "\n")
	if !strings.Contains(joined, "prompt handler") || !strings.Contains(joined, "`if` condition") {
		t.Errorf("Unsupported should say why each was skipped, got:\n%s", joined)
	}
}

func TestImportHooksFromPluginJSONInline(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, ".claude-plugin", "plugin.json"), `{"name":"demo-kit","hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"bye.sh"}]}]}}`, 0o644)
	plan := planFor(t, plugin)
	if a := find(t, plan, artifact.KindHook, "sessionend-all"); a == nil {
		t.Fatal("inline plugin.json hook not imported")
	}
}

func TestParseTimeout(t *testing.T) {
	// Exercised through a hook so the whole read path is covered.
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{"A":[{"hooks":[
	  {"type":"command","command":"a1","timeout":30},
	  {"type":"command","command":"a2","timeout":"2m"},
	  {"type":"command","command":"a3","timeout":"1500ms"},
	  {"type":"command","command":"a4","timeout":"soon"},
	  {"type":"command","command":"a5"}
	]}]}}`, 0o644)
	plan := planFor(t, plugin)
	got := map[string]int{}
	for _, a := range plan.Artifacts {
		hs, _ := a.HookHandlers()
		for _, h := range hs {
			got[h.Command] = h.Timeout
		}
	}
	want := map[string]int{"a1": 30, "a2": 120, "a3": 2, "a4": 0, "a5": 0}
	for cmd, secs := range want {
		if got[cmd] != secs {
			t.Errorf("timeout for %s = %d, want %d", cmd, got[cmd], secs)
		}
	}
}

func TestImportWarnsAboutMissingReferencedFiles(t *testing.T) {
	plugin := pluginWith(t)
	writeTestFile(t, filepath.Join(plugin, ".mcp.json"), `{"mcpServers":{"s":{"command":"${CLAUDE_PLUGIN_ROOT}/bin/missing","env":{"ROOT":"${CLAUDE_PLUGIN_ROOT}"},"cwd":"/tmp"}}}`, 0o644)
	plan := planFor(t, plugin)
	joined := strings.Join(plan.Warnings, "\n")
	for _, want := range []string{"bin", "isn't in the plugin", "plugin root itself", "cwd"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings should mention %q, got:\n%s", want, joined)
		}
	}
}

func TestImportHashesTheEntryAndItsFiles(t *testing.T) {
	// `update` decides whether upstream changed by hashing the staged content,
	// so both an edit to the JSON entry and an edit to a referenced file must
	// change it.
	hashOf := func(mcpJSON, scriptBody string) string {
		plugin := pluginWith(t)
		writeTestFile(t, filepath.Join(plugin, ".mcp.json"), mcpJSON, 0o644)
		writeTestFile(t, filepath.Join(plugin, "run.sh"), scriptBody, 0o755)
		plan := planFor(t, plugin)
		a := find(t, plan, artifact.KindMCP, "s")
		h, err := lockfile.HashDir(plan.HashSources[a.Dir])
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	base := hashOf(`{"mcpServers":{"s":{"command":"${CLAUDE_PLUGIN_ROOT}/run.sh"}}}`, "one")
	if again := hashOf(`{"mcpServers":{"s":{"command":"${CLAUDE_PLUGIN_ROOT}/run.sh"}}}`, "one"); again != base {
		t.Error("the same upstream content must hash the same")
	}
	if h := hashOf(`{"mcpServers":{"s":{"command":"${CLAUDE_PLUGIN_ROOT}/run.sh"}}}`, "two"); h == base {
		t.Error("editing a referenced file must change the hash")
	}
	if h := hashOf(`{"mcpServers":{"s":{"command":"${CLAUDE_PLUGIN_ROOT}/run.sh","args":["--new"]}}}`, "one"); h == base {
		t.Error("editing the server entry must change the hash")
	}
}
