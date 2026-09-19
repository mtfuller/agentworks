package importer

import (
	"fmt"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
	"github.com/mtfuller/agentworks/internal/targets/claudecode"
)

// planPlugin builds a multi-artifact Plan for a Claude Code plugin,
// decomposing skills/*/SKILL.md and agents/*.md into individual artifacts,
// each MCP server into an mcp artifact, and hook handlers into hook
// artifacts (grouped by the script they run). See mcp.go and hooks.go for
// what is and isn't representable; the rest is reported in Unsupported.
func planPlugin(root string, src Source, contentDir string, opts Options) (*Plan, error) {
	if opts.Name != "" {
		return nil, fmt.Errorf("--name can only be used when importing a single skill, not a multi-artifact plugin")
	}

	pluginName, _, err := claudecode.ReadPluginManifest(contentDir)
	if err != nil {
		return nil, err
	}

	ns, err := resolveNamespace(src, opts.Namespace)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		Source:      src,
		PluginName:  pluginName,
		srcDirs:     map[string]string{},
		HashSources: map[string]string{},
		Subpaths:    map[string]string{},
	}

	skillDirs, err := filepath.Glob(filepath.Join(contentDir, "skills", "*"))
	if err != nil {
		return nil, err
	}
	for _, dir := range skillDirs {
		if !agentskills.IsSkillDir(dir) {
			continue
		}
		a, err := agentskills.Read(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		if err := finalizeArtifact(a, "", ns, src); err != nil {
			return nil, err
		}
		a.Dir = filepath.Join(root, artifact.KindSkill.DirName(), ns, a.Name)
		plan.Artifacts = append(plan.Artifacts, a)
		plan.srcDirs[a.Dir] = dir
		plan.HashSources[a.Dir] = dir
		plan.Subpaths[a.Dir] = subpathOf(contentDir, dir)
	}

	agentFiles, err := filepath.Glob(filepath.Join(contentDir, "agents", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range agentFiles {
		a, err := claudecode.ReadAgentFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := finalizeArtifact(a, "", ns, src); err != nil {
			return nil, err
		}
		a.Dir = filepath.Join(root, artifact.KindAgent.DirName(), ns, a.Name)
		plan.Artifacts = append(plan.Artifacts, a)
		plan.HashSources[a.Dir] = path
		plan.Subpaths[a.Dir] = subpathOf(contentDir, path)
	}

	if err := planMCPServers(root, src, contentDir, pluginName, ns, plan); err != nil {
		return nil, err
	}
	if err := planHooks(root, src, contentDir, pluginName, ns, plan); err != nil {
		return nil, err
	}

	if len(plan.Artifacts) == 0 && len(plan.Unsupported) == 0 {
		return nil, fmt.Errorf("%s: plugin has nothing importable (no skills, agents, MCP servers, or hooks)", src)
	}
	return plan, nil
}

// subpathOf returns loc's path relative to contentDir, slash-normalized,
// falling back to "" (rather than propagating a filepath.Rel error that
// can't actually happen here -- loc always comes from a Glob rooted at
// contentDir) so a Plan's Subpaths always has a usable value.
func subpathOf(contentDir, loc string) string {
	rel, err := filepath.Rel(contentDir, loc)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
