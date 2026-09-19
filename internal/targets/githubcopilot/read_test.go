package githubcopilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsPluginDirAndManifest(t *testing.T) {
	root := t.TempDir()
	if IsPluginDir(root) {
		t.Error("an empty directory is not a plugin")
	}
	writeFile(t, root, "plugin.json", `{"name":"kit","description":"A kit."}`)
	if !IsPluginDir(root) {
		t.Fatal("a root plugin.json makes a plugin")
	}
	name, desc, err := ReadPluginManifest(root)
	if err != nil || name != "kit" || desc != "A kit." {
		t.Errorf("ReadPluginManifest() = %q, %q, %v", name, desc, err)
	}

	nested := t.TempDir()
	writeFile(t, nested, ".github/plugin/plugin.json", `{"name":"nested"}`)
	if !IsPluginDir(nested) {
		t.Error(".github/plugin/plugin.json also makes a plugin")
	}
	if name, _, _ := ReadPluginManifest(nested); name != "nested" {
		t.Errorf("name = %q", name)
	}

	// A Claude Code plugin's manifest location is deliberately not this format's.
	claude := t.TempDir()
	writeFile(t, claude, ".claude-plugin/plugin.json", `{"name":"c"}`)
	if IsPluginDir(claude) {
		t.Error("a .claude-plugin/ directory belongs to the Claude Code layout")
	}
	if _, _, err := ReadPluginManifest(claude); err == nil {
		t.Error("reading a manifest that isn't there should fail")
	}
	bad := t.TempDir()
	writeFile(t, bad, "plugin.json", `{`)
	if _, _, err := ReadPluginManifest(bad); err == nil {
		t.Error("malformed plugin.json should fail")
	}
}

func TestIsMarketplaceDir(t *testing.T) {
	dir := t.TempDir()
	if IsMarketplaceDir(dir) {
		t.Error("empty directory is not a marketplace")
	}
	writeFile(t, dir, ".github/plugin/marketplace.json", `{}`)
	if !IsMarketplaceDir(dir) {
		t.Error(".github/plugin/marketplace.json marks a marketplace")
	}
}

func TestAgentFilesFindsEachLocationOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "com.github.copilot/agents/one.agent.md", "x")
	writeFile(t, dir, "agents/two.agent.md", "x")
	writeFile(t, dir, "agents/three.md", "x")
	files, err := AgentFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range files {
		got[filepath.Base(f)]++
	}
	for _, name := range []string{"one.agent.md", "two.agent.md", "three.md"} {
		if got[name] != 1 {
			t.Errorf("%s listed %d times, want exactly once (agents/*.md also matches *.agent.md): %v", name, got[name], files)
		}
	}
}

func TestReadPluginHooks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "com.github.copilot/hooks/hooks.json", `{"version":1,"hooks":{
		"preToolUse":[
			{"type":"command","bash":"check.sh","timeoutSec":20,"matcher":"bash"},
			{"command":"portable.sh","timeout":7},
			{"powershell":"only-windows.ps1"},
			{"type":"prompt","bash":"x"}
		],
		"sessionStart":[{"bash":"  "}]
	}}`)
	hooks, err := ReadPluginHooks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 5 {
		t.Fatalf("got %d hooks: %+v", len(hooks), hooks)
	}
	// Events are read in sorted order, entries in file order.
	pre := hooks[:4]
	if pre[0].Command != "check.sh" || pre[0].Timeout != 20 || pre[0].Matcher != "bash" || pre[0].Unsupported != "" {
		t.Errorf("first hook = %+v", pre[0])
	}
	if pre[1].Command != "portable.sh" || pre[1].Timeout != 7 {
		t.Errorf("a `command`/`timeout` entry = %+v", pre[1])
	}
	if !strings.Contains(pre[2].Unsupported, "powershell") {
		t.Errorf("a powershell-only hook should be unsupported: %+v", pre[2])
	}
	if !strings.Contains(pre[3].Unsupported, "prompt handler") {
		t.Errorf("a prompt-type hook should be unsupported: %+v", pre[3])
	}
	if hooks[4].Unsupported == "" {
		t.Errorf("a blank command should be unsupported: %+v", hooks[4])
	}

	// The other locations are read too; a bad file is an error, none at all is fine.
	other := t.TempDir()
	writeFile(t, other, "hooks/hooks.json", `{"hooks":{"stop":[{"bash":"a.sh"}]}}`)
	writeFile(t, other, "hooks.json", `{"hooks":{"stop":[{"bash":"b.sh"}]}}`)
	if hooks, err := ReadPluginHooks(other); err != nil || len(hooks) != 2 {
		t.Errorf("hooks from hooks/hooks.json and hooks.json = %+v, %v", hooks, err)
	}
	bad := t.TempDir()
	writeFile(t, bad, "hooks.json", `{`)
	if _, err := ReadPluginHooks(bad); err == nil {
		t.Error("malformed hooks.json should fail")
	}
	if hooks, err := ReadPluginHooks(t.TempDir()); err != nil || len(hooks) != 0 {
		t.Errorf("no hooks file = %v, %v", hooks, err)
	}
}
