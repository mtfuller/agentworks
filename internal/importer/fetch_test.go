package importer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildZipBytes returns an in-memory zip archive containing the given
// entries (path -> content), with no single top-level wrapping directory.
func buildZipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zw.Create(%q) error = %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("writing %q error = %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close() error = %v", err)
	}
	return buf.Bytes()
}

// buildTarGzBytes returns an in-memory gzip-compressed tar archive, with
// every entry namespaced under a single "root/" directory -- mirroring how
// GitHub's codeload tarballs always wrap content in "<repo>-<ref>/".
func buildTarGzBytes(t *testing.T, root string, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		full := root + "/" + name
		if err := tw.WriteHeader(&tar.Header{Name: full, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", full, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing %q error = %v", full, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close() error = %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz.Close() error = %v", err)
	}
	return buf.Bytes()
}

// withTestServer swaps httpClient and codeloadBase to point at ts for the
// duration of the test, restoring both afterward. Redirecting codeloadBase
// unconditionally is harmless for archive-source tests (which fetch
// src.URL directly and never consult it) and is what lets github-source
// tests reach the fake server at all, since a Source only carries a repo
// and ref, not a full URL.
func withTestServer(t *testing.T, ts *httptest.Server) {
	t.Helper()
	origClient, origBase := httpClient, codeloadBase
	httpClient = ts.Client()
	codeloadBase = ts.URL
	t.Cleanup(func() {
		httpClient = origClient
		codeloadBase = origBase
	})
}

func TestFetchArchiveZip(t *testing.T) {
	body := buildZipBytes(t, map[string]string{"SKILL.md": "---\nname: x\n---\n"})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer ts.Close()
	withTestServer(t, ts)

	dir, err := Fetch(context.Background(), Source{Kind: SourceArchive, URL: ts.URL + "/skill.zip"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not extracted: %v", err)
	}
}

func TestFetchSniffsTarGzWithNoExtension(t *testing.T) {
	body := buildTarGzBytes(t, "repo-main", map[string]string{"SKILL.md": "---\nname: x\n---\n"})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No extension and no Content-Type hint -- Fetch must sniff magic bytes.
		w.Write(body)
	}))
	defer ts.Close()
	withTestServer(t, ts)

	dir, err := Fetch(context.Background(), Source{Kind: SourceArchive, URL: ts.URL + "/api/skills/download/42"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not extracted: %v", err)
	}
}

func TestFetchStripsSingleRootDir(t *testing.T) {
	body := buildTarGzBytes(t, "repo-main", map[string]string{
		"SKILL.md":        "---\nname: x\n---\n",
		"scripts/main.py": "print(1)",
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer ts.Close()
	withTestServer(t, ts)

	dir, err := Fetch(context.Background(), Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	// dir should be .../content/repo-main, not .../content itself.
	if filepath.Base(dir) != "repo-main" {
		t.Errorf("Fetch() dir = %q, want it to end in repo-main (single root stripped)", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found after stripping root: %v", err)
	}
}

func TestFetchDescendsIntoPath(t *testing.T) {
	body := buildTarGzBytes(t, "repo-main", map[string]string{
		"skills/pdf/SKILL.md": "---\nname: pdf\n---\n",
		"README.md":           "not the skill",
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer ts.Close()
	withTestServer(t, ts)

	dir, err := Fetch(context.Background(), Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main", Path: "skills/pdf"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found at the descended path: %v", err)
	}
}

func TestFetchPathNotFound(t *testing.T) {
	body := buildTarGzBytes(t, "repo-main", map[string]string{"SKILL.md": "x"})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer ts.Close()
	withTestServer(t, ts)

	_, err := Fetch(context.Background(), Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main", Path: "does/not/exist"}, t.TempDir())
	if err == nil {
		t.Fatal("Fetch() with a nonexistent path expected error, got nil")
	}
}

func TestFetchNon200ReportsStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("<html>oops</html>"))
	}))
	defer ts.Close()
	withTestServer(t, ts)

	_, err := Fetch(context.Background(), Source{Kind: SourceArchive, URL: ts.URL + "/skill.zip"}, t.TempDir())
	if err == nil {
		t.Fatal("Fetch() against a 500 response expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to name the HTTP status rather than fail later as an archive-parsing error", err)
	}
	if strings.Contains(err.Error(), "gzip:") || strings.Contains(err.Error(), "not a recognized") {
		t.Errorf("error = %v, want a clear HTTP-status error, not an archive-parsing error", err)
	}
}

func TestFetchGitHubFallsBackToMaster(t *testing.T) {
	body := buildTarGzBytes(t, "repo-master", map[string]string{"SKILL.md": "x"})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tar.gz/main") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(body)
	}))
	defer ts.Close()
	withTestServer(t, ts)

	dir, err := Fetch(context.Background(), Source{Kind: SourceGitHub, Repo: "owner/repo"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch() error = %v, want it to fall back to master", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found after master fallback: %v", err)
	}
}

// withAuthTestServer additionally points githubAPIBase at ts, for tests of
// the authenticated-tarball fallback.
func withAuthTestServer(t *testing.T, ts *httptest.Server) {
	t.Helper()
	withTestServer(t, ts)
	origAPIBase := githubAPIBase
	githubAPIBase = ts.URL
	t.Cleanup(func() { githubAPIBase = origAPIBase })
}

// stubGHToken makes githubToken() return tok without touching the real
// environment or shelling out to gh, and restores both paths afterward.
func stubGHToken(t *testing.T, tok string) {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	origGH := ghAuthToken
	ghAuthToken = func() (string, error) { return tok, nil }
	t.Cleanup(func() { ghAuthToken = origGH })
}

func TestFetchGitHubFallsBackToAuthedAPIWhenTokenAvailable(t *testing.T) {
	body := buildTarGzBytes(t, "repo-main", map[string]string{"SKILL.md": "x"})
	var sawAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/repos/owner/repo/tarball/main") {
			sawAuth = r.Header.Get("Authorization")
			w.Write(body)
			return
		}
		// Unauthenticated codeload path: simulate a private repo 404.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	withAuthTestServer(t, ts)
	stubGHToken(t, "secret-token")

	dir, err := Fetch(context.Background(), Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch() error = %v, want it to fall back to the authenticated tarball API", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not found after authenticated fallback: %v", err)
	}
	if sawAuth != "Bearer secret-token" {
		t.Errorf("Authorization header = %q, want Bearer secret-token", sawAuth)
	}
}

func TestFetchGitHubReportsNotFoundWithoutToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	withAuthTestServer(t, ts)
	stubGHToken(t, "")

	_, err := Fetch(context.Background(), Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main"}, t.TempDir())
	if !errors.Is(err, errNotFound) {
		t.Errorf("Fetch() error = %v, want errNotFound with no token available", err)
	}
}

func TestFetchRespectsContextCancellation(t *testing.T) {
	block := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer ts.Close()
	defer close(block)
	withTestServer(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Fetch(ctx, Source{Kind: SourceArchive, URL: ts.URL + "/skill.zip"}, t.TempDir())
	if err == nil {
		t.Fatal("Fetch() with a cancelled context expected error, got nil")
	}
}

// GitHub names a tarball's root "<repo>-<commit sha>", which is how a fetch
// learns what a moving ref (a branch, "latest") actually resolved to.
func TestPrepareRecordsTheResolvedCommit(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	body := buildTarGzBytes(t, "demo-"+sha, map[string]string{
		"SKILL.md": "---\nname: pinned\ndescription: A skill used to check the commit is recorded.\n---\n\nbody\n",
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer ts.Close()
	withTestServer(t, ts)

	root := newTestProject(t)
	plan, err := Prepare(context.Background(), root, Source{Kind: SourceGitHub, Repo: "owner/demo", Ref: "main"}, Options{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer plan.Close()

	if plan.Commit != sha {
		t.Errorf("Commit = %q, want the sha from the tarball's root directory", plan.Commit)
	}
	prov, _ := plan.Artifacts[0].Extra["source"].(map[string]any)
	if prov["commit"] != sha {
		t.Errorf("provenance = %v, want the commit recorded in the artifact's source block", prov)
	}
}

func TestPrepareOfAnArchiveURLHasNoCommit(t *testing.T) {
	body := buildTarGzBytes(t, "kit-main", map[string]string{
		"SKILL.md": "---\nname: plain\ndescription: A skill from an archive URL with no commit to record.\n---\n\nbody\n",
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer ts.Close()
	withTestServer(t, ts)

	plan, err := Prepare(context.Background(), newTestProject(t), Source{Kind: SourceArchive, URL: ts.URL + "/kit.tar.gz"}, Options{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer plan.Close()
	if plan.Commit != "" {
		t.Errorf("Commit = %q, want none for an archive URL", plan.Commit)
	}
}
