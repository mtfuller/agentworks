// Package lockfile is AgentWorks' drift/versioning safety net: a single
// agentworks.lock file at a project's root that pins what `agentworks add`
// actually fetched (so `agentworks update` has something to compare
// against) and what `agentworks export` actually wrote (so a hand-edited
// dist/ can be told apart from a merely stale one).
//
// Unlike a package manager's lockfile, entries aren't pinned to a resolved
// commit SHA -- AgentWorks' GitHub fetches deliberately go through
// codeload tarballs rather than the api.github.com REST API (see
// internal/importer/fetch.go) to avoid rate limits and a token
// requirement, and codeload doesn't hand back the commit a ref resolved
// to. Instead, entries are pinned by a content hash of what was actually
// written to disk, which is what a hand-edit or an upstream change would
// actually change.
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// FileName is the name of the lockfile at a project's root, alongside
// agentworks.yaml.
const FileName = "agentworks.lock"

// currentVersion is bumped if the lockfile's shape ever changes
// incompatibly; Load rejects a file from a newer version than this binary
// understands rather than silently misreading it.
const currentVersion = 1

// SourceRef identifies where an imported artifact's content came from,
// mirroring the fields of importer.Source (this package doesn't import
// importer to avoid a dependency cycle -- importer already sits below
// cmd, which is where both packages meet).
type SourceRef struct {
	Type    string `yaml:"type"` // "github", "git", "npm", or "archive"
	Repo    string `yaml:"repo,omitempty"`
	Ref     string `yaml:"ref,omitempty"`
	Path    string `yaml:"path,omitempty"`
	URL     string `yaml:"url,omitempty"`
	Package string `yaml:"package,omitempty"` // npm
	Version string `yaml:"version,omitempty"` // npm: the version asked for, empty for "latest"
}

// ImportEntry records what `agentworks add` (or `agentworks update`) last
// wrote for one artifact directory.
type ImportEntry struct {
	Kind   string    `yaml:"kind"`
	Source SourceRef `yaml:"source"`
	// Commit is the exact commit the artifact was imported from (for an npm
	// package, the exact version that "latest" or a range resolved to). The
	// ref in Source may be a branch that has since moved; this is what was
	// actually fetched, so `agentworks update` can say what moved.
	Commit string `yaml:"commit,omitempty"`
	// SourceSubpath is this artifact's path relative to Source's own
	// fetched root -- "" for a bare single-artifact import, or e.g.
	// "skills/foo" when Source is a multi-artifact plugin. Lets
	// `agentworks update` find the same spot again after a fresh fetch.
	SourceSubpath string `yaml:"source_subpath,omitempty"`
	ContentSHA256 string `yaml:"content_sha256"`
	// LocalSHA256 is a hash of the artifact directory exactly as
	// `agentworks add`/`update` wrote it. If the directory no longer hashes to
	// this, someone edited it since, and `update --apply` will not overwrite
	// it without --force. Empty for an entry written before this field
	// existed, which is treated as unedited.
	LocalSHA256 string `yaml:"local_sha256,omitempty"`
	Imported    string `yaml:"imported"` // YYYY-MM-DD, UTC
}

// ExportEntry records what `agentworks export` last wrote for one
// (target, artifact) pair. Entries are keyed by ExportKey rather than by
// Output itself, since the output path isn't known until after the
// exporter has already run (and by then it's too late to compare against
// what was there before) -- see ExportKey.
type ExportEntry struct {
	Target   string `yaml:"target"`
	Artifact string `yaml:"artifact"` // source artifact dir(s), project-root-relative; "+"-joined for a bundle
	// Output is where the export was written -- an absolute path, unlike
	// Imports' project-root-relative keys, since --out can point outside
	// the project and dist/ output is a regenerated build artifact, not
	// something pinned for portability across machines/checkouts.
	Output       string `yaml:"output"`
	SourceSHA256 string `yaml:"source_sha256"`
	OutputSHA256 string `yaml:"output_sha256"`
	Exported     string `yaml:"exported"` // YYYY-MM-DD, UTC
}

// ExportKey builds the Exports map key for one (target, artifact) export:
// target plus either a single artifact's project-root-relative dir or a
// bundle name, so the same artifact exported to two different targets (or
// as part of two different bundles) gets independent entries.
func ExportKey(target, artifactOrBundle string) string {
	return target + ":" + artifactOrBundle
}

// Lockfile is the parsed content of a project's agentworks.lock. Both maps
// are keyed by a path relative to the project root (an artifact directory
// for Imports, an export's output path for Exports) -- see RelKey.
type Lockfile struct {
	Version int                    `yaml:"version"`
	Imports map[string]ImportEntry `yaml:"imports,omitempty"`
	Exports map[string]ExportEntry `yaml:"exports,omitempty"`
}

// empty returns a freshly initialized Lockfile, used both for a
// not-yet-created lockfile and as Load's zero value on error.
func empty() *Lockfile {
	return &Lockfile{
		Version: currentVersion,
		Imports: map[string]ImportEntry{},
		Exports: map[string]ExportEntry{},
	}
}

// Load reads the lockfile at root's agentworks.lock. A missing file is not
// an error -- it returns an empty Lockfile, the same "nothing pinned yet"
// state a brand new project starts in.
func Load(root string) (*Lockfile, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return empty(), nil
		}
		return nil, fmt.Errorf("reading %s: %w", FileName, err)
	}

	l := empty()
	if err := yaml.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", FileName, err)
	}
	if l.Imports == nil {
		l.Imports = map[string]ImportEntry{}
	}
	if l.Exports == nil {
		l.Exports = map[string]ExportEntry{}
	}
	if l.Version > currentVersion {
		return nil, fmt.Errorf("%s is version %d, newer than this build of agentworks understands (%d) -- upgrade agentworks", FileName, l.Version, currentVersion)
	}
	if l.Version == 0 {
		l.Version = currentVersion
	}
	return l, nil
}

// Save writes l back to root's agentworks.lock.
func (l *Lockfile) Save(root string) error {
	if l.Version == 0 {
		l.Version = currentVersion
	}
	data, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", FileName, err)
	}
	if err := os.WriteFile(filepath.Join(root, FileName), data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", FileName, err)
	}
	return nil
}

// SetImport records/overwrites the ImportEntry for artifact directory key
// (see RelKey).
func (l *Lockfile) SetImport(key string, entry ImportEntry) {
	if l.Imports == nil {
		l.Imports = map[string]ImportEntry{}
	}
	l.Imports[key] = entry
}

// RemoveImport drops the ImportEntry for key, if any.
func (l *Lockfile) RemoveImport(key string) {
	delete(l.Imports, key)
}

// SetExport records/overwrites the ExportEntry for output path key (see
// RelKey).
func (l *Lockfile) SetExport(key string, entry ExportEntry) {
	if l.Exports == nil {
		l.Exports = map[string]ExportEntry{}
	}
	l.Exports[key] = entry
}

// RemoveExport drops the ExportEntry for key, if any.
func (l *Lockfile) RemoveExport(key string) {
	delete(l.Exports, key)
}

// RelKey turns an absolute (or root-relative) path into the
// project-root-relative, slash-normalized form used as a Lockfile map key,
// so entries are portable across machines/checkouts rather than baking in
// an absolute path.
func RelKey(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("resolving %s relative to %s: %w", path, root, err)
	}
	return filepath.ToSlash(rel), nil
}

// HashDir computes a deterministic content hash of path: if path is a
// directory, every regular file under it (sorted by path relative to it)
// is folded into one sha256 digest over "<relpath>\n<contents>" for each
// file in turn; if path is a single file (as an export's output is when
// --zip was used), its bytes are hashed directly. Either way, the same
// content in the same relative layout always hashes the same, and any
// change to a file's bytes, name, or presence changes the result.
func HashDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return hashFile(path)
	}

	var relPaths []string
	fileHashes := map[string]string{}
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		h, err := hashFile(p)
		if err != nil {
			return err
		}
		relPaths = append(relPaths, rel)
		fileHashes[rel] = h
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	sort.Strings(relPaths)

	tree := sha256.New()
	for _, rel := range relPaths {
		fmt.Fprintf(tree, "%s\n%s\n", rel, fileHashes[rel])
	}
	return hex.EncodeToString(tree.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
