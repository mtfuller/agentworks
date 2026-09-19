package importer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
)

// Exporting a project as a claude-code bundle and importing that bundle back
// must give back equivalent artifacts: the same hook handlers, the same MCP
// command and environment, and the same bundled files. This is the property
// that makes importing your own published marketplace safe.
func TestClaudeCodeBundleRoundTrips(t *testing.T) {
	srcRoot := newTestProject(t)

	hook := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindHook, Name: "check-shell", Description: "Checks shell commands before they run.",
			Extra: map[string]any{"handlers": []any{
				map[string]any{"event": "PreToolUse", "matcher": "Bash", "command": `"${ARTIFACT_DIR}"/scripts/check.sh`, "timeout": 20},
				map[string]any{"event": "SessionStart", "command": `"${ARTIFACT_DIR}"/scripts/check.sh --banner`},
			}},
		},
		Dir: filepath.Join(srcRoot, "hooks", "check-shell"),
	}
	writeTestFile(t, filepath.Join(hook.Dir, "scripts", "check.sh"), "#!/bin/sh\necho checking\n", 0o755)

	server := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindMCP, Name: "greeter", Description: "Greets people by name over MCP.",
			Extra: map[string]any{
				"command": "python3 src/server.py",
				"env":     map[string]any{"REGION": "us-east-1"},
				"auth":    []any{"GREETER_TOKEN"},
			},
		},
		Dir: filepath.Join(srcRoot, "mcp", "greeter"),
	}
	writeTestFile(t, filepath.Join(server.Dir, "src", "server.py"), "print('hi')\n", 0o644)

	for _, a := range []*artifact.Artifact{hook, server} {
		if err := a.Save(); err != nil {
			t.Fatalf("Save(%s) error = %v", a.Name, err)
		}
	}

	exporter, err := targets.GetExporter("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	bundler, ok := exporter.(targets.BundleExporter)
	if !ok {
		t.Fatal("claude-code should be a BundleExporter")
	}
	outDir := t.TempDir()
	plugin, err := bundler.ExportBundle("kit", "A kit for the round trip.", []*artifact.Artifact{hook, server}, outDir, targets.ExportOptions{})
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}

	// Import the exported plugin into a fresh project.
	plan := planFor(t, plugin)
	if len(plan.Unsupported) != 0 {
		t.Fatalf("nothing AgentWorks exported should be unsupported on re-import: %v", plan.Unsupported)
	}

	gotHook := find(t, plan, artifact.KindHook, "check-shell") // the original name, recovered from hook-files/<name>
	wantHandlers, _ := hook.HookHandlers()
	gotHandlers, err := gotHook.HookHandlers()
	if err != nil {
		t.Fatal(err)
	}
	// Handlers come back grouped and in event order; compare as sets.
	if !sameHandlers(wantHandlers, gotHandlers) {
		t.Errorf("hook handlers changed in the round trip:\n want %+v\n  got %+v", wantHandlers, gotHandlers)
	}

	gotServer := find(t, plan, artifact.KindMCP, "greeter")
	if got, want := gotServer.ExtraString("command"), "python3 src/server.py"; got != want {
		t.Errorf("mcp command = %q, want the original %q back (not the bundle's cd wrapper)", got, want)
	}
	if !reflect.DeepEqual(gotServer.ExtraStringMap("env"), map[string]string{"REGION": "us-east-1"}) {
		t.Errorf("mcp env = %v", gotServer.ExtraStringMap("env"))
	}
	if !reflect.DeepEqual(gotServer.ExtraStringSlice("auth"), []string{"GREETER_TOKEN"}) {
		t.Errorf("mcp auth = %v", gotServer.ExtraStringSlice("auth"))
	}

	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	for path, want := range map[string]string{
		filepath.Join(gotHook.Dir, "scripts", "check.sh"): "#!/bin/sh\necho checking\n",
		filepath.Join(gotServer.Dir, "src", "server.py"):  "print('hi')\n",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("bundled file lost in the round trip: %v", err)
			continue
		}
		if string(data) != want {
			t.Errorf("%s = %q, want %q", path, data, want)
		}
	}
	// Only this hook's own files, not every hook-files/ directory in the plugin.
	if _, err := os.Stat(filepath.Join(gotHook.Dir, "hook-files")); err == nil {
		t.Error("the plugin's hook-files/ layout leaked into the imported artifact")
	}
	if _, err := os.Stat(filepath.Join(gotServer.Dir, "mcp")); err == nil {
		t.Error("the plugin's mcp/ layout leaked into the imported artifact")
	}
}

func sameHandlers(a, b []artifact.HookHandler) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[artifact.HookHandler]int{}
	for _, h := range a {
		seen[h]++
	}
	for _, h := range b {
		seen[h]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// A hook that runs a bundled script exports for claude-code, but a target with
// no way to locate the files skips it instead of emitting a broken command.
func TestBundledScriptHookIsSkippedWhereItCannotWork(t *testing.T) {
	hook := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindHook, Name: "h", Description: "A hook with a script.",
			Extra: map[string]any{"handlers": []any{map[string]any{"event": "PreToolUse", "command": "${ARTIFACT_DIR}/x.sh"}}},
		},
		Dir: "hooks/h",
	}
	inline := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindHook, Name: "i", Description: "An inline hook.",
			Extra: map[string]any{"events": []any{"PreToolUse"}, "command": "echo hi"},
		},
		Dir: "hooks/i",
	}
	for _, target := range []string{"github-copilot", "cursor", "gemini-cli"} {
		if targets.UnsupportedReason(target, hook) == "" {
			t.Errorf("%s should refuse a hook that needs its bundled files", target)
		}
		if targets.UnsupportedReason(target, inline) != "" {
			t.Errorf("%s should accept an inline hook", target)
		}
	}
	if targets.UnsupportedReason("claude-code", hook) != "" {
		t.Error("claude-code ships a hook's files and should accept it")
	}
}

// The same property for a GitHub Copilot (Agent Plugins) bundle: agents live in
// com.github.copilot/agents/, hooks in com.github.copilot/hooks/hooks.json, MCP
// servers in mcp.json with a `cd 'mcp/<name>'` wrapper relative to the plugin root.
func TestGitHubCopilotBundleRoundTrips(t *testing.T) {
	srcRoot := newTestProject(t)

	agent := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{Kind: artifact.KindAgent, Name: "triager", Description: "Triages support tickets and assigns a priority."},
		Body:        "# Triager\n\nRead the ticket and assign a priority.\n",
		Dir:         filepath.Join(srcRoot, "agents", "triager"),
	}
	hook := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindHook, Name: "audit", Description: "Audits shell commands before they run.",
			Extra: map[string]any{"handlers": []any{
				map[string]any{"event": "preToolUse", "matcher": "bash", "command": "echo audit", "timeout": 12},
			}},
		},
		Dir: filepath.Join(srcRoot, "hooks", "audit"),
	}
	server := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind: artifact.KindMCP, Name: "greeter", Description: "Greets people by name over MCP.",
			Extra: map[string]any{"command": "python3 src/server.py", "auth": []any{"GREETER_TOKEN"}},
		},
		Dir: filepath.Join(srcRoot, "mcp", "greeter"),
	}
	writeTestFile(t, filepath.Join(server.Dir, "src", "server.py"), "print('hi')\n", 0o644)
	for _, a := range []*artifact.Artifact{agent, hook, server} {
		if err := a.Save(); err != nil {
			t.Fatalf("Save(%s) error = %v", a.Name, err)
		}
	}

	exporter, err := targets.GetExporter("github-copilot")
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := exporter.(targets.BundleExporter).ExportBundle("kit", "A kit for the round trip.", []*artifact.Artifact{agent, hook, server}, t.TempDir(), targets.ExportOptions{})
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}

	plan := planFor(t, plugin)
	if len(plan.Unsupported) != 0 {
		t.Fatalf("nothing AgentWorks exported should be unsupported on re-import: %v", plan.Unsupported)
	}

	gotAgent := find(t, plan, artifact.KindAgent, "triager")
	if gotAgent.Description != agent.Description || !strings.Contains(gotAgent.Body, "assign a priority") {
		t.Errorf("agent not carried: %+v / %q", gotAgent.Frontmatter, gotAgent.Body)
	}

	gotHook := find(t, plan, artifact.KindHook, "pretooluse-bash") // an inline hook has no directory to recover its original name from
	handlers, _ := gotHook.HookHandlers()
	want := []artifact.HookHandler{{Event: "preToolUse", Matcher: "bash", Command: "echo audit", Timeout: 12}}
	if !sameHandlers(handlers, want) {
		t.Errorf("hook handlers = %+v, want %+v", handlers, want)
	}

	gotServer := find(t, plan, artifact.KindMCP, "greeter")
	if got := gotServer.ExtraString("command"); got != "python3 src/server.py" {
		t.Errorf("mcp command = %q, want the original back", got)
	}
	if err := plan.Apply(); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(gotServer.Dir, "src", "server.py")); err != nil || string(data) != "print('hi')\n" {
		t.Errorf("the server's files didn't survive the round trip: %q, %v", data, err)
	}
}
