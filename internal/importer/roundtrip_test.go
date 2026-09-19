package importer

import (
	"os"
	"path/filepath"
	"reflect"
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
