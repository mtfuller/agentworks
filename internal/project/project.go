// Package project loads and scaffolds an AgentWorks project: the
// agentworks.yaml manifest at its root and the agent/skill/tool/hook/
// workflow directories discovered underneath it.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// ManifestFile is the name of a project's root manifest.
const ManifestFile = "agentworks.yaml"

// ErrNotFound is returned by FindRoot when no agentworks.yaml is found.
var ErrNotFound = errors.New("not inside an AgentWorks project (no agentworks.yaml found)")

// Manifest is the content of a project's agentworks.yaml.
type Manifest struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	// Targets lists the vendor targets this project exports to. It is the
	// only place targets are configured -- artifacts don't carry their own
	// -- and `agentworks export` falls back to it when --target isn't given.
	Targets []string `yaml:"targets,omitempty"`
	// Eval is optional project-level eval configuration, currently just a
	// default runner command for `agentworks eval` (see internal/evalspec)
	// when an individual artifact doesn't declare its own "eval_runner".
	// Left unset, an artifact with no runner of its own is skipped.
	Eval *EvalConfig `yaml:"eval,omitempty"`
}

// EvalConfig is a project's default behavior-eval settings.
type EvalConfig struct {
	// DefaultRunner is the shell command `agentworks eval` pipes a case's
	// prompt to when an artifact doesn't declare its own "eval_runner"
	// frontmatter field.
	DefaultRunner string `yaml:"default_runner,omitempty"`
}

// gitignoreTemplate is the .gitignore a new project starts with: regenerable
// export output and dependency/cache directories. agentworks.lock is
// deliberately not listed -- it pins imports and should be committed.
const gitignoreTemplate = `# agentworks export output (regenerate with ` + "`agentworks export`" + `)
dist/

# dependencies and caches inside artifacts
node_modules/
__pycache__/
.venv/

.DS_Store
`

// Init scaffolds a new project at dir: agentworks.yaml plus one directory
// per artifact kind. dir is created if it doesn't exist. It fails if dir
// already contains a manifest.
func Init(dir, name string, targets []string) (*Manifest, error) {
	manifestPath := filepath.Join(dir, ManifestFile)
	if _, err := os.Stat(manifestPath); err == nil {
		return nil, fmt.Errorf("%s already exists", manifestPath)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	for _, k := range artifact.Kinds() {
		if err := os.MkdirAll(filepath.Join(dir, k.DirName()), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", k.DirName(), err)
		}
	}

	if err := writeAgentDocs(dir, name); err != nil {
		return nil, err
	}
	if err := writeIfAbsent(filepath.Join(dir, ".gitignore"), gitignoreTemplate); err != nil {
		return nil, err
	}

	m := &Manifest{Name: name, Targets: targets}
	if err := m.save(manifestPath); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manifest) save(path string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", ManifestFile, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Load reads the manifest at the root of an existing project.
func Load(root string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", ManifestFile, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ManifestFile, err)
	}
	return &m, nil
}

// FindRoot walks upward from start looking for a directory containing
// agentworks.yaml, the same way git finds a repo root from .git.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// Discover walks a project's artifact directories and loads every artifact
// it finds. kinds, if non-empty, restricts discovery to those kinds.
// Artifacts that fail to load are returned in errs rather than aborting the
// whole scan, so one bad file doesn't hide the rest of the project.
//
// Each entry directly under a kind directory (e.g. "skills/<entry>") is
// either an unnamespaced artifact (holds "<kind>.md" itself) or a namespace
// directory one level deep (e.g. "skills/team-a/<name>" -- see
// artifact.Frontmatter.Namespace); Discover checks for the manifest at
// both depths.
func Discover(root string, kinds ...artifact.Kind) (artifacts []*artifact.Artifact, errs []error) {
	if len(kinds) == 0 {
		kinds = artifact.Kinds()
	}

	for _, k := range kinds {
		kindDir := filepath.Join(root, k.DirName())
		entries, err := os.ReadDir(kindDir)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("reading %s: %w", kindDir, err))
			}
			continue
		}

		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)

		for _, name := range names {
			dir := filepath.Join(kindDir, name)
			if _, err := os.Stat(filepath.Join(dir, k.FileName())); err == nil {
				a, err := artifact.Load(dir, k)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				artifacts = append(artifacts, a)
				continue
			}

			// Not an artifact directory itself -- check one level deeper
			// for namespaced artifacts (skills/<namespace>/<name>).
			nsEntries, err := os.ReadDir(dir)
			if err != nil {
				continue // a stray non-directory or unreadable subfolder
			}
			nsNames := make([]string, 0, len(nsEntries))
			for _, e := range nsEntries {
				if e.IsDir() {
					nsNames = append(nsNames, e.Name())
				}
			}
			sort.Strings(nsNames)

			for _, nsName := range nsNames {
				nsDir := filepath.Join(dir, nsName)
				if _, err := os.Stat(filepath.Join(nsDir, k.FileName())); err != nil {
					continue // not an artifact directory, just a stray subfolder
				}
				a, err := artifact.Load(nsDir, k)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				artifacts = append(artifacts, a)
			}
		}
	}
	return artifacts, errs
}

// ResolveArtifactDir turns a bare "name" or namespace-qualified
// "namespace/name" reference (as accepted in a workflow's `steps:` list or
// a CLI path argument) into the directory it should live in.
// filepath.Join handles an embedded "/" in ref identically to a bare name,
// so this is a thin, self-documenting wrapper rather than doing anything
// clever with the two forms.
func ResolveArtifactDir(root string, kind artifact.Kind, ref string) string {
	return filepath.Join(root, kind.DirName(), ref)
}
