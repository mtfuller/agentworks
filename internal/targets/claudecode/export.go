// Package claudecode implements the "claude-code" export target.
//
// Skills export to a real Claude Code skill directory (Claude Code
// consumes the same Agent Skills format AgentWorks' own <kind>.md is
// modeled on -- see internal/targets/agentskills). Tools export to a
// project .mcp.json server registration (see internal/targets/mcpconfig),
// since MCP is how Claude Code actually wires up an arbitrary authenticated
// external capability -- there's no "tool" concept of its own to target.
// Agents, hooks, and workflows each export as a real Claude Code plugin
// (agents/*.md, hooks/hooks.json, and a workflow's fuller bundle --
// see agent.go, hook.go, and workflow.go respectively). All are optionally
// zipped for upload/sharing.
package claudecode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

func init() {
	targets.Register(exporter{})
}

const TargetID = "claude-code"

type exporter struct{}

func (exporter) TargetID() string { return TargetID }

func (exporter) Export(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	switch a.Kind {
	case artifact.KindSkill:
		return exportSkill(a, outDir, opts)
	case artifact.KindTool:
		return exportTool(a, outDir, opts)
	case artifact.KindAgent:
		return exportAgent(a, outDir, opts)
	case artifact.KindHook:
		return exportHook(a, outDir, opts)
	default:
		return "", fmt.Errorf("claude-code export doesn't support %s", a.Kind)
	}
}

func exportSkill(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	destDir := filepath.Join(outDir, a.Name)
	if err := agentskills.Write(a, destDir); err != nil {
		return "", err
	}
	if opts.Zip {
		return agentskills.Zip(destDir)
	}
	return destDir, nil
}

func exportTool(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	server, err := mcpconfig.ServerFor(a)
	if err != nil {
		return "", err
	}

	destDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(destDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", destDir, err)
	}
	if err := filecopy.CopyArtifactFiles(a, destDir); err != nil {
		return "", err
	}

	// No $schema: this mirrors Claude Code's own .mcp.json examples, which
	// don't include one.
	file := mcpconfig.File{MCPServers: map[string]mcpconfig.Server{a.Name: server}}
	if err := writeMCPFile(filepath.Join(destDir, ".mcp.json"), file); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(destDir)
	}
	return destDir, nil
}
