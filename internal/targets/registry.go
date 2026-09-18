// Package targets is AgentWorks' vendor registry: which AI harnesses
// (targets) AgentWorks knows about, which artifact kinds each one can
// consume, and the Exporter that turns an artifact into that vendor's
// native format.
package targets

import (
	"fmt"

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
}

// registry is the static set of vendors AgentWorks knows about. Adding a
// target here makes it show up in `agentworks targets` and become a valid
// --target value immediately; a real Exporter can land separately.
var registry = []Target{
	{
		ID:       "claude-code",
		Name:     "Claude Code",
		Supports: artifact.Kinds(),
		Notes:    "Every kind has a real exporter: skills (Agent Skills format), tools (MCP server), agents and hooks (each a small plugin), and workflows (a bundled plugin: subagents + MCP servers + an orchestrator command).",
	},
	{
		ID:       "chatgpt",
		Name:     "ChatGPT",
		Supports: []artifact.Kind{artifact.KindSkill},
		Notes:    "Skill uploads only, by design -- see AGENTS.md, \"ChatGPT: why skills only.\"",
	},
	{
		ID:       "github-copilot",
		Name:     "GitHub Copilot",
		Supports: []artifact.Kind{artifact.KindAgent, artifact.KindSkill, artifact.KindTool, artifact.KindHook, artifact.KindWorkflow},
		Notes:    "Every supported kind has a real exporter: skills, tools (MCP server via Agent Plugins' mcp.json), agents and hooks (com.github.copilot/ namespace), and workflows (a bundled plugin).",
	},
	{
		ID:       "m365-copilot",
		Name:     "Microsoft 365 Copilot",
		Supports: []artifact.Kind{artifact.KindSkill, artifact.KindAgent},
		Notes:    "Declarative agent / skill packages.",
	},
	{
		ID:       "cursor",
		Name:     "Cursor",
		Supports: []artifact.Kind{artifact.KindSkill, artifact.KindAgent, artifact.KindTool, artifact.KindHook},
		Notes:    "Every supported kind has a real exporter, but none are plugins -- Cursor has no bundle/plugin format, so each is a loose project-scoped file: skills as a project rule (.cursor/rules/<name>.mdc), agents as a real subagent file (.cursor/agents/<name>.md, name/description only -- see AGENTS.md), tools as .cursor/mcp.json, hooks as .cursor/hooks.json. Workflow is unsupported -- no orchestration/bundle format exists.",
	},
	{
		ID:       "gemini-cli",
		Name:     "Gemini CLI",
		Supports: []artifact.Kind{artifact.KindSkill, artifact.KindAgent, artifact.KindTool, artifact.KindHook},
		Notes:    "Skills and tools export as a Gemini CLI extension (gemini-extension.json, + GEMINI.md and supporting files for a skill), agents as a real subagent file (.gemini/agents/<name>.md, with real tools:/model: mapping -- see AGENTS.md), hooks as a .gemini/settings.json fragment meant to be merged by hand (Gemini CLI hooks live only in settings.json, not an extension-scoped format). Workflow is unsupported -- no orchestration/bundle format exists.",
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

// ExportOptions customizes an export run.
type ExportOptions struct {
	// Zip additionally packages the export output as a .zip alongside the
	// exported directory.
	Zip bool
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
