package tests

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestCLIVersion tests the version command
func TestCLIVersion(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to run version command: %v\nOutput: %s", err, out.String())
	}

	output := out.String()
	if !strings.Contains(output, "agentworks") {
		t.Errorf("Version output should contain 'agentworks', got: %s", output)
	}
	if !strings.Contains(output, "Version:") {
		t.Errorf("Version output should contain 'Version:', got: %s", output)
	}
}

// TestCLIVersionShort tests the version command with --short flag
func TestCLIVersionShort(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "version", "--short")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to run version --short command: %v\nOutput: %s", err, out.String())
	}

	output := strings.TrimSpace(out.String())
	// Should only output the version string
	if output != "dev" {
		t.Errorf("Version --short output should be 'dev', got: %s", output)
	}
}

// TestCLIHelp tests the help command
func TestCLIHelp(t *testing.T) {
	cmd := exec.Command("go", "run", "../main.go", "--help")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to run help command: %v\nOutput: %s", err, out.String())
	}

	output := out.String()
	if !strings.Contains(output, "Usage:") {
		t.Errorf("Help output should contain 'Usage:', got: %s", output)
	}
	if !strings.Contains(output, "Available Commands:") {
		t.Errorf("Help output should contain 'Available Commands:', got: %s", output)
	}
}

// TestCLIDoctorAgainstStarterProject runs "doctor" over the whole checked-in
// examples/starter-project. jira-fetch's three "auth:" variables are never
// set in a test environment, so doctor should warn about each by name but
// still exit 0 -- an unset auth variable doesn't stop an artifact from
// being discovered/listed, only from being actually called (see
// cmd/doctor.go's doctorChecks).
func TestCLIDoctorAgainstStarterProject(t *testing.T) {
	mainGo, err := filepath.Abs("../main.go")
	if err != nil {
		t.Fatalf("filepath.Abs(main.go) error = %v", err)
	}
	cmd := exec.Command("go", "run", mainGo, "doctor")
	cmd.Dir = "../examples/starter-project"
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("doctor should pass without --strict: %v\nOutput: %s", err, out.String())
	}

	output := out.String()
	for _, want := range []string{"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN"} {
		if !strings.Contains(output, want) {
			t.Errorf("doctor output should warn about missing %q, got: %s", want, output)
		}
	}
}

// TestCLIDoctorStrictFailsOnMissingAuth checks --strict promotes those same
// warnings to a failing exit code, matching "validate --strict"'s
// convention.
func TestCLIDoctorStrictFailsOnMissingAuth(t *testing.T) {
	mainGo, err := filepath.Abs("../main.go")
	if err != nil {
		t.Fatalf("filepath.Abs(main.go) error = %v", err)
	}
	cmd := exec.Command("go", "run", mainGo, "doctor", "--strict")
	cmd.Dir = "../examples/starter-project"
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err == nil {
		t.Fatalf("doctor --strict should fail with unset auth variables, got exit 0\nOutput: %s", out.String())
	}
}

// TestCLIRunNeedsInteractiveTerminal checks "run" refuses to launch the
// inspector's full-screen UI when stdin isn't a real terminal -- as it
// never is when go test execs the CLI -- rather than attempting it and
// hanging or corrupting the test runner's own output. A real end-to-end
// run of the inspector against examples/starter-project's jira-fetch tool
// isn't practical to drive here; internal/mcpclient and internal/inspector
// carry that coverage in their own unit tests instead (see AGENTS.md).
func TestCLIRunNeedsInteractiveTerminal(t *testing.T) {
	mainGo, err := filepath.Abs("../main.go")
	if err != nil {
		t.Fatalf("filepath.Abs(main.go) error = %v", err)
	}
	cmd := exec.Command("go", "run", mainGo, "run", "tools/jira-fetch")
	cmd.Dir = "../examples/starter-project"
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err == nil {
		t.Fatalf("run should fail without a terminal, got exit 0\nOutput: %s", out.String())
	}
	if !strings.Contains(out.String(), "interactive terminal") {
		t.Errorf("run output should mention needing an interactive terminal, got: %s", out.String())
	}
}

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

// runCLI builds the agentworks binary once per test run and runs it in dir
// (go run can't be used from a temp directory outside the module), returning
// combined output and error.
func runCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "agentworks-bin")
		if buildErr != nil {
			return
		}
		out, err := exec.Command("go", "build", "-o", filepath.Join(buildDir, "agentworks"), "..").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("%v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("building agentworks: %v", buildErr)
	}
	cmd := exec.Command(filepath.Join(buildDir, "agentworks"), args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// TestCLIExportRunsBuildFirst checks export runs each artifact's build:
// command (so bundled output ships fresh) and aborts before exporting
// anything if one fails, unless --no-build is passed.
func TestCLIExportRunsBuildFirst(t *testing.T) {
	dir := t.TempDir()
	if out, err := runCLI(t, dir, "init", "--target", "claude-code"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if out, err := runCLI(t, dir, "new", "skill", "bundled", "--description", "A skill whose build produces its output"); err != nil {
		t.Fatalf("new: %v\n%s", err, out)
	}
	skillMD := filepath.Join(dir, "skills", "bundled", "skill.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatal(err)
	}
	withBuild := func(cmd string) {
		t.Helper()
		content := strings.Replace(string(data), "entrypoint:", "build: "+cmd+"\nentrypoint:", 1)
		if err := os.WriteFile(skillMD, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	withBuild("mkdir -p dist && echo bundled > dist/main.js")
	if out, err := runCLI(t, dir, "export"); err != nil {
		t.Fatalf("export with passing build: %v\n%s", err, out)
	}
	shipped, _ := filepath.Glob(filepath.Join(dir, "dist", "claude-code", "*", "skills", "bundled", "dist", "main.js"))
	if len(shipped) == 0 {
		t.Errorf("built dist/main.js should be included in the export, output tree:\n%s", listTree(t, filepath.Join(dir, "dist", "claude-code")))
	}

	if err := os.RemoveAll(filepath.Join(dir, "dist")); err != nil {
		t.Fatal(err)
	}
	withBuild("exit 1")
	out, err := runCLI(t, dir, "export")
	if err == nil {
		t.Fatalf("export should fail when a build fails\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dist")); statErr == nil {
		t.Errorf("nothing should be exported after a failed build\n%s", out)
	}

	if out, err := runCLI(t, dir, "export", "--no-build"); err != nil {
		t.Fatalf("export --no-build should skip the failing build: %v\n%s", err, out)
	}
}

func listTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err == nil {
			b.WriteString(p + "\n")
		}
		return nil
	})
	return b.String()
}
