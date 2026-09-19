package importer

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ---- generic git ----------------------------------------------------------

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// gitAllowedProtocols is the value of GIT_ALLOW_PROTOCOL for a clone. It is a
// var only so tests can add "file" to clone from a local repository.
var gitAllowedProtocols = "https:ssh"

// fetchGitSource clones a git remote with the local git binary and returns the
// content directory (with any src.Path applied) and the commit HEAD resolved
// to. The clone is shallow, and its .git directory is removed so the content
// hashes deterministically and never gets copied into a project.
//
// Two hardening measures matter because the URL is user- (or marketplace-)
// supplied: transports are restricted to https and ssh (git's "ext::" helper
// runs commands), and credential prompts are disabled so a private repo fails
// fast instead of hanging.
func fetchGitSource(ctx context.Context, src Source, destDir string) (contentDir, commit string, err error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", "", fmt.Errorf("importing from %s needs the git command on PATH", src.URL)
	}
	repoDir := filepath.Join(destDir, "content")

	run := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, gitBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ALLOW_PROTOCOL="+gitAllowedProtocols,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args[:1], " "), err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", "", err
	}
	switch {
	case shaPattern.MatchString(src.Ref):
		// A commit can't be cloned by name; fetch exactly it.
		if _, err := run(repoDir, "init", "--quiet"); err != nil {
			return "", "", err
		}
		if _, err := run(repoDir, "fetch", "--quiet", "--depth", "1", "--", src.URL, src.Ref); err != nil {
			return "", "", err
		}
		if _, err := run(repoDir, "checkout", "--quiet", "FETCH_HEAD"); err != nil {
			return "", "", err
		}
	default:
		args := []string{"clone", "--quiet", "--depth", "1"}
		if src.Ref != "" {
			args = append(args, "--branch", src.Ref)
		}
		args = append(args, "--", src.URL, repoDir)
		if _, err := run(destDir, args...); err != nil {
			return "", "", err
		}
	}

	commit, err = run(repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	if err := os.RemoveAll(filepath.Join(repoDir, ".git")); err != nil {
		return "", "", err
	}

	contentDir = repoDir
	if src.Path != "" {
		contentDir = filepath.Join(repoDir, filepath.FromSlash(src.Path))
		if rel, err := filepath.Rel(repoDir, contentDir); err != nil || strings.HasPrefix(rel, "..") {
			return "", "", fmt.Errorf("%s: path %q escapes the repository", src, src.Path)
		}
		if _, err := os.Stat(contentDir); err != nil {
			return "", "", fmt.Errorf("%s: path %q not found: %w", src, src.Path, err)
		}
	}
	return contentDir, commit, nil
}

// ---- npm ------------------------------------------------------------------

// npmRegistryBase is a var so tests can point it at an httptest.Server.
var npmRegistryBase = "https://registry.npmjs.org"

// npmVersionDoc is the part of the registry's version document used here.
type npmVersionDoc struct {
	Version string `json:"version"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"` // SRI, e.g. "sha512-<base64>"
		Shasum    string `json:"shasum"`    // hex sha1, for old packages
	} `json:"dist"`
}

// fetchNPM downloads a package's tarball from the registry into archivePath and
// returns the exact version it resolved to. The download is verified against
// the registry's own checksum (sha512 integrity, or sha1 shasum for old
// packages), so a corrupted or swapped tarball is rejected.
func fetchNPM(ctx context.Context, src Source, archivePath string) (version string, err error) {
	selector := src.Version
	if selector == "" {
		selector = "latest"
	}
	// A scoped name's slash is escaped in the registry path (@scope%2Fname).
	metaURL := fmt.Sprintf("%s/%s/%s", npmRegistryBase, url.PathEscape(src.Package), url.PathEscape(selector))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", metaURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("npm package %s not found (%s)", src, metaURL)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: unexpected response %s", metaURL, resp.Status)
	}
	var doc npmVersionDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("%s: %w", metaURL, err)
	}
	if doc.Dist.Tarball == "" || doc.Version == "" {
		return "", fmt.Errorf("%s: the registry returned no tarball for %s (a version range isn't supported; give an exact version or none)", metaURL, src)
	}

	if err := download(ctx, doc.Dist.Tarball, archivePath, ""); err != nil {
		return "", err
	}
	if err := verifyNPMIntegrity(archivePath, doc); err != nil {
		os.Remove(archivePath)
		return "", fmt.Errorf("%s@%s: %w", src.Package, doc.Version, err)
	}
	return doc.Version, nil
}

func verifyNPMIntegrity(path string, doc npmVersionDoc) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if algo, sri, ok := strings.Cut(doc.Dist.Integrity, "-"); ok && algo == "sha512" {
		sum := sha512.Sum512(data)
		if base64.StdEncoding.EncodeToString(sum[:]) != sri {
			return fmt.Errorf("the downloaded tarball does not match the registry's sha512 integrity checksum")
		}
		return nil
	}
	if doc.Dist.Shasum != "" {
		sum := sha1.Sum(data)
		if hex.EncodeToString(sum[:]) != strings.ToLower(doc.Dist.Shasum) {
			return fmt.Errorf("the downloaded tarball does not match the registry's sha1 checksum")
		}
		return nil
	}
	return fmt.Errorf("the registry supplied no checksum to verify the tarball against")
}
