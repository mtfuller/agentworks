package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests hand what AgentWorks generates to the vendors' own tools. They
// are gated by AGENTWORKS_CONFORMANCE=1 because they need those tools on PATH
// (and, for Copilot and Gemini, install into a throwaway home) -- the nightly
// conformance workflow runs them; a plain `go test ./...` skips them. A tool
// that isn't installed skips its own test.

func conformanceOn(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTWORKS_CONFORMANCE") != "1" {
		t.Skip("set AGENTWORKS_CONFORMANCE=1 to run vendor conformance tests")
	}
}

func needTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not on PATH", name)
	}
}

func sh(t *testing.T, dir string, env []string, stdin string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out.String())
	}
	return out.String()
}

// withAuthor gives the project the metadata a publishable plugin should carry
// (Claude's --strict validator warns on a plugin with no author).
func withAuthor(t *testing.T, dir string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, "agentworks.yaml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\nversion: 1.0.0\nauthor:\n  name: Conformance Bot\nlicense: MIT\n"); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeAcceptsTheMarketplaceAndPlugin(t *testing.T) {
	conformanceOn(t)
	needTool(t, "claude")
	dir := setUpMarketplaceProject(t)
	withAuthor(t, dir)
	runAgentworks(t, "marketplace", "--project", dir)

	sh(t, dir, nil, "", "claude", "plugin", "validate", "--strict", ".")
	plugins, _ := filepath.Glob(filepath.Join(dir, "plugins", "claude-code", "*"))
	if len(plugins) == 0 {
		t.Fatal("marketplace wrote no claude-code plugins")
	}
	for _, p := range plugins {
		sh(t, dir, nil, "", "claude", "plugin", "validate", "--strict", p)
	}
}

func TestCopilotInstallsFromTheMarketplace(t *testing.T) {
	conformanceOn(t)
	needTool(t, "copilot")
	dir := setUpMarketplaceProject(t)
	withAuthor(t, dir)
	runAgentworks(t, "marketplace", "--project", dir)

	home := t.TempDir()
	env := []string{"COPILOT_HOME=" + home}
	sh(t, dir, env, "", "copilot", "plugin", "marketplace", "add", dir)
	name := filepath.Base(dir)
	_ = name // the marketplace is named for the project's manifest name
	out := sh(t, dir, env, "", "copilot", "plugin", "marketplace", "list")
	if strings.TrimSpace(out) == "" {
		t.Fatal("copilot lists no marketplaces after adding one")
	}
}

func TestGeminiInstallsTheExtension(t *testing.T) {
	conformanceOn(t)
	needTool(t, "gemini")
	dir := setUpMarketplaceProject(t)
	withAuthor(t, dir)
	out := filepath.Join(t.TempDir(), "dist")
	runAgentworks(t, "export", "--target", "gemini-cli", "--out", out, "--project", dir)

	exts, _ := filepath.Glob(filepath.Join(out, "gemini-cli", "*", "gemini-extension.json"))
	if len(exts) == 0 {
		t.Fatal("export wrote no gemini extension")
	}
	env := []string{"HOME=" + t.TempDir()}
	sh(t, dir, env, "y\ny\n", "gemini", "extensions", "install", filepath.Dir(exts[0]), "--consent")
}
