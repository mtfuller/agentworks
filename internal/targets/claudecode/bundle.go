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

// ExportBundle packages a set of agent/skill/tool/hook artifacts into one
// Claude Code plugin -- the "ship a cohesive toolkit" counterpart to a
// workflow's ordered orchestration (internal/targets/claudecode/
// workflow.go), which this mirrors the shape of: loop over members,
// accumulate what needs merging (MCP servers, hook events), write the rest
// immediately. Unlike a workflow, there's no generated orchestrator
// command -- a bundle just makes its members available together, it
// doesn't sequence them -- and members can be any of agent/skill/tool/hook,
// not just agent/tool.
func (exporter) ExportBundle(name, description string, artifacts []*artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	for _, m := range artifacts {
		if m.Kind == artifact.KindWorkflow {
			return "", fmt.Errorf("%s: a workflow can't be a bundle member -- export it on its own instead", m.Name)
		}
	}

	pluginDir := filepath.Join(outDir, name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", pluginDir, err)
	}
	if err := writeClaudePluginManifestNamed(pluginDir, name, description); err != nil {
		return "", err
	}

	flat := targets.FlatNames(artifacts)
	var hooks []*artifact.Artifact
	mcpServers := map[string]mcpconfig.Server{}
	for _, m := range artifacts {
		switch m.Kind {
		case artifact.KindSkill:
			if err := agentskills.Write(m, filepath.Join(pluginDir, "skills", flat[m])); err != nil {
				return "", fmt.Errorf("bundling skill %q: %w", m.QualifiedName(), err)
			}
		case artifact.KindAgent:
			path := filepath.Join(pluginDir, "agents", flat[m]+".md")
			if err := writeClaudeAgentFile(path, m); err != nil {
				return "", fmt.Errorf("bundling agent %q: %w", m.QualifiedName(), err)
			}
		case artifact.KindTool:
			// Namespaced under tools/<qualified-name>/, not the plugin
			// root: two tools' own src/ dirs would otherwise collide --
			// including two same-named tools from different namespaces.
			toolDir := filepath.Join("tools", m.QualifiedName())
			if err := filecopy.CopyArtifactFiles(m, filepath.Join(pluginDir, toolDir)); err != nil {
				return "", fmt.Errorf("bundling tool %q: %w", m.QualifiedName(), err)
			}
			server, err := mcpconfig.ServerForDir(m, toolDir)
			if err != nil {
				return "", fmt.Errorf("bundling tool %q: %w", m.QualifiedName(), err)
			}
			mcpServers[m.QualifiedName()] = server
		case artifact.KindHook:
			hooks = append(hooks, m)
		}
	}

	if len(mcpServers) > 0 {
		if err := writeMCPFile(filepath.Join(pluginDir, ".mcp.json"), mcpconfig.File{MCPServers: mcpServers}); err != nil {
			return "", err
		}
	}
	if len(hooks) > 0 {
		doc, err := buildHooksDoc(hooks)
		if err != nil {
			return "", err
		}
		if err := writeHooksDoc(pluginDir, doc); err != nil {
			return "", err
		}
	}

	if opts.Zip {
		return filecopy.ZipDir(pluginDir)
	}
	return pluginDir, nil
}
