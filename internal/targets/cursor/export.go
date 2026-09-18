// Package cursor implements the "cursor" export target. Cursor has no
// plugin/bundle format like Claude Code or GitHub Copilot -- every artifact
// kind it supports maps to a loose, project-scoped config file or directory
// under .cursor/, confirmed against Cursor's own docs (cursor.com/docs,
// researched Sept 2026):
//
//   - skill -> a project rule (.cursor/rules/<name>.mdc), since Cursor has
//     no native Agent Skills/SKILL.md support.
//   - agent -> a real subagent file (.cursor/agents/<name>.md).
//   - tool  -> .cursor/mcp.json, the same {"mcpServers": {...}} shape
//     internal/targets/mcpconfig already builds for claudecode/githubcopilot.
//   - hook  -> .cursor/hooks.json, in Cursor's own event vocabulary.
//
// workflow is deliberately unsupported: Cursor has no orchestration/
// composition primitive to export one to -- see AGENTS.md, "Cursor and
// Gemini CLI: what's real vs. deferred."
package cursor

import (
	"fmt"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
)

func init() {
	targets.Register(exporter{})
}

// TargetID is the Target.ID this package implements.
const TargetID = "cursor"

type exporter struct{}

func (exporter) TargetID() string { return TargetID }

func (exporter) Export(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	switch a.Kind {
	case artifact.KindSkill:
		return exportSkill(a, outDir)
	case artifact.KindAgent:
		return exportAgent(a, outDir)
	case artifact.KindTool:
		return exportTool(a, outDir)
	case artifact.KindHook:
		return exportHook(a, outDir)
	default:
		return "", fmt.Errorf("cursor export does not support %s artifacts yet", a.Kind)
	}
}
