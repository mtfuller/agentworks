// Package targets is AgentWorks' vendor registry: which AI harnesses
// (targets) AgentWorks knows about, which artifact kinds each one can
// consume, and the Exporter that turns an artifact into that vendor's
// native format.
package targets

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// Target describes one vendor AI harness AgentWorks can export to.
type Target struct {
	// ID is the stable identifier used in frontmatter `targets:` lists and
	// the --target flag, e.g. "claude-code".
	ID   string
	Name string
	// Supports lists the artifact kinds this vendor can consume in
	// principle. It does not imply an Exporter is registered yet — see
	// GetExporter.
	Supports []artifact.Kind
	Notes    string
	// Format names the vendor format an export is written against and
	// Verified is when it was last checked against the real tool (or its
	// published schema), as YYYY-MM-DD. Vendor formats move; a stale date is
	// the cue to rerun the conformance suite (see COMPATIBILITY.md).
	Format   string
	Verified string
}

// registry is the static set of vendors AgentWorks knows about. Adding a
// target here makes it show up in `agentworks targets` and become a valid
// --target value immediately; a real Exporter can land separately.
var registry = []Target{
	{
		ID:       "claude-code",
		Format:   "Claude Code plugin (.claude-plugin/plugin.json), checked with `claude plugin validate --strict`",
		Verified: "2026-09-18",
		Name:     "Claude Code",
		Supports: artifact.Kinds(),
		Notes:    "Every kind has a real exporter: skills (Agent Skills format), tools (MCP server), agents and hooks (each a small plugin).",
	},
	{
		ID:       "chatgpt",
		Format:   "Agent Skills archive (.skill / skills.zip)",
		Verified: "2026-09-18",
		Name:     "ChatGPT",
		Supports: []artifact.Kind{artifact.KindSkill},
		Notes:    "Skill uploads only, by design -- see AGENTS.md, \"ChatGPT: why skills only.\"",
	},
	{
		ID:       "github-copilot",
		Format:   "Agent Plugins 1.0.0 (plugin.json + mcp.json JSON Schemas, vendored in internal/targets/testdata/agentplugins)",
		Verified: "2026-09-18",
		Name:     "GitHub Copilot",
		Supports: []artifact.Kind{artifact.KindAgent, artifact.KindSkill, artifact.KindMCP, artifact.KindHook},
		Notes:    "Every supported kind has a real exporter: skills, tools (MCP server via Agent Plugins' mcp.json), agents and hooks (com.github.copilot/ namespace).",
	},
	{
		ID:       "cursor",
		Format:   "Loose project files: .cursor/rules, agents, mcp.json, hooks.json",
		Verified: "2026-09-18",
		Name:     "Cursor",
		Supports: []artifact.Kind{artifact.KindSkill, artifact.KindAgent, artifact.KindMCP, artifact.KindHook},
		Notes:    "Every supported kind has a real exporter, but none are plugins -- Cursor has no bundle/plugin format, so each is a loose project-scoped file: skills as a project rule (.cursor/rules/<name>.mdc), agents as a real subagent file (.cursor/agents/<name>.md, name/description only -- see AGENTS.md), tools as .cursor/mcp.json, hooks as .cursor/hooks.json.",
	},
	{
		ID:       "gemini-cli",
		Format:   "Gemini CLI extension (gemini-extension.json) + .gemini/agents + settings.json fragment",
		Verified: "2026-09-18",
		Name:     "Gemini CLI",
		Supports: []artifact.Kind{artifact.KindSkill, artifact.KindAgent, artifact.KindMCP, artifact.KindHook},
		Notes:    "Skills and tools export as a Gemini CLI extension (gemini-extension.json, + GEMINI.md and supporting files for a skill), agents as a real subagent file (.gemini/agents/<name>.md, with real tools:/model: mapping -- see AGENTS.md), hooks as a .gemini/settings.json fragment meant to be merged by hand (Gemini CLI hooks live only in settings.json, not an extension-scoped format).",
	},
}

// All returns every known target, in registration order.
func All() []Target {
	out := make([]Target, len(registry))
	copy(out, registry)
	return out
}

// Get looks up a target by ID.
func Get(id string) (Target, bool) {
	for _, t := range registry {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// Supports reports whether target id can, in principle, consume artifacts
// of the given kind.
func Supports(id string, kind artifact.Kind) bool {
	t, ok := Get(id)
	if !ok {
		return false
	}
	for _, k := range t.Supports {
		if k == kind {
			return true
		}
	}
	return false
}

// UnsupportedReason explains why target id can't export artifact a even though
// it supports a's kind, or returns "" if it can. Exporters skip such an
// artifact with a warning rather than emitting output that can't work.
//
// Today that is a hook whose command uses ${ARTIFACT_DIR} to run a bundled
// script: claude-code ships the files and resolves the path via
// ${CLAUDE_PLUGIN_ROOT}, but no other target documents a plugin-root variable
// (github-copilot's hooks docs name none) or a place for a hook's files.
func UnsupportedReason(id string, a *artifact.Artifact) string {
	if a.Kind == artifact.KindHook && id != "claude-code" && a.UsesArtifactDir() {
		return "its command runs a bundled script (" + artifact.ArtifactDirVar + "), which " + id + " has no way to locate"
	}
	return ""
}

// ExportOptions customizes an export run.
type ExportOptions struct {
	// Zip additionally packages the export output as a .zip alongside the
	// exported directory.
	Zip bool
	// Meta describes the publisher, for the plugin manifests that carry it.
	Meta PluginMeta
}

// Author is who published a plugin.
type Author struct {
	Name  string
	Email string
	URL   string
}

// IsZero reports whether no author details are set.
func (a Author) IsZero() bool { return a == Author{} }

// PluginMeta is the publisher information a project supplies (in
// agentworks.yaml) for the plugins it exports. Every field is optional.
type PluginMeta struct {
	// Version is the plugin's version. When empty, a plugin built from one
	// artifact uses that artifact's version, and a bundle uses DefaultVersion.
	Version    string
	Author     Author
	License    string
	Homepage   string
	Repository string
}

// DefaultVersion is the version of a bundle plugin when neither the project
// nor anything else names one, matching an artifact's own default.
const DefaultVersion = "0.1.0"

// VersionFor picks the version to write into a plugin manifest.
func (m PluginMeta) VersionFor(artifactVersion string) string {
	switch {
	case m.Version != "":
		return m.Version
	case artifactVersion != "":
		return artifactVersion
	}
	return DefaultVersion
}

var pluginNameInvalid = regexp.MustCompile(`[^a-z0-9.-]+`)

// PluginName converts s to a name every plugin format accepts: lowercase
// letters, digits, hyphens, and periods, starting and ending alphanumeric, no
// "--" or "..", at most 64 characters. Agent Plugins' schema is strict about
// this (a project directory called "My Project" would otherwise export a
// manifest the schema rejects). An input that yields nothing becomes "plugin".
func PluginName(s string) string {
	name := pluginNameInvalid.ReplaceAllString(strings.ToLower(s), "-")
	for strings.Contains(name, "--") || strings.Contains(name, "..") {
		name = strings.ReplaceAll(strings.ReplaceAll(name, "--", "-"), "..", ".")
	}
	name = strings.Trim(name, "-.")
	if len(name) > 64 {
		name = strings.Trim(name[:64], "-.")
	}
	if name == "" {
		return "plugin"
	}
	return name
}

// Exporter turns an artifact into a target's native on-disk format under
// outDir, returning the path it wrote.
type Exporter interface {
	// TargetID is the Target.ID this exporter implements.
	TargetID() string
	Export(a *artifact.Artifact, outDir string, opts ExportOptions) (string, error)
}

// BundleExporter is an optional capability an Exporter may also implement:
// packaging a *set* of artifacts into one plugin, for targets whose native
// format is actually meant to bundle several components together (agents,
// skills, tools, hooks) rather than ship one component per package. Callers
// type-assert an Exporter to this (the same "optional interface" pattern as
// io.ReaderFrom) and report a clear error if a target doesn't implement it.
type BundleExporter interface {
	ExportBundle(name, description string, artifacts []*artifact.Artifact, outDir string, opts ExportOptions) (string, error)
}

// FlatNames picks the single-segment name each bundle member is filed under
// where a vendor format only discovers components one directory deep (a
// plugin's skills/<name>/SKILL.md and agents/<name>.md). It's just the
// artifact's Name, unless two members share one (same name, different
// namespaces) -- those become "<namespace>-<name>" so neither is dropped.
func FlatNames(members []*artifact.Artifact) map[*artifact.Artifact]string {
	count := map[string]int{}
	for _, m := range members {
		count[string(m.Kind)+"/"+m.Name]++
	}
	out := make(map[*artifact.Artifact]string, len(members))
	for _, m := range members {
		if count[string(m.Kind)+"/"+m.Name] > 1 && m.Namespace != "" {
			out[m] = m.Namespace + "-" + m.Name
		} else {
			out[m] = m.Name
		}
	}
	return out
}

var exporters = map[string]Exporter{}

// Register makes an Exporter available via GetExporter. Called from an
// exporter package's init(), e.g. internal/targets/claudecode.
func Register(e Exporter) {
	exporters[e.TargetID()] = e
}

// GetExporter returns the registered Exporter for a target ID.
func GetExporter(id string) (Exporter, error) {
	if _, ok := Get(id); !ok {
		return nil, fmt.Errorf("unknown target %q", id)
	}
	e, ok := exporters[id]
	if !ok {
		return nil, fmt.Errorf("export to %q is not implemented yet", id)
	}
	return e, nil
}
