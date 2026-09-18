// Package importer is the reverse of internal/targets: instead of turning
// an AgentWorks artifact into a vendor's native format, it turns a fetched
// skill or Claude Code plugin back into one or more local artifacts.
//
// importer never imports internal/marketplace -- marketplace depends on
// importer for Source (a marketplace search result resolves to one), and
// the reverse would be a cycle.
package importer

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// SourceKind is where a Source's content is actually fetched from.
type SourceKind string

const (
	// SourceGitHub fetches a GitHub repo (optionally a ref and a subpath
	// within it) as a codeload tarball -- no local git binary, no
	// api.github.com rate limits.
	SourceGitHub SourceKind = "github"
	// SourceArchive fetches a direct zip or tar.gz URL (or a URL with no
	// extension whose response sniffs as one, e.g. agentskills.codes's
	// download-by-id endpoint).
	SourceArchive SourceKind = "archive"
)

// Source is a resolved fetch target. A bare SKILL.md/raw-file URL is
// deliberately not its own kind: ParseAddArgument rewrites a raw file URL
// into a SourceGitHub with Path set to the file's containing directory, so
// the whole skill directory (and its supporting files) comes along rather
// than just one file.
type Source struct {
	Kind SourceKind

	// Repo, Ref, Path apply to SourceGitHub. Ref is a branch, tag, or
	// commit SHA; empty means "try main, then master." Path is a subdir
	// within the repo to treat as the content root; empty means the repo
	// root.
	Repo string
	Ref  string
	Path string

	// URL applies to SourceArchive.
	URL string
}

func (s Source) String() string {
	switch s.Kind {
	case SourceGitHub:
		ref := s.Ref
		if ref == "" {
			ref = "default branch"
		}
		if s.Path != "" {
			return fmt.Sprintf("github.com/%s@%s/%s", s.Repo, ref, s.Path)
		}
		return fmt.Sprintf("github.com/%s@%s", s.Repo, ref)
	case SourceArchive:
		return s.URL
	default:
		return string(s.Kind)
	}
}

// DefaultNamespace is the namespace artifacts imported from s are filed
// under: the GitHub owner ("obra" for obra/superpowers), or for a bare
// archive URL the first label of its host. Empty only if neither yields a
// usable slug.
func (s Source) DefaultNamespace() string {
	switch s.Kind {
	case SourceGitHub:
		owner, _, _ := strings.Cut(s.Repo, "/")
		if slug, err := slugify(owner); err == nil {
			return slug
		}
	case SourceArchive:
		if u, err := url.Parse(s.URL); err == nil {
			host, _, _ := strings.Cut(strings.TrimPrefix(u.Hostname(), "www."), ".")
			if slug, err := slugify(host); err == nil {
				return slug
			}
		}
	}
	return "imported"
}

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts an arbitrary display name (as real marketplace entries
// use -- "Azure DevOps", "PDF_Tools") into the lowercase-letters-digits-
// hyphens shape artifact.Validate requires for both an artifact's Name and
// its directory basename.
func slugify(s string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(s))
	slug := strings.Trim(slugInvalid.ReplaceAllString(lower, "-"), "-")
	if slug == "" {
		return "", fmt.Errorf("%q has no usable characters for a name", s)
	}
	return slug, nil
}

// ParseAddArgument turns a CLI argument into a Source: an "owner/repo"
// shorthand, a full GitHub repo/tree/blob URL, a raw.githubusercontent.com
// file URL, or a direct archive URL (.zip/.tar.gz/.tgz, or an
// agentskills.codes download link). Anything else -- gitlab, bitbucket,
// npm, a bare git URL -- is rejected with a message naming what's actually
// supported, rather than failing later with a confusing download error.
func ParseAddArgument(arg string) (Source, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return Source{}, fmt.Errorf("empty source")
	}

	if isBareRepoShorthand(arg) {
		return Source{Kind: SourceGitHub, Repo: arg}, nil
	}

	u, err := url.Parse(arg)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Source{}, fmt.Errorf("%q doesn't look like a URL or an \"owner/repo\" shorthand", arg)
	}

	switch u.Host {
	case "github.com":
		return parseGitHubURL(u)
	case "raw.githubusercontent.com":
		return parseRawGitHubURL(u)
	case "gitlab.com", "bitbucket.org":
		return Source{}, fmt.Errorf("%s isn't supported yet -- only GitHub-hosted sources and direct .zip/.tar.gz archive URLs are", u.Host)
	}

	lower := strings.ToLower(u.Path)
	if strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return Source{Kind: SourceArchive, URL: arg}, nil
	}
	if u.Host == "agentskills.codes" && strings.HasPrefix(u.Path, "/api/skills/download/") {
		return Source{Kind: SourceArchive, URL: arg}, nil
	}

	return Source{}, fmt.Errorf("%q doesn't look like a supported source (a GitHub repo, a direct .zip/.tar.gz URL, or an agentskills.codes download link)", arg)
}

func isBareRepoShorthand(s string) bool {
	if strings.Contains(s, "://") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return false
	}
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func parseGitHubURL(u *url.URL) (Source, error) {
	segs := splitPath(u.Path)
	if len(segs) < 2 {
		return Source{}, fmt.Errorf("%s: expected /owner/repo", u.String())
	}
	repo := segs[0] + "/" + strings.TrimSuffix(segs[1], ".git")
	src := Source{Kind: SourceGitHub, Repo: repo}
	if len(segs) == 2 {
		return src, nil
	}

	if len(segs) < 4 || (segs[2] != "tree" && segs[2] != "blob") {
		return Source{}, fmt.Errorf("%s: expected /owner/repo/tree/<ref>/<path> or /owner/repo/blob/<ref>/<path>", u.String())
	}
	src.Ref = segs[3]
	p := strings.Join(segs[4:], "/")
	if segs[2] == "blob" {
		p = pathDir(p)
	}
	src.Path = p
	return src, nil
}

func parseRawGitHubURL(u *url.URL) (Source, error) {
	segs := splitPath(u.Path)
	if len(segs) < 3 {
		return Source{}, fmt.Errorf("%s: expected /owner/repo/<ref>/<path>", u.String())
	}
	return Source{
		Kind: SourceGitHub,
		Repo: segs[0] + "/" + segs[1],
		Ref:  segs[2],
		Path: pathDir(strings.Join(segs[3:], "/")),
	}, nil
}

func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pathDir is path.Dir, except a file with no directory component ("/x")
// yields "" (the content root) rather than ".".
func pathDir(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}
