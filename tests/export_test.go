package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runAgentworksErr runs the CLI and returns its combined output and error,
// for cases where failure is the expected outcome.
func runAgentworksErr(args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"run", "../main.go"}, args...)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

func mustExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func setTargets(t *testing.T, dir string, targets ...string) {
	t.Helper()
	body := "name: kit\ntargets:\n"
	for _, tg := range targets {
		body += "  - " + tg + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "agentworks.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExportUsesProjectTargetsAndBundlesEverything is the headline case: no
// --target, no paths -- the project's own targets decide, and everything
// lands in one plugin.
func TestExportUsesProjectTargetsAndBundlesEverything(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	setTargets(t, dir, "claude-code")

	runAgentworks(t, "export", "--project", dir, "--out", filepath.Join(dir, "dist"))

	plugin := filepath.Join(dir, "dist", "claude-code", "kit")
	mustExist(t,
		filepath.Join(plugin, ".claude-plugin", "plugin.json"),
		filepath.Join(plugin, "skills", "csv-analyzer", "SKILL.md"),
		filepath.Join(plugin, "skills", "reviewer", "SKILL.md"), // team-a's, flattened
		filepath.Join(plugin, "agents", "triager.md"),
		filepath.Join(plugin, ".mcp.json"),
	)
}

func TestExportWithoutTargetsExplainsHowToSetThem(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	out, err := runAgentworksErr("export", "--project", dir)
	if err == nil || !strings.Contains(out, "targets:") {
		t.Fatalf("export with no targets = %v, output %q; want an error pointing at agentworks.yaml", err, out)
	}
}

func TestExportNamespaceFlagBundlesPerNamespace(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	setTargets(t, dir, "claude-code")

	runAgentworks(t, "export", "--project", dir, "--out", filepath.Join(dir, "dist"), "--namespace", "team-a")

	mustExist(t, filepath.Join(dir, "dist", "claude-code", "team-a", "skills", "reviewer", "SKILL.md"))
	if _, err := os.Stat(filepath.Join(dir, "dist", "claude-code", "kit")); err == nil {
		t.Error("the project's own plugin was written though only team-a was requested")
	}
}

func TestExportSkillsAsZipAndDotSkillNeedNoTarget(t *testing.T) {
	dir := setUpMarketplaceProject(t) // init'd with no targets
	out := filepath.Join(dir, "dist")

	runAgentworks(t, "export", "--project", dir, "--out", out, "--format", "skills.zip")
	runAgentworks(t, "export", "--project", dir, "--out", out, "--format", "skill")

	mustExist(t,
		filepath.Join(out, filepath.Base(dir)+"-skills.zip"),
		filepath.Join(out, "skills", "csv-analyzer.skill"),
		filepath.Join(out, "skills", "reviewer.skill"),
	)
}

func TestExportRejectsBadFlagCombos(t *testing.T) {
	dir := setUpMarketplaceProject(t)
	setTargets(t, dir, "claude-code")
	for _, args := range [][]string{
		{"export", "--project", dir, "--format", "nonsense"},
		{"export", "--project", dir, "--format", "skill", "--namespace", "team-a"},
		{"export", "--project", dir, "--namespace", "does-not-exist"},
	} {
		if out, err := runAgentworksErr(args...); err == nil {
			t.Errorf("%v should fail, output: %s", args, out)
		}
	}
}
