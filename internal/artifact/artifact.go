package artifact

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatterDelim marks the start and end of the YAML frontmatter block at
// the top of a <kind>.md file, same convention as Claude Code's SKILL.md.
const frontmatterDelim = "---"

// namePattern is the slug shape required for an artifact's Name: lowercase
// letters, digits, and hyphens, matching the directory it lives in.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// Frontmatter is the structured metadata at the top of a <kind>.md file.
// Fields common to every kind are named explicitly; kind-specific fields
// (e.g. an mcp server's "entrypoint", a hook's "events") round-trip through Extra
// so the parser doesn't need a separate struct per kind.
type Frontmatter struct {
	Kind        Kind   `yaml:"kind"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version,omitempty"`
	// Namespace optionally scopes an artifact under a team/org prefix (e.g.
	// "team-a"), so two projects (or two teams within one registry) can
	// each have their own "csv-analyzer" without colliding. When set, the
	// artifact lives at "<kind>s/<namespace>/<name>/<kind>.md" instead of
	// "<kind>s/<name>/<kind>.md" -- see Validate and QualifiedName.
	Namespace string         `yaml:"namespace,omitempty"`
	Extra     map[string]any `yaml:",inline"`
}

// ExtraString returns Extra[key] as a string, or "" if unset/not a string.
func (f Frontmatter) ExtraString(key string) string {
	s, _ := f.Extra[key].(string)
	return s
}

// ExtraStringSlice returns Extra[key] as a []string, or nil if unset/not a
// sequence of strings.
func (f Frontmatter) ExtraStringSlice(key string) []string {
	raw, ok := f.Extra[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ExtraStringMap returns Extra[key] as a map[string]string, or nil if
// unset/not a string-to-string mapping. Non-string values are skipped.
func (f Frontmatter) ExtraStringMap(key string) map[string]string {
	raw, ok := f.Extra[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// Artifact is a fully loaded agent, skill, mcp server, or hook: its
// frontmatter, its Markdown body, and where it lives on disk.
type Artifact struct {
	Frontmatter `yaml:",inline"`
	Body        string

	// Dir is the artifact's directory (e.g. "skills/csv-analyzer").
	Dir string
}

// File is the path to the artifact's <kind>.md manifest file.
func (a *Artifact) File() string {
	return filepath.Join(a.Dir, a.Kind.FileName())
}

// QualifiedName is the artifact's full reference string: "namespace/name"
// when Namespace is set, otherwise just Name. This is the form accepted
// back wherever an artifact is referenced by string -- a workflow's
// `steps:` entries, a CLI path argument, a bundle member -- since
// filepath.Join(root, kind.DirName(), qualifiedName) resolves to the right
// directory either way.
func (a *Artifact) QualifiedName() string {
	if a.Namespace == "" {
		return a.Name
	}
	return a.Namespace + "/" + a.Name
}

// DisplayName is QualifiedName in the form users see: "@namespace/name" for a
// namespaced artifact (e.g. one imported from a plugin), else just Name. Only
// QualifiedName ("namespace/name") is a valid on-disk/CLI reference.
func (a *Artifact) DisplayName() string {
	if a.Namespace == "" {
		return a.Name
	}
	return "@" + a.Namespace + "/" + a.Name
}

// Validate checks that an artifact's required fields are present and
// well-formed, independent of any vendor target.
func (a *Artifact) Validate() error {
	var errs []string

	if !a.Kind.Valid() {
		errs = append(errs, fmt.Sprintf("kind %q is not a known artifact kind", a.Kind))
	}
	if a.Name == "" {
		errs = append(errs, "name is required")
	} else if !namePattern.MatchString(a.Name) {
		errs = append(errs, fmt.Sprintf("name %q must be lowercase letters, digits, and hyphens", a.Name))
	}
	if a.Description == "" {
		errs = append(errs, "description is required")
	}
	if a.Namespace != "" && !namePattern.MatchString(a.Namespace) {
		errs = append(errs, fmt.Sprintf("namespace %q must be lowercase letters, digits, and hyphens", a.Namespace))
	}
	if a.Dir != "" {
		if base := filepath.Base(a.Dir); base != a.Name && a.Name != "" {
			errs = append(errs, fmt.Sprintf("name %q does not match its directory %q", a.Name, base))
		}
		if a.Namespace != "" {
			if parent := filepath.Base(filepath.Dir(a.Dir)); parent != a.Namespace {
				errs = append(errs, fmt.Sprintf("namespace %q does not match its parent directory %q", a.Namespace, parent))
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", a.Dir, strings.Join(errs, "; "))
}

// Parse splits raw <kind>.md content into its frontmatter and Markdown body.
func Parse(data []byte) (Frontmatter, string, error) {
	// Strip a UTF-8 BOM if present, rather than embedding the BOM rune
	// literally in source (editors/tools tend to mangle it there).
	bom := []byte{0xEF, 0xBB, 0xBF}
	data = bytes.TrimPrefix(data, bom)
	text := string(data)

	if !strings.HasPrefix(strings.TrimLeft(text, "\r\n"), frontmatterDelim) {
		return Frontmatter{}, "", fmt.Errorf("missing %q frontmatter delimiter at start of file", frontmatterDelim)
	}
	text = strings.TrimLeft(text, "\r\n")
	rest := strings.TrimPrefix(text, frontmatterDelim)

	end := strings.Index(rest, "\n"+frontmatterDelim)
	if end < 0 {
		return Frontmatter{}, "", fmt.Errorf("missing closing %q frontmatter delimiter", frontmatterDelim)
	}
	fmBlock := rest[:end]
	body := rest[end+len("\n"+frontmatterDelim):]
	// Strip every blank line between the closing delimiter and the body,
	// not just one -- Render always emits exactly one, but a hand-edited
	// file may have more, and a lone TrimPrefix would leave the rest in.
	body = strings.TrimLeft(body, "\r\n")

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return Frontmatter{}, "", fmt.Errorf("parsing frontmatter: %w", err)
	}
	return fm, body, nil
}

// Render serializes a Frontmatter + body back into <kind>.md file content.
func Render(fm Frontmatter, body string) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return nil, fmt.Errorf("encoding frontmatter: %w", err)
	}
	_ = enc.Close()

	out := frontmatterDelim + "\n" + buf.String() + frontmatterDelim + "\n"
	if body != "" {
		out += "\n" + strings.TrimLeft(body, "\n")
	}
	return []byte(out), nil
}

// Load reads and parses the <kind>.md file for the artifact in dir.
func Load(dir string, kind Kind) (*Artifact, error) {
	path := filepath.Join(dir, kind.FileName())
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	fm, body, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &Artifact{Frontmatter: fm, Body: body, Dir: dir}, nil
}

// LoadFile loads an artifact by the path to its <kind>.md file directly,
// inferring the kind from the file name.
func LoadFile(path string) (*Artifact, error) {
	base := filepath.Base(path)
	kind, err := ParseKind(strings.TrimSuffix(base, ".md"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return Load(filepath.Dir(path), kind)
}

// Save writes the artifact back to its <kind>.md file, creating Dir if
// needed.
func (a *Artifact) Save() error {
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", a.Dir, err)
	}
	data, err := Render(a.Frontmatter, a.Body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.File(), data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", a.File(), err)
	}
	return nil
}
