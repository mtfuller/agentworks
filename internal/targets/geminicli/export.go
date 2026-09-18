// Package geminicli implements the "gemini-cli" export target, confirmed
// against Gemini CLI's own docs (geminicli.com,
// github.com/google-gemini/gemini-cli, researched Sept 2026):
//
//   - skill -> a Gemini CLI extension directory (gemini-extension.json +
//     GEMINI.md + supporting files) -- the closest real, distributable unit
//     Gemini CLI has to a Claude Code plugin-wrapped skill.
//   - agent -> a real subagent file (.gemini/agents/<name>.md), with a real
//     "tools:" array and "model:" alias -- see internal/targets/agentcaps's
//     ForGeminiCLI.
//   - tool  -> an extension's gemini-extension.json with only its
//     "mcpServers" field populated (no GEMINI.md needed for a bare tool).
//   - hook  -> a .gemini/settings.json fragment, since Gemini CLI hooks
//     live only in settings.json (shared with unrelated user settings), not
//     in any extension-scoped format -- meant to be merged by hand, the
//     same "AgentWorks produces an artifact, never edits your live project"
//     posture a tool's own .mcp.json-shaped output already has elsewhere.
//
// workflow is deliberately unsupported: Gemini CLI has no orchestration/
// composition primitive beyond a single extension's own commands -- see
// AGENTS.md, "Cursor and Gemini CLI: what's real vs. deferred."
package geminicli

import (
	"fmt"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
)

func init() {
	targets.Register(exporter{})
}

// TargetID is the Target.ID this package implements.
const TargetID = "gemini-cli"

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
		return "", fmt.Errorf("gemini-cli export does not support %s artifacts yet", a.Kind)
	}
}
