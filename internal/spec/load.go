package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ProjectFile = "agentworks.yaml"
	LocalFile   = "agentworks.local.yaml"
	AgentFile   = "AGENT.md"
	TeamFile    = "team.yaml"
	ToolFile    = "tool.yaml"
	SourceFile  = "source.yaml"
	RouteFile   = "route.yaml"
)

// LoadProject loads and validates a format-2 project. Format-1 projects remain
// owned by internal/project until the new architecture is ready to become the
// default.
func LoadProject(root string) (*Project, error) {
	path := filepath.Join(root, ProjectFile)
	var project Project
	if err := decodeFile(path, &project); err != nil {
		return nil, err
	}
	applyProjectDefaults(&project)
	if err := project.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &project, nil
}

// LoadLocal loads machine-specific workspace bindings. A missing local file is
// valid and returns an empty configuration.
func LoadLocal(root string) (*LocalConfig, error) {
	path := filepath.Join(root, LocalFile)
	var local LocalConfig
	if err := decodeFile(path, &local); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			local.Workspaces = map[string]WorkspaceBinding{}
			return &local, nil
		}
		return nil, err
	}
	if local.Workspaces == nil {
		local.Workspaces = map[string]WorkspaceBinding{}
	}
	if err := local.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &local, nil
}

// LoadTeam loads teams/<name>/team.yaml and verifies that its declared name
// matches its directory.
func LoadTeam(root, name string) (*Team, error) {
	if err := validateRef("team reference", name); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "teams", filepath.FromSlash(name), TeamFile)
	var team Team
	if err := decodeFile(path, &team); err != nil {
		return nil, err
	}
	if err := team.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if team.Name != pathBase(name) {
		return nil, fmt.Errorf("%s: team name %q does not match directory %q", path, team.Name, pathBase(name))
	}
	return &team, nil
}

// LoadAgent loads agents/<name>/AGENT.md and strictly decodes its YAML
// frontmatter. The Markdown body is the agent's harness-neutral instructions.
func LoadAgent(root, name string) (*Agent, error) {
	if err := validateRef("agent reference", name); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "agents", filepath.FromSlash(name), AgentFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	frontmatter, body, err := splitMarkdown(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var agent Agent
	if err := decodeStrict(frontmatter, &agent); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if agent.MaxPermission == "" {
		agent.MaxPermission = PermissionReadonly
	}
	agent.Instructions = body
	agent.Path = path
	if err := agent.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if agent.Name != pathBase(name) {
		return nil, fmt.Errorf("%s: agent name %q does not match directory %q", path, agent.Name, pathBase(name))
	}
	return &agent, nil
}

// LoadTool loads tools/<name>/tool.yaml and verifies its identity.
func LoadTool(root, name string) (*Tool, error) {
	if err := validateRef("tool reference", name); err != nil {
		return nil, err
	}
	return loadToolAt(filepath.Join(root, "tools", filepath.FromSlash(name)), name)
}

// LoadInstalledTool loads a materialized tool dependency.
func LoadInstalledTool(root, name string) (*Tool, error) {
	if err := validateRef("tool reference", name); err != nil {
		return nil, err
	}
	return loadToolAt(filepath.Join(root, ".agentworks", "deps", "tools", filepath.FromSlash(name)), name)
}

func loadToolAt(dir, name string) (*Tool, error) {
	path := filepath.Join(dir, ToolFile)
	var tool Tool
	if err := decodeFile(path, &tool); err != nil {
		return nil, err
	}
	tool.Path = dir
	if err := tool.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if tool.Name != pathBase(name) {
		return nil, fmt.Errorf("%s: tool name %q does not match directory %q", path, tool.Name, pathBase(name))
	}
	return &tool, nil
}

func decodeFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := decodeStrict(data, value); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func decodeStrict(data []byte, value any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple YAML documents are not allowed")
		}
		return err
	}
	return nil
}

func splitMarkdown(data []byte) ([]byte, string, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(strings.TrimSuffix(lines[0], "\r")) != "---" {
		return nil, "", errors.New("missing opening YAML frontmatter delimiter")
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(strings.TrimSuffix(lines[index], "\r")) != "---" {
			continue
		}
		frontmatter := strings.Join(lines[1:index], "\n")
		body := strings.TrimSpace(strings.Join(lines[index+1:], "\n"))
		return []byte(frontmatter), body, nil
	}
	return nil, "", errors.New("missing closing YAML frontmatter delimiter")
}

func applyProjectDefaults(project *Project) {
	if project.Runtime.StorageLimit == "" {
		project.Runtime.StorageLimit = DefaultStorageLimit
	}
	if project.Runtime.ShutdownGrace == "" {
		project.Runtime.ShutdownGrace = DefaultShutdownGrace
	}
}

func pathBase(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}
