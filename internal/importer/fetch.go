package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// httpClient is package-level so tests can point it at an httptest.Server
// with a custom Transport; a bare http.Get has no timeout and would hang
// the CLI/TUI forever against a stalled server.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// maxDownloadBytes caps the raw archive download, ahead of and consistent
// with filecopy's own extraction-time cap on uncompressed content.
const maxDownloadBytes = 64 << 20

var errNotFound = errors.New("not found")

// Prepare fetches src into a fresh temp directory and resolves it into a
// Plan. Callers must call Plan.Close() (typically via defer, right after a
// successful Prepare) to remove the temp directory regardless of what
// Apply() does afterward.
func Prepare(ctx context.Context, root string, src Source, opts Options) (*Plan, error) {
	tempDir, err := os.MkdirTemp("", "agentworks-import-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	contentDir, err := Fetch(ctx, src, tempDir)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}

	plan, err := detect(root, src, contentDir, opts)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}
	plan.tempDir = tempDir
	return plan, nil
}

// Fetch downloads src into destDir and returns the directory that actually
// holds the content, after unwrapping a single top-level directory (as
// GitHub's tarballs always have) and descending into src.Path, if set.
func Fetch(ctx context.Context, src Source, destDir string) (string, error) {
	archivePath := filepath.Join(destDir, "archive")
	extractDir := filepath.Join(destDir, "content")

	switch src.Kind {
	case SourceGitHub:
		if err := fetchGitHub(ctx, src, archivePath); err != nil {
			return "", err
		}
	case SourceArchive:
		if err := download(ctx, src.URL, archivePath, ""); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported source kind %q", src.Kind)
	}

	if err := extractArchive(archivePath, extractDir); err != nil {
		return "", err
	}

	contentDir, err := stripSingleRoot(extractDir)
	if err != nil {
		return "", err
	}
	if src.Path != "" {
		contentDir = filepath.Join(contentDir, src.Path)
		if _, err := os.Stat(contentDir); err != nil {
			return "", fmt.Errorf("%s: path %q not found: %w", src, src.Path, err)
		}
	}
	return contentDir, nil
}

// fetchGitHub downloads a repo as a codeload tarball -- no local git
// binary, no api.github.com rate limits, and plain ref names (branch, tag,
// or commit SHA) all work identically. With no ref given, main is tried
// first and master second, since codeload 404s rather than resolving a
// repo's actual default branch for you.
func fetchGitHub(ctx context.Context, src Source, archivePath string) error {
	if src.Ref != "" {
		return fetchRef(ctx, src.Repo, src.Ref, archivePath)
	}
	err := fetchRef(ctx, src.Repo, "main", archivePath)
	if errors.Is(err, errNotFound) {
		return fetchRef(ctx, src.Repo, "master", archivePath)
	}
	return err
}

// fetchRef downloads repo@ref's tarball. It tries codeload unauthenticated
// first -- fast, no rate limit, works for any public repo -- and only
// reaches for a token and GitHub's authenticated tarball API when that 404s
// and a token is actually available, so a public-repo fetch never pays for
// token resolution (which may shell out to gh) at all.
func fetchRef(ctx context.Context, repo, ref, archivePath string) error {
	err := download(ctx, codeloadURL(repo, ref), archivePath, "")
	if !errors.Is(err, errNotFound) {
		return err
	}
	tok := githubToken()
	if tok == "" {
		return err
	}
	return download(ctx, tarballAPIURL(repo, ref), archivePath, tok)
}

// codeloadBase is a var, not a const, so tests can redirect it to an
// httptest.Server instead of the real codeload.github.com.
var codeloadBase = "https://codeload.github.com"

func codeloadURL(repo, ref string) string {
	return fmt.Sprintf("%s/%s/tar.gz/%s", codeloadBase, repo, ref)
}

// githubAPIBase is a var, not a const, so tests can redirect it to an
// httptest.Server instead of the real api.github.com.
var githubAPIBase = "https://api.github.com"

// tarballAPIURL is GitHub's authenticated tarball endpoint -- unlike
// codeload, it accepts a bearer token and works for private repos. It
// responds with a redirect to a signed codeload URL; Go's http.Client
// follows it automatically and strips the Authorization header since the
// redirect target is a different host, which is exactly what's wanted since
// the signed URL doesn't need (or want) the token repeated at it.
func tarballAPIURL(repo, ref string) string {
	return fmt.Sprintf("%s/repos/%s/tarball/%s", githubAPIBase, repo, ref)
}

// ghAuthToken runs `gh auth token`, capturing an already-authenticated
// GitHub CLI's token. It's a var so tests can substitute a fake without
// requiring gh to be installed.
var ghAuthToken = func() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// githubToken resolves a token for authenticating to a private GitHub repo:
// GITHUB_TOKEN or GH_TOKEN first (the conventions gh and GitHub Actions
// already use), then a token from an already-logged-in gh CLI. Empty means
// unauthenticated -- fetchRef only calls this once the unauthenticated
// codeload request 404s.
func githubToken() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	if tok := os.Getenv("GH_TOKEN"); tok != "" {
		return tok
	}
	tok, err := ghAuthToken()
	if err != nil {
		return ""
	}
	return tok
}

// download fetches url into destFile, sending an Authorization header when
// token is non-empty. A plain http.Get does not return an error on a 404 --
// without this explicit status check, a GitHub 404 HTML page would reach
// the gzip reader and fail as "invalid header" instead of the much clearer
// "not found" this returns.
func download(ctx context.Context, url, destFile, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected response %s", url, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(destFile), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destFile)
	if err != nil {
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	if written > maxDownloadBytes {
		return fmt.Errorf("%s: response exceeds %d bytes", url, maxDownloadBytes)
	}
	return nil
}

// extractArchive sniffs archivePath's magic bytes rather than trusting a
// URL's extension -- agentskills.codes's download-by-id endpoint, for
// instance, has none.
func extractArchive(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	var magic [4]byte
	n, _ := io.ReadFull(f, magic[:])
	f.Close()

	switch {
	case n >= 4 && magic[0] == 'P' && magic[1] == 'K' && magic[2] == 0x03 && magic[3] == 0x04:
		return filecopy.Unzip(archivePath, destDir)
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		return filecopy.UntarGz(archivePath, destDir)
	default:
		return fmt.Errorf("%s: not a recognized zip or gzip archive", archivePath)
	}
}

// stripSingleRoot returns the path to unwrap into when dir contains
// exactly one entry and it's a directory (GitHub's tarballs always wrap
// everything in a single "<repo>-<ref>/" folder); otherwise dir itself.
func stripSingleRoot(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(dir, entries[0].Name()), nil
	}
	return dir, nil
}
