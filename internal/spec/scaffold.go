package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Init creates a minimal format-2 runtime project that can be opened directly
// in Studio. It never overwrites an existing manifest.
func Init(root, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("project name is required")
	}
	project := Project{
		Format: CurrentFormat, Name: name, Teams: []string{"default"}, Workspaces: []string{"workspace"},
		Dependencies: Dependencies{Skills: map[string]Dependency{}, Tools: map[string]Dependency{}},
		Runtime:      Runtime{StorageLimit: DefaultStorageLimit, ShutdownGrace: DefaultShutdownGrace},
	}
	if err := project.Validate(); err != nil {
		return err
	}
	projectData, err := yaml.Marshal(project)
	if err != nil {
		return fmt.Errorf("encode runtime project: %w", err)
	}
	manifest := filepath.Join(root, ProjectFile)
	if _, err := os.Stat(manifest); err == nil {
		return fmt.Errorf("%s already exists", manifest)
	} else if !os.IsNotExist(err) {
		return err
	}
	files := map[string]string{
		ProjectFile:                         string(projectData),
		"teams/default/team.yaml":           "name: default\ndescription: A general local agent team.\nagents: [assistant]\ndefault_agent: assistant\nmemory:\n  team: memory/team.md\n",
		"agents/assistant/AGENT.md":         "---\nname: assistant\ndescription: Handles local requests carefully and reports verifiable results.\nskills: [working-carefully]\nmax_permission: readwrite\nmemory: memory/agents/assistant.md\n---\n\n# Assistant\n\nUnderstand the request before acting, use the smallest necessary authority, verify important\nresults, and finish with a concise account of work completed and anything still unresolved.\n",
		"skills/working-carefully/SKILL.md": "---\nname: working-carefully\ndescription: Plan bounded work, preserve user files, and verify important outcomes before completion.\n---\n\n# Working carefully\n\nInspect relevant context before changing anything. Keep actions within the selected workspace\nand permission level. Verify proportionately to risk and report failures honestly.\n",
		"memory/team.md":                    "# Shared team memory\n\nReview proposed durable facts in Studio before adding them here.\n",
		"memory/agents/assistant.md":        "# Assistant memory\n\nPrivate durable guidance for the assistant.\n",
		"sources/manual/source.yaml":        "name: manual\ndescription: Events submitted from local AgentWorks Studio.\nkind: manual\n",
		"routes/manual/route.yaml":          "name: manual\ndescription: Route explicit local requests to the default assistant.\npriority: 100\nwhen:\n  source: manual\n  type: work.requested\ninvoke:\n  team: default\n  agent: assistant\n  workspace: workspace\n  harness: claude-code\n  permission: readonly\ntests:\n  - name: local request\n    event:\n      source: manual\n      type: work.requested\n      subject: example\n      data: {}\n    expect: manual\n",
		"agentworks.local.yaml.example":     "workspaces:\n  workspace:\n    path: /absolute/path/to/a/folder\n",
		".gitignore":                        "agentworks.local.yaml\n.agentworks/deps/\ndist/\n",
		"AGENTS.md":                         "# AGENTS.md\n\n" + name + " is an AgentWorks format-2 project. Agent teams are the composition boundary and Studio is the local runtime.\n\n- Project and team definitions are YAML under agentworks.yaml and teams/.\n- Agent instructions are Markdown under agents/<name>/AGENT.md.\n- Shared Agent Skills live under skills/<name>/SKILL.md and tools under tools/.\n- Sources and routes determine which event invokes exactly one agent.\n- Shared and private memory are reviewable Markdown files under memory/.\n- Machine-specific workspace paths belong only in the gitignored agentworks.local.yaml.\n\nRun agentworks plan default to inspect the resolved closure, then agentworks studio --experimental to run it locally.\n",
	}
	for relative, content := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create runtime project: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	if _, err := LoadProject(root); err != nil {
		return fmt.Errorf("validate initialized project: %w", err)
	}
	if _, err := LoadTeam(root, "default"); err != nil {
		return fmt.Errorf("validate initialized team: %w", err)
	}
	if _, err := LoadAgent(root, "assistant"); err != nil {
		return fmt.Errorf("validate initialized agent: %w", err)
	}
	return nil
}
