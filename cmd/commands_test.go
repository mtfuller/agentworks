package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInitNewListValidateTargets(t *testing.T) {
	dir := newProject(t, "claude-code")
	mustRun(t, dir, "new", "skill", "csv-analyzer", "--description", "Analyze a CSV file and flag rows that stand out from the rest.")
	mustRun(t, dir, "new", "mcp", "greeter", "--description", "Greet people by name over MCP when a tool needs a greeting.")

	list := mustRun(t, dir, "list")
	for _, want := range []string{"csv-analyzer", "greeter", "skill", "mcp"} {
		if !strings.Contains(list.stdout, want) {
			t.Errorf("list should show %q:\n%s", want, list.stdout)
		}
	}
	if only := mustRun(t, dir, "list", "mcp"); strings.Contains(only.stdout, "csv-analyzer") || !strings.Contains(only.stdout, "greeter") {
		t.Errorf("list mcp should filter by kind:\n%s", only.stdout)
	}
	if r := runCLI(t, dir, "list", "nonsense"); r.err == nil {
		t.Error("list with an unknown kind should fail")
	}

	var doc struct {
		OK        bool `json:"ok"`
		Artifacts []struct{ Name, Path string }
	}
	if err := json.Unmarshal([]byte(mustRun(t, dir, "list", "--json").stdout), &doc); err != nil || !doc.OK || len(doc.Artifacts) != 2 {
		t.Errorf("list --json = %+v, %v", doc, err)
	}

	if r := runCLI(t, dir, "validate", "--strict"); r.err != nil {
		t.Errorf("a fresh project should pass validate --strict: %v\n%s", r.err, r.combined())
	}
	if tg := mustRun(t, dir, "targets"); !strings.Contains(tg.stdout, "Claude Code") || !strings.Contains(tg.stdout, "export implemented") {
		t.Errorf("targets output:\n%s", tg.stdout)
	}
	if v := mustRun(t, dir, "version"); !strings.Contains(v.stdout, "Version:") || !strings.Contains(v.stdout, "Format:") {
		t.Errorf("version output:\n%s", v.stdout)
	}
	if v := mustRun(t, dir, "version", "--short"); strings.TrimSpace(v.stdout) == "" {
		t.Error("version --short should print the version")
	}
}

func TestValidateReportsAndFails(t *testing.T) {
	dir := newProject(t)
	mustRun(t, dir, "new", "skill", "vague", "--description", "Helps with stuff.")

	if r := runCLI(t, dir, "validate"); r.err != nil || !strings.Contains(r.combined(), "very short") {
		t.Errorf("a weak description should only warn: %v\n%s", r.err, r.combined())
	}
	if r := runCLI(t, dir, "validate", "--strict"); r.err == nil {
		t.Error("--strict should fail on a warning")
	}

	skill := filepath.Join(dir, "skills", "vague", "skill.md")
	data, _ := os.ReadFile(skill)
	os.WriteFile(skill, []byte(strings.Replace(string(data), "name: vague", "name: Not A Valid Name", 1)), 0o644)
	r := runCLI(t, dir, "validate", "--json")
	if r.err == nil {
		t.Fatal("an invalid artifact should fail validate")
	}
	var doc struct {
		OK      bool `json:"ok"`
		Summary struct{ Failed int }
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.OK || doc.Summary.Failed != 1 {
		t.Errorf("validate --json = %+v, %v\n%s", doc, err, r.stdout)
	}
}

func TestOutsideAProjectFailsClearly(t *testing.T) {
	empty := t.TempDir()
	for _, cmd := range []string{"list", "validate", "doctor", "status"} {
		r := runCLI(t, empty, cmd)
		if r.err == nil || !strings.Contains(r.err.Error(), "AgentWorks project") {
			t.Errorf("%s outside a project error = %v, want it to say so", cmd, r.err)
		}
	}
	r := runCLI(t, empty, "list", "--json")
	var doc struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.OK || doc.Error == "" {
		t.Errorf("--json outside a project should still produce an error document: %q, %v", r.stdout, err)
	}
}

func TestBuildRunsDeclaredBuildCommands(t *testing.T) {
	dir := newProject(t)
	mustRun(t, dir, "new", "skill", "builds", "--description", "A skill whose build command writes a file, to check build runs it.")
	skill := filepath.Join(dir, "skills", "builds", "skill.md")
	data, _ := os.ReadFile(skill)
	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", "version: 0.1.0\nbuild: echo built > out.txt", 1)), 0o644)

	mustRun(t, dir, "build")
	if got, _ := os.ReadFile(filepath.Join(dir, "skills", "builds", "out.txt")); strings.TrimSpace(string(got)) != "built" {
		t.Errorf("build should have run the command in the artifact's directory, out.txt = %q", got)
	}

	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", "version: 0.1.0\nbuild: exit 4", 1)), 0o644)
	if r := runCLI(t, dir, "build"); r.err == nil {
		t.Error("a failing build should fail the command")
	}
}

func TestExportStatusAndDrift(t *testing.T) {
	dir := newProject(t, "claude-code")
	mustRun(t, dir, "new", "skill", "csv-analyzer", "--description", "Analyze a CSV file and flag rows that stand out from the rest.")
	out := filepath.Join(dir, "dist")

	if r := runCLI(t, dir, "status"); r.err != nil || !strings.Contains(r.combined(), "No exports recorded") {
		t.Errorf("status before any export: %v\n%s", r.err, r.combined())
	}
	mustRun(t, dir, "export", "--out", out)

	if r := mustRun(t, dir, "status"); !strings.Contains(r.stdout, "in sync") {
		t.Errorf("status after export:\n%s", r.stdout)
	}
	if r := runCLI(t, dir, "status", "--fail-on-drift"); r.err != nil {
		t.Errorf("--fail-on-drift on a clean export: %v", r.err)
	}

	// Edit the source: stale. Edit the output: modified. Delete it: missing.
	skill := filepath.Join(dir, "skills", "csv-analyzer", "skill.md")
	f, _ := os.OpenFile(skill, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\nnew line\n")
	f.Close()
	if r := runCLI(t, dir, "status", "--fail-on-drift"); r.err == nil || !strings.Contains(r.stdout, "stale") {
		t.Errorf("a changed source should be stale and fail --fail-on-drift: %v\n%s", r.err, r.stdout)
	}
	mustRun(t, dir, "export", "--out", out)
	exported := filepath.Join(out, "claude-code", filepath.Base(dir), "skills", "csv-analyzer", "SKILL.md")
	g, err := os.OpenFile(exported, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("expected an exported skill at %s: %v", exported, err)
	}
	g.WriteString("hand edit\n")
	g.Close()
	if r := runCLI(t, dir, "status"); !strings.Contains(r.stdout, "modified") {
		t.Errorf("a hand-edited output should be modified:\n%s", r.stdout)
	}
	os.RemoveAll(filepath.Join(out, "claude-code"))
	if r := runCLI(t, dir, "status"); !strings.Contains(r.stdout, "missing") {
		t.Errorf("a deleted output should be missing:\n%s", r.stdout)
	}
}

func TestMarketplacePublishAndCheck(t *testing.T) {
	dir := newProject(t)
	mustRun(t, dir, "new", "skill", "csv-analyzer", "--description", "Analyze a CSV file and flag rows that stand out from the rest.")
	mustRun(t, dir, "new", "mcp", "greeter", "--description", "Greet people by name over MCP when a tool needs a greeting.")

	if r := runCLI(t, dir, "marketplace", "--check"); r.err == nil || !strings.Contains(r.combined(), "missing") {
		t.Errorf("--check before publishing should fail as missing: %v\n%s", r.err, r.combined())
	}
	mustRun(t, dir, "marketplace")
	for _, p := range []string{".claude-plugin/marketplace.json", ".github/plugin/marketplace.json"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("expected %s: %v", p, err)
		}
	}
	if r := runCLI(t, dir, "marketplace", "--check"); r.err != nil {
		t.Errorf("--check right after publishing: %v\n%s", r.err, r.combined())
	}
	if r := runCLI(t, dir, "marketplace", "--target", "claude-code", "--single"); r.err != nil {
		t.Errorf("a single-target, single-plugin publish: %v", r.err)
	}
	if r := runCLI(t, dir, "marketplace", "--target", "chatgpt"); r.err == nil {
		t.Error("an unknown marketplace target should fail")
	}

	skill := filepath.Join(dir, "skills", "csv-analyzer", "skill.md")
	f, _ := os.OpenFile(skill, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\nchanged\n")
	f.Close()
	if r := runCLI(t, dir, "marketplace", "--check"); r.err == nil || !strings.Contains(r.combined(), "stale") {
		t.Errorf("--check after a source change should be stale: %v\n%s", r.err, r.combined())
	}
}

func TestEvalRunsCasesAgainstTheRunner(t *testing.T) {
	dir := newProject(t)
	mustRun(t, dir, "new", "skill", "greeter", "--description", "Greets people by name whenever a greeting is called for.")
	skillDir := filepath.Join(dir, "skills", "greeter")
	os.WriteFile(filepath.Join(skillDir, "evals", "example.yaml"), []byte(`cases:
  - name: says-hello
    prompt: hello there
    assert:
      contains: ["HELLO"]
  - name: never-says-goodbye
    prompt: hi
    assert:
      not_contains: ["GOODBYE"]
`), 0o644)

	skill := filepath.Join(skillDir, "skill.md")
	data, _ := os.ReadFile(skill)
	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", `version: 0.1.0
eval_runner: tr a-z A-Z`, 1)), 0o644)

	r := runCLI(t, dir, "eval", "--json")
	if r.err != nil {
		t.Fatalf("eval failed: %v\n%s", r.err, r.combined())
	}
	var doc struct {
		Summary struct{ Ran, Failed int }
		Cases   []struct{ Case, Status string }
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.Summary.Ran != 2 || doc.Summary.Failed != 0 {
		t.Errorf("eval --json = %+v, %v\n%s", doc, err, r.stdout)
	}
	if one := runCLI(t, dir, "eval", "--case", "says-hello"); one.err != nil || strings.Contains(one.combined(), "never-says-goodbye") {
		t.Errorf("--case should run one case: %v\n%s", one.err, one.combined())
	}

	// A runner whose output breaks an assertion fails the command.
	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", `version: 0.1.0
eval_runner: echo nothing useful`, 1)), 0o644)
	if r := runCLI(t, dir, "eval"); r.err == nil || !strings.Contains(r.combined(), "failed") {
		t.Errorf("a failing assertion should fail eval: %v\n%s", r.err, r.combined())
	}
}

func TestTestCommandRunsDeclaredCommands(t *testing.T) {
	dir := newProject(t)
	mustRun(t, dir, "new", "skill", "checks", "--description", "A skill whose test command records that it ran, to check the command.")
	skill := filepath.Join(dir, "skills", "checks", "skill.md")
	data, _ := os.ReadFile(skill)
	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", "version: 0.1.0\ntest: echo ok > ran.txt", 1)), 0o644)

	mustRun(t, dir, "test")
	if _, err := os.Stat(filepath.Join(dir, "skills", "checks", "ran.txt")); err != nil {
		t.Errorf("test should have run the declared command: %v", err)
	}
	if r := mustRun(t, dir, "test", "--json"); !strings.Contains(r.stdout, `"status": "passed"`) {
		t.Errorf("test --json:\n%s", r.stdout)
	}

	os.WriteFile(skill, []byte(strings.Replace(string(data), "version: 0.1.0", "version: 0.1.0\ntest: exit 1", 1)), 0o644)
	if r := runCLI(t, dir, "test"); r.err == nil {
		t.Error("a failing test command should fail the command")
	}
}

func TestDoctorFindsMissingBinariesAndEnvironment(t *testing.T) {
	dir := newProject(t)
	mustRun(t, dir, "new", "mcp", "srv", "--description", "A server whose command names a binary that doesn't exist.")
	mcp := filepath.Join(dir, "mcp", "srv", "mcp.md")
	data, _ := os.ReadFile(mcp)
	s := strings.Replace(string(data), "command: python3 src/server.py", "command: definitely-not-a-real-binary-xyz", 1)
	s = strings.Replace(s, "auth: []", "auth:\n- SOME_UNSET_VARIABLE_XYZ", 1)
	os.WriteFile(mcp, []byte(s), 0o644)

	r := runCLI(t, dir, "doctor")
	if r.err == nil || !strings.Contains(r.combined(), "not on PATH") {
		t.Errorf("a missing binary should fail doctor: %v\n%s", r.err, r.combined())
	}
	if !strings.Contains(r.combined(), "SOME_UNSET_VARIABLE_XYZ") {
		t.Errorf("an unset auth variable should be reported:\n%s", r.combined())
	}
}

// ---- import / update, in process -----------------------------------------

func kitArchive(t *testing.T, server string) []byte {
	return pluginTarGz(t, map[string]string{
		".claude-plugin/plugin.json": `{"name":"kit","description":"A kit."}`,
		".mcp.json":                  `{"mcpServers":{"db":{"command":"${CLAUDE_PLUGIN_ROOT}/servers/db.sh"}}}`,
		"servers/db.sh":              server,
		"hooks/hooks.json":           `{"hooks":{"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}\"/scripts/fmt.sh","timeout":15}]}]}}`,
		"scripts/fmt.sh":             "#!/bin/sh\necho fmt\n",
	})
}

func TestAddAndUpdateInProcess(t *testing.T) {
	var mu sync.Mutex
	archive := kitArchive(t, "#!/bin/sh\necho v1\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Write(archive)
	}))
	defer srv.Close()
	setServer := func(body string) {
		mu.Lock()
		defer mu.Unlock()
		archive = kitArchive(t, body)
	}
	url := srv.URL + "/kit.tar.gz"

	dir := newProject(t)

	dry := mustRun(t, dir, "add", url, "--namespace", "kit", "--dry-run")
	if !strings.Contains(dry.stdout, "mcp") || !strings.Contains(dry.stdout, "hook") {
		t.Errorf("--dry-run should list what would be imported:\n%s", dry.stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "mcp")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "mcp"))
		if len(entries) != 0 {
			t.Error("--dry-run must not write anything")
		}
	}

	// Without --yes, importing content that declares a command refuses when not interactive.
	if r := runCLI(t, dir, "add", url, "--namespace", "kit"); r.err == nil || !strings.Contains(r.combined(), "--yes") {
		t.Errorf("add without --yes should refuse: %v\n%s", r.err, r.combined())
	}
	mustRun(t, dir, "add", url, "--namespace", "kit", "--yes")

	if r := runCLI(t, dir, "add", url, "--namespace", "kit", "--yes"); r.err == nil || !strings.Contains(r.combined(), "already exists") {
		t.Errorf("re-adding should fail: %v", r.err)
	}
	mustRun(t, dir, "add", url, "--namespace", "kit", "--yes", "--force")

	if r := mustRun(t, dir, "update"); strings.Count(r.combined(), "up to date") < 2 {
		t.Errorf("update on an unchanged source:\n%s", r.combined())
	}

	serverFile := filepath.Join(dir, "mcp", "kit", "db", "servers", "db.sh")
	setServer("#!/bin/sh\necho v2\n")
	report := mustRun(t, dir, "update", "--diff")
	if !strings.Contains(report.combined(), "upstream changed") || !strings.Contains(report.stdout, "+echo v2") {
		t.Errorf("update --diff should show the change:\n%s", report.combined())
	}
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "v1") {
		t.Error("update without --apply must not write")
	}
	mustRun(t, dir, "update", "--apply", "--yes")
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "v2") {
		t.Errorf("update --apply should write the new content, got %q", data)
	}

	// A local edit protects the artifact until --force.
	os.WriteFile(serverFile, []byte("#!/bin/sh\necho mine\n"), 0o755)
	setServer("#!/bin/sh\necho v3\n")
	if r := runCLI(t, dir, "update", "--apply", "--yes"); r.err == nil || !strings.Contains(r.combined(), "--force") {
		t.Errorf("update should refuse to discard a local edit: %v\n%s", r.err, r.combined())
	}
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "mine") {
		t.Error("the local edit must survive a refused update")
	}
	mustRun(t, dir, "update", "--apply", "--yes", "--force")
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "v3") {
		t.Errorf("--force should overwrite, got %q", data)
	}

	// Updating a path that was never imported is an error naming the lockfile.
	if r := runCLI(t, dir, "update", "skills/none"); r.err == nil || !strings.Contains(r.err.Error(), "agentworks.lock") {
		t.Errorf("update of an unrecorded path error = %v", r.err)
	}
}

func TestExportSkipsHooksThatBundleScriptsForOtherTargets(t *testing.T) {
	archive := kitArchive(t, "#!/bin/sh\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer srv.Close()

	dir := newProject(t, "claude-code", "cursor")
	mustRun(t, dir, "add", srv.URL+"/kit.tar.gz", "--namespace", "kit", "--yes")
	r := mustRun(t, dir, "export", "--out", filepath.Join(dir, "dist"))
	if !strings.Contains(r.combined(), "cursor: skipping") || !strings.Contains(r.combined(), "bundled script") {
		t.Errorf("cursor can't locate a bundled hook script, so export should say it skipped it:\n%s", r.combined())
	}
}

func TestSmokeTestAgainstARealLocalScaffold(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	dir := newProject(t)
	mustRun(t, dir, "new", "mcp", "srv", "--description", "A scaffolded server used to check the smoke test end to end.")
	// Drop the declared test so `test` falls back to the built-in smoke check.
	mcp := filepath.Join(dir, "mcp", "srv", "mcp.md")
	data, _ := os.ReadFile(mcp)
	lines := strings.Split(string(data), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if !strings.HasPrefix(l, "test:") {
			kept = append(kept, l)
		}
	}
	os.WriteFile(mcp, []byte(strings.Join(kept, "\n")), 0o644)

	r := mustRun(t, dir, "test", "--json")
	var doc struct {
		OK      bool `json:"ok"`
		Results []struct{ Type, Status string }
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || !doc.OK || len(doc.Results) != 1 || doc.Results[0].Type != "smoke" || doc.Results[0].Status != "passed" {
		t.Errorf("test --json = %+v, %v\n%s", doc, err, r.stdout)
	}
}
