package importer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
	"github.com/mtfuller/agentworks/internal/targets/claudecode"
)

// planPlugin builds a multi-artifact Plan for a Claude Code plugin,
// decomposing skills/*/SKILL.md and agents/*.md into individual artifacts.
// Tools (.mcp.json) and hooks (hooks/hooks.json) are reported in
// Unsupported rather than imported -- collapsing a merged hooks.json or
// mcp.json back into "the original N artifacts" is inherently
// lossy/ambiguous (the same many-to-one problem as the export side, in
// reverse), unlike skills and agents which are a clean file per artifact.
func planPlugin(root string, src Source, contentDir string, opts Options) (*Plan, error) {
	if opts.Name != "" {
		return nil, fmt.Errorf("--name can only be used when importing a single skill, not a multi-artifact plugin")
	}

	pluginName, _, err := claudecode.ReadPluginManifest(contentDir)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Source: src, PluginName: pluginName, srcDirs: map[string]string{}}

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
		if err := finalizeArtifact(root, a, "", src); err != nil {
			return nil, err
		}
		a.Dir = filepath.Join(root, artifact.KindSkill.DirName(), a.Name)
		plan.Artifacts = append(plan.Artifacts, a)
		plan.srcDirs[a.Dir] = dir
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
		if err := finalizeArtifact(root, a, "", src); err != nil {
			return nil, err
		}
		a.Dir = filepath.Join(root, artifact.KindAgent.DirName(), a.Name)
		plan.Artifacts = append(plan.Artifacts, a)
	}

	if _, err := os.Stat(filepath.Join(contentDir, "hooks", "hooks.json")); err == nil {
		plan.Unsupported = append(plan.Unsupported, "hooks/hooks.json (hook import not supported yet)")
	}
	if _, err := os.Stat(filepath.Join(contentDir, ".mcp.json")); err == nil {
		plan.Unsupported = append(plan.Unsupported, ".mcp.json (tool import not supported yet)")
	}

	if len(plan.Artifacts) == 0 && len(plan.Unsupported) == 0 {
		return nil, fmt.Errorf("%s: plugin has no importable skills or agents", src)
	}
	return plan, nil
}
