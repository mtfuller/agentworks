// Package githubcopilot implements the "github-copilot" export target: it
// wraps an artifact in a minimal Agent Plugin (https://agent-plugins.org),
// the open plugin format GitHub Copilot adopted in its "Agent Plugins 1.0"
// release.
//
// Skills export as an Agent Skills directory (see internal/targets/
// agentskills) under skills/<name>/. Tools export as an mcp.json server
// registration at the plugin root (see internal/targets/mcpconfig), since
// MCP is how Copilot actually wires up an arbitrary authenticated external
// capability -- the Agent Plugins spec has no other "tool" concept. Agents,
// hooks, and workflows export under the client-specific com.github.copilot/
// namespace (see agent.go, hook.go, and workflow.go respectively).
package githubcopilot

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

const TargetID = "github-copilot"

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
		return "", fmt.Errorf("github-copilot export doesn't support %s", a.Kind)
	}
}

func exportSkill(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}

	if err := writePluginManifest(pluginDir, a); err != nil {
		return "", err
	}
	// Per the Agent Plugins spec, skills live under skills/<name>/.
	if err := agentskills.Write(a, filepath.Join(pluginDir, "skills", a.Name)); err != nil {
		return "", err
	}

	if opts.Zip {
		return agentskills.Zip(pluginDir)
	}
	return pluginDir, nil
}

func exportTool(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	server, err := mcpconfig.ServerFor(a)
	if err != nil {
		return "", err
	}

	pluginDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writePluginManifest(pluginDir, a); err != nil {
		return "", err
	}
	// Unlike skills, a tool has no skills/ wrapping: mcp.json's `command`
	// is resolved relative to the plugin root (cwd defaults to it too), so
	// the tool's source lives directly under pluginDir.
	if err := filecopy.CopyArtifactFiles(a, pluginDir); err != nil {
		return "", err
	}

	file := mcpconfig.File{Schema: mcpSchema, MCPServers: map[string]mcpconfig.Server{a.Name: server}}
	if err := writeMCPFile(filepath.Join(pluginDir, "mcp.json"), file); err != nil {
		return "", err
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}
