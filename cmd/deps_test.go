package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReq(t *testing.T, dir, kind, name, requires string) {
	t.Helper()
	d := filepath.Join(dir, map[string]string{"agent": "agents", "skill": "skills", "mcp": "mcp", "hook": "hooks"}[kind], name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\nkind: " + kind + "\nname: " + name + "\ndescription: Handles " + name + " requests when a user needs that particular job done.\n"
	if kind == "mcp" {
		fm += "command: python3 main.py\n"
	}
	if requires != "" {
		fm += "requires: [" + requires + "]\n"
	}
	body := "---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(d, kind+".md"), []byte(fm+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsDanglingRequires(t *testing.T) {
	dir := newProject(t, "claude-code")
	writeReq(t, dir, "skill", "csv", "")
	writeReq(t, dir, "agent", "bot", "skill:csv, skill:missing")
	r := runCLI(t, dir, "validate")
	if r.err == nil || !strings.Contains(r.combined(), "requires skill:missing") {
		t.Fatalf("validate should fail on the dangling reference: %v\n%s", r.err, r.combined())
	}
}

func TestExportRefusesABundleThatWouldDangle(t *testing.T) {
	dir := newProject(t, "claude-code")
	writeReq(t, dir, "skill", "csv", "")
	writeReq(t, dir, "agent", "bot", "skill:csv")
	out := filepath.Join(t.TempDir(), "dist")

	r := runCLI(t, dir, "export", "--out", out, filepath.Join(dir, "agents", "bot"))
	if r.err == nil || !strings.Contains(r.combined(), "requires skill:csv") || !strings.Contains(r.combined(), "include it") {
		t.Fatalf("a partial export should fail loudly: %v\n%s", r.err, r.combined())
	}
	mustRun(t, dir, "export", "--out", out)

	// The agent's own instructions name what it depends on.
	found, _ := filepath.Glob(filepath.Join(out, "claude-code", "*", "agents", "bot.md"))
	if len(found) != 1 {
		t.Fatalf("agent not exported: %v", found)
	}
	data, _ := os.ReadFile(found[0])
	if !strings.Contains(string(data), "skill `csv`") {
		t.Errorf("exported agent should list its requirements:\n%s", data)
	}
}

func TestGraphAndListShowDependencies(t *testing.T) {
	dir := newProject(t, "claude-code")
	writeReq(t, dir, "skill", "csv", "")
	writeReq(t, dir, "agent", "bot", "skill:csv")
	out := mustRun(t, dir, "graph").stdout
	if !strings.Contains(out, "requires: skill:csv") || !strings.Contains(out, "required by: agent:bot") {
		t.Errorf("graph = %q", out)
	}
	if dot := mustRun(t, dir, "graph", "--dot").stdout; !strings.Contains(dot, `"agent:bot" -> "skill:csv"`) {
		t.Errorf("dot = %q", dot)
	}
	if list := mustRun(t, dir, "list", "--json").stdout; !strings.Contains(list, `"skill:csv"`) {
		t.Errorf("list --json lacks the edge: %s", list)
	}
}

func TestDoctorChecksBins(t *testing.T) {
	dir := newProject(t, "claude-code")
	writeReq(t, dir, "skill", "csv", "")
	p := filepath.Join(dir, "skills", "csv", "skill.md")
	data, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(data), "\n---\n\nBody", "\nbins: [no-such-binary-xyz]\n---\n\nBody", 1)), 0o644)
	r := runCLI(t, dir, "doctor")
	if r.err == nil || !strings.Contains(r.combined(), "no-such-binary-xyz") {
		t.Fatalf("doctor should flag the missing binary: %v\n%s", r.err, r.combined())
	}
}
