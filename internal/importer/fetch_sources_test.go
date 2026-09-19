package importer

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestPrepareFromAGitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origProtocols := gitAllowedProtocols
	gitAllowedProtocols = "https:ssh:file" // let the test clone a local repository
	t.Cleanup(func() { gitAllowedProtocols = origProtocols })

	remote := t.TempDir()
	runGit(t, remote, "init", "--quiet", "-b", "main")
	writeTestFile(t, filepath.Join(remote, "plugins", "kit", ".claude-plugin", "plugin.json"), `{"name":"kit"}`, 0o644)
	writeTestFile(t, filepath.Join(remote, "plugins", "kit", ".mcp.json"), `{"mcpServers":{"s":{"command":"echo"}}}`, 0o644)
	writeTestFile(t, filepath.Join(remote, "plugins", "kit", "skills", "one", "SKILL.md"), "---\nname: one\ndescription: A skill from a git remote, used to test the fetch.\n---\n\nbody\n", 0o644)
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "--quiet", "-m", "first")
	first := runGit(t, remote, "rev-parse", "HEAD")

	src := Source{Kind: SourceGit, URL: remote, Ref: "main", Path: "plugins/kit"}
	plan, err := Prepare(context.Background(), newTestProject(t), src, Options{Namespace: "team"})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer plan.Close()

	if plan.Commit != first {
		t.Errorf("Commit = %q, want the remote's HEAD %q", plan.Commit, first)
	}
	find(t, plan, "skill", "one")
	find(t, plan, "mcp", "s")
	// The clone's .git directory must not be part of what gets hashed or copied.
	for _, dir := range plan.HashSources {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			t.Errorf("%s still contains a .git directory", dir)
		}
	}

	// Fetching a specific commit works too, and a second commit moves HEAD.
	writeTestFile(t, filepath.Join(remote, "plugins", "kit", "extra.txt"), "x", 0o644)
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "--quiet", "-m", "second")
	pinned, err := Prepare(context.Background(), newTestProject(t), Source{Kind: SourceGit, URL: remote, Ref: first, Path: "plugins/kit"}, Options{})
	if err != nil {
		t.Fatalf("Prepare(pinned sha) error = %v", err)
	}
	defer pinned.Close()
	if pinned.Commit != first {
		t.Errorf("pinned Commit = %q, want the requested %q", pinned.Commit, first)
	}
}

func TestGitFetchRefusesAPathOutsideTheRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origProtocols := gitAllowedProtocols
	gitAllowedProtocols = "https:ssh:file"
	t.Cleanup(func() { gitAllowedProtocols = origProtocols })

	remote := t.TempDir()
	runGit(t, remote, "init", "--quiet", "-b", "main")
	writeTestFile(t, filepath.Join(remote, "a.txt"), "a", 0o644)
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "--quiet", "-m", "first")

	_, err := Prepare(context.Background(), newTestProject(t), Source{Kind: SourceGit, URL: remote, Path: "../.."}, Options{})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Errorf("Prepare() with a path escaping the repo error = %v, want it refused", err)
	}
}

// npmServer serves a registry version document and a tarball, and lets a test
// tamper with the tarball after the checksum was computed.
func npmServer(t *testing.T, tarball []byte, advertised []byte, integrityOverride string) *httptest.Server {
	t.Helper()
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tgz") {
			w.Write(tarball)
			return
		}
		sum := sha512.Sum512(advertised)
		integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
		if integrityOverride != "" {
			integrity = integrityOverride
		}
		doc := map[string]any{
			"version": "1.2.3",
			"dist":    map[string]any{"tarball": ts.URL + "/kit/-/kit-1.2.3.tgz", "integrity": integrity},
		}
		json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func withNPMRegistry(t *testing.T, ts *httptest.Server) {
	t.Helper()
	origBase, origClient := npmRegistryBase, httpClient
	npmRegistryBase, httpClient = ts.URL, ts.Client()
	t.Cleanup(func() { npmRegistryBase, httpClient = origBase, origClient })
}

func TestPrepareFromNPMVerifiesTheChecksum(t *testing.T) {
	tarball := buildTarGzBytes(t, "package", map[string]string{
		".claude-plugin/plugin.json": `{"name":"kit"}`,
		".mcp.json":                  `{"mcpServers":{"s":{"command":"echo"}}}`,
	})

	// A good tarball imports, and the exact version becomes the pin.
	good := npmServer(t, tarball, tarball, "")
	withNPMRegistry(t, good)
	plan, err := Prepare(context.Background(), newTestProject(t), Source{Kind: SourceNPM, Package: "@acme/kit"}, Options{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer plan.Close()
	find(t, plan, "mcp", "s")
	if plan.Commit != "1.2.3" {
		t.Errorf("Commit = %q, want the resolved version 1.2.3", plan.Commit)
	}

	// A tarball that doesn't match what the registry advertised is rejected.
	tampered := append(bytes.Clone(tarball), []byte("tampered")...)
	bad := npmServer(t, tampered, tarball, "")
	withNPMRegistry(t, bad)
	if _, err := Prepare(context.Background(), newTestProject(t), Source{Kind: SourceNPM, Package: "@acme/kit"}, Options{}); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Errorf("Prepare() of a tampered tarball error = %v, want an integrity failure", err)
	}

	// A registry that supplies no checksum at all is refused rather than trusted.
	none := npmServer(t, tarball, tarball, "none")
	withNPMRegistry(t, none)
	if _, err := Prepare(context.Background(), newTestProject(t), Source{Kind: SourceNPM, Package: "@acme/kit"}, Options{}); err == nil {
		t.Error("Prepare() with an unverifiable checksum should fail")
	}
	_ = fmt.Sprint
}
