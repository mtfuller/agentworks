package tests

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

// agentworksBinary builds the CLI once per test run; go run per invocation
// would dominate the runtime of a multi-step scenario.
func agentworksBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "agentworks-test-bin-*")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "agentworks")
		out, err := exec.Command("go", "build", "-o", binPath, "../main.go").CombinedOutput()
		if err != nil {
			binErr = err
			t.Logf("build output: %s", out)
		}
	})
	if binErr != nil {
		t.Fatalf("building agentworks: %v", binErr)
	}
	return binPath
}

func run(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(agentworksBinary(t), append(args, "--project", dir)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running agentworks %v: %v", args, err)
	}
	return out.String(), code
}

// pluginArchive builds a .tar.gz of a Claude Code plugin under a single
// top-level directory, the way a GitHub tarball is laid out. files maps a
// path to its content; a path ending in ".sh" is written executable.
func pluginArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: "kit-main/" + name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func kitFiles(serverBody, hookBody string) map[string]string {
	return map[string]string{
		".claude-plugin/plugin.json": `{"name":"kit","description":"A kit."}`,
		".mcp.json":                  `{"mcpServers":{"db":{"command":"${CLAUDE_PLUGIN_ROOT}/servers/db.sh","env":{"DB_TOKEN":"${DB_TOKEN}"}}}}`,
		"servers/db.sh":              serverBody,
		"hooks/hooks.json":           `{"hooks":{"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}\"/scripts/fmt.sh","timeout":15}]}]}}`,
		"scripts/fmt.sh":             hookBody,
	}
}

func TestImportUpdateLifecycleForMCPAndHooks(t *testing.T) {
	var mu sync.Mutex
	archive := pluginArchive(t, kitFiles("#!/bin/sh\necho v1\n", "#!/bin/sh\necho fmt1\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Write(archive)
	}))
	defer srv.Close()
	setUpstream := func(server string) {
		mu.Lock()
		defer mu.Unlock()
		archive = pluginArchive(t, kitFiles(server, "#!/bin/sh\necho fmt1\n"))
	}
	url := srv.URL + "/kit.tar.gz"

	dir := t.TempDir()
	if out, code := run(t, dir, "init", dir); code != 0 {
		t.Fatalf("init failed: %s", out)
	}

	// --- add: an MCP server and a hook come in, with their files.
	out, code := run(t, dir, "add", url, "--namespace", "kit", "--yes")
	if code != 0 {
		t.Fatalf("add failed: %s", out)
	}
	mcpMD := filepath.Join(dir, "mcp", "kit", "db", "mcp.md")
	hookMD := filepath.Join(dir, "hooks", "kit", "fmt", "hook.md")
	serverFile := filepath.Join(dir, "mcp", "kit", "db", "servers", "db.sh")
	for _, p := range []string{mcpMD, hookMD, serverFile, filepath.Join(dir, "hooks", "kit", "fmt", "scripts", "fmt.sh")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s after add: %v\n%s", p, err, out)
		}
	}
	if info, _ := os.Stat(serverFile); info.Mode()&0o111 == 0 {
		t.Error("the imported server script lost its executable bit")
	}
	lock, _ := os.ReadFile(filepath.Join(dir, "agentworks.lock"))
	if !strings.Contains(string(lock), "local_sha256") {
		t.Errorf("the lockfile should record a local hash to detect later edits:\n%s", lock)
	}
	if out, code := run(t, dir, "validate", "--strict"); code != 0 {
		t.Errorf("imported artifacts should pass validate --strict:\n%s", out)
	}

	// --- add again: refuses without --force, succeeds with it.
	if out, code := run(t, dir, "add", url, "--namespace", "kit", "--yes"); code == 0 || !strings.Contains(out, "already exists") {
		t.Errorf("re-adding should fail with 'already exists' (exit %d):\n%s", code, out)
	}
	if out, code := run(t, dir, "add", url, "--namespace", "kit", "--yes", "--force"); code != 0 {
		t.Errorf("add --force should replace the existing artifacts:\n%s", out)
	}

	// --- update: nothing changed yet.
	if out, code := run(t, dir, "update"); code != 0 || strings.Count(out, "up to date") < 2 {
		t.Fatalf("update on an unchanged source should report everything up to date (exit %d):\n%s", code, out)
	}

	// --- upstream edits the server script.
	setUpstream("#!/bin/sh\necho v2\n")
	out, code = run(t, dir, "update", "--diff")
	if code != 0 {
		t.Fatalf("update --diff (report only) exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "mcp/kit/db: upstream changed") || !strings.Contains(out, "+echo v2") || !strings.Contains(out, "-echo v1") {
		t.Errorf("update --diff should show the change as a diff:\n%s", out)
	}
	if !strings.Contains(out, "hooks/kit/fmt: up to date") {
		t.Errorf("the untouched hook should still be up to date:\n%s", out)
	}
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "v1") {
		t.Error("update without --apply must not write anything")
	}

	if out, code := run(t, dir, "update", "--apply", "--yes"); code != 0 {
		t.Fatalf("update --apply failed:\n%s", out)
	}
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "v2") {
		t.Errorf("update --apply should have written the new script, got %q", data)
	}
	if out, code := run(t, dir, "update"); code != 0 || strings.Contains(out, "upstream changed") {
		t.Errorf("after --apply everything should be up to date again:\n%s", out)
	}

	// --- you edit the server locally; upstream changes again.
	if err := os.WriteFile(serverFile, []byte("#!/bin/sh\necho my-local-edit\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	setUpstream("#!/bin/sh\necho v3\n")

	out, code = run(t, dir, "update", "--apply", "--yes")
	if code == 0 {
		t.Fatalf("update --apply should refuse to discard local edits:\n%s", out)
	}
	if !strings.Contains(out, "edited this since") || !strings.Contains(out, "--force") {
		t.Errorf("the refusal should explain itself and name --force:\n%s", out)
	}
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "my-local-edit") {
		t.Errorf("the local edit was overwritten despite the refusal: %q", data)
	}

	if out, code := run(t, dir, "update", "--apply", "--yes", "--force"); code != 0 {
		t.Fatalf("update --apply --force should overwrite:\n%s", out)
	}
	if data, _ := os.ReadFile(serverFile); !strings.Contains(string(data), "v3") {
		t.Errorf("--force should have written upstream's version, got %q", data)
	}
}

// A hook that runs a bundled script exports for claude-code with its files, and
// is skipped (with a warning) for a target that has no way to find them.
func TestExportShipsBundledHookFilesOnlyWhereTheyCanWork(t *testing.T) {
	archive := pluginArchive(t, kitFiles("#!/bin/sh\n", "#!/bin/sh\necho fmt\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer srv.Close()

	dir := t.TempDir()
	if out, code := run(t, dir, "init", dir, "--target", "claude-code", "--target", "cursor"); code != 0 {
		t.Fatalf("init failed: %s", out)
	}
	if out, code := run(t, dir, "add", srv.URL+"/kit.tar.gz", "--namespace", "kit", "--yes"); code != 0 {
		t.Fatalf("add failed: %s", out)
	}

	out, code := run(t, dir, "export", "--out", filepath.Join(dir, "dist"))
	if code != 0 {
		t.Fatalf("export failed:\n%s", out)
	}
	if !strings.Contains(out, "cursor: skipping") || !strings.Contains(out, "bundled script") {
		t.Errorf("cursor can't locate a hook's bundled script, so the export should say it skipped it:\n%s", out)
	}

	hooksJSON, err := os.ReadFile(filepath.Join(dir, "dist", "claude-code", filepath.Base(dir), "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("reading the exported hooks.json: %v", err)
	}
	if !strings.Contains(string(hooksJSON), "${CLAUDE_PLUGIN_ROOT}/hook-files/fmt") {
		t.Errorf("the hook command should locate its files via ${CLAUDE_PLUGIN_ROOT}:\n%s", hooksJSON)
	}
	script := filepath.Join(dir, "dist", "claude-code", filepath.Base(dir), "hook-files", "fmt", "scripts", "fmt.sh")
	if info, err := os.Stat(script); err != nil {
		t.Errorf("the hook's script wasn't shipped: %v", err)
	} else if info.Mode()&0o111 == 0 {
		t.Error("the shipped hook script isn't executable")
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "cursor", ".cursor", "hooks.json")); err == nil {
		t.Error("cursor got a hooks.json even though its only hook was skipped")
	}
}
