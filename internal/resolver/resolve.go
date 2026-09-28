// Package resolver turns format-2 definitions into deterministic, immutable
// agent-team plans without involving a vendor renderer or runtime.
package resolver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mtfuller/agentworks/internal/lockfile"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
)

type Origin string

const (
	OriginLocal     Origin = "local"
	OriginInstalled Origin = "installed"
)

type Options struct {
	AvailableProviders []spec.Provider
	// SelectVariant may apply machine-specific readiness checks. When nil,
	// resolution remains portable and selects the first declared variant whose
	// provider is in AvailableProviders.
	SelectVariant VariantSelector
}

type VariantSelector func(toolName, workdir string, variants []spec.RuntimeVariant, available []spec.Provider) (VariantSelection, error)

type VariantSelection struct {
	Variant        spec.RuntimeVariant
	Runtime        string
	RuntimeVersion string
	ImageDigest    string
	Network        string
}

type TeamPlan struct {
	Format           int             `json:"format"`
	Project          string          `json:"project"`
	Team             string          `json:"team"`
	Description      string          `json:"description,omitempty"`
	DefaultAgent     string          `json:"default_agent,omitempty"`
	Memory           string          `json:"memory,omitempty"`
	DefinitionDigest string          `json:"definition_digest"`
	Agents           []AgentPlan     `json:"agents"`
	Skills           []ComponentPlan `json:"skills"`
	Tools            []ToolPlan      `json:"tools"`
	Digest           string          `json:"digest"`
}

type AgentPlan struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Skills        []string        `json:"skills"`
	Tools         []string        `json:"tools"`
	Delegates     []string        `json:"delegates"`
	MaxPermission spec.Permission `json:"max_permission"`
	Memory        string          `json:"memory,omitempty"`
	Digest        string          `json:"digest"`
}

type ComponentPlan struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Origin      Origin `json:"origin"`
	Digest      string `json:"digest"`
}

type ToolPlan struct {
	ComponentPlan
	Transport        spec.Transport `json:"transport"`
	SelectedProvider spec.Provider  `json:"selected_provider"`
	Command          []string       `json:"command,omitempty"`
	Image            string         `json:"image,omitempty"`
	URL              string         `json:"url,omitempty"`
	Auth             []string       `json:"auth"`
	Runtime          string         `json:"runtime,omitempty"`
	RuntimeVersion   string         `json:"runtime_version,omitempty"`
	ImageDigest      string         `json:"image_digest,omitempty"`
	Network          string         `json:"network,omitempty"`
}

// ResolveTeam resolves one declared team and exactly the skills and tools its
// agents can see. Shared components occur once in the returned plan.
func ResolveTeam(root, teamName string, options Options) (*TeamPlan, error) {
	project, err := spec.LoadProject(root)
	if err != nil {
		return nil, err
	}
	if !contains(project.Teams, teamName) {
		return nil, fmt.Errorf("team %q is not declared by project %q", teamName, project.Name)
	}
	team, err := spec.LoadTeam(root, teamName)
	if err != nil {
		return nil, err
	}

	agents := make(map[string]*spec.Agent, len(team.Agents))
	for _, name := range team.Agents {
		agent, err := spec.LoadAgent(root, name)
		if err != nil {
			return nil, err
		}
		agents[name] = agent
	}
	if err := validateAgentGraph(team.Agents, agents); err != nil {
		return nil, err
	}

	teamDigest, err := lockfile.HashDir(filepath.Join(root, "teams", filepath.FromSlash(teamName)))
	if err != nil {
		return nil, fmt.Errorf("hash team %q: %w", teamName, err)
	}
	plan := &TeamPlan{
		Format:           spec.CurrentFormat,
		Project:          project.Name,
		Team:             teamName,
		Description:      team.Description,
		DefaultAgent:     team.DefaultAgent,
		Memory:           team.Memory.Team,
		DefinitionDigest: teamDigest,
		Agents:           make([]AgentPlan, 0, len(agents)),
		Skills:           []ComponentPlan{},
		Tools:            []ToolPlan{},
	}

	skillRefs := map[string]struct{}{}
	toolRefs := map[string]struct{}{}
	for name, agent := range agents {
		digest, err := lockfile.HashDir(filepath.Dir(agent.Path))
		if err != nil {
			return nil, fmt.Errorf("hash agent %q: %w", name, err)
		}
		agentPlan := AgentPlan{
			Name:          name,
			Description:   agent.Description,
			Skills:        sortedCopy(agent.Skills),
			Tools:         sortedCopy(agent.Tools),
			Delegates:     sortedCopy(agent.Delegates),
			MaxPermission: agent.MaxPermission,
			Memory:        agent.Memory,
			Digest:        digest,
		}
		plan.Agents = append(plan.Agents, agentPlan)
		for _, ref := range agent.Skills {
			skillRefs[ref] = struct{}{}
		}
		for _, ref := range agent.Tools {
			toolRefs[ref] = struct{}{}
		}
	}
	sort.Slice(plan.Agents, func(i, j int) bool { return plan.Agents[i].Name < plan.Agents[j].Name })

	for _, ref := range sortedKeys(skillRefs) {
		component, err := resolveSkill(root, ref, project.Dependencies.Skills)
		if err != nil {
			return nil, fmt.Errorf("agent skill %q: %w", ref, err)
		}
		plan.Skills = append(plan.Skills, component)
	}
	for _, ref := range sortedKeys(toolRefs) {
		tool, err := resolveTool(root, ref, project.Dependencies.Tools, options)
		if err != nil {
			return nil, fmt.Errorf("agent tool %q: %w", ref, err)
		}
		plan.Tools = append(plan.Tools, tool)
	}

	digest, err := planDigest(plan)
	if err != nil {
		return nil, err
	}
	plan.Digest = digest
	return plan, nil
}

func validateAgentGraph(members []string, agents map[string]*spec.Agent) error {
	var errs []error
	for name, agent := range agents {
		for _, delegate := range agent.Delegates {
			if !contains(members, delegate) {
				errs = append(errs, fmt.Errorf("agent %q delegates to %q, which is not a member of the team", name, delegate))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	state := map[string]uint8{}
	stack := []string{}
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			start := 0
			for index, item := range stack {
				if item == name {
					start = index
					break
				}
			}
			cycle := append(append([]string(nil), stack[start:]...), name)
			return fmt.Errorf("agent delegation cycle: %s", strings.Join(cycle, " -> "))
		case 2:
			return nil
		}
		state[name] = 1
		stack = append(stack, name)
		for _, delegate := range agents[name].Delegates {
			if err := visit(delegate); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2
		return nil
	}
	for _, name := range sortedCopy(members) {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func resolveSkill(root, ref string, dependencies map[string]spec.Dependency) (ComponentPlan, error) {
	local := filepath.Join(root, "skills", filepath.FromSlash(ref))
	installed := filepath.Join(root, ".agentworks", "deps", "skills", filepath.FromSlash(ref))
	dir, origin, err := chooseComponent(ref, local, installed, dependencies)
	if err != nil {
		return ComponentPlan{}, err
	}
	skill, err := agentskills.Read(dir)
	if err != nil {
		return ComponentPlan{}, err
	}
	skill.Dir = dir
	if err := skill.Validate(); err != nil {
		return ComponentPlan{}, err
	}
	if skill.Name != pathBase(ref) {
		return ComponentPlan{}, fmt.Errorf("SKILL.md name %q does not match reference %q", skill.Name, ref)
	}
	digest, err := lockfile.HashDir(dir)
	if err != nil {
		return ComponentPlan{}, err
	}
	return ComponentPlan{Name: ref, Description: skill.Description, Origin: origin, Digest: digest}, nil
}

func resolveTool(root, ref string, dependencies map[string]spec.Dependency, options Options) (ToolPlan, error) {
	local := filepath.Join(root, "tools", filepath.FromSlash(ref))
	installed := filepath.Join(root, ".agentworks", "deps", "tools", filepath.FromSlash(ref))
	dir, origin, err := chooseComponent(ref, local, installed, dependencies)
	if err != nil {
		return ToolPlan{}, err
	}
	var tool *spec.Tool
	if origin == OriginLocal {
		tool, err = spec.LoadTool(root, ref)
	} else {
		tool, err = spec.LoadInstalledTool(root, ref)
	}
	if err != nil {
		return ToolPlan{}, err
	}
	selection := VariantSelection{}
	if options.SelectVariant != nil {
		selection, err = options.SelectVariant(ref, dir, tool.Runtime.Variants, options.AvailableProviders)
	} else {
		selection.Variant, err = selectVariant(tool.Runtime.Variants, options.AvailableProviders)
	}
	if err != nil {
		return ToolPlan{}, fmt.Errorf("tool %q: %w", ref, err)
	}
	variant := selection.Variant
	digest, err := lockfile.HashDir(dir)
	if err != nil {
		return ToolPlan{}, err
	}
	return ToolPlan{
		ComponentPlan: ComponentPlan{Name: ref, Description: tool.Description, Origin: origin, Digest: digest},
		Transport:     tool.Transport, SelectedProvider: variant.Provider, Command: variant.Argv(),
		Image: variant.Image, URL: variant.URL, Auth: sortedCopy(tool.Auth),
		Runtime: selection.Runtime, RuntimeVersion: selection.RuntimeVersion,
		ImageDigest: selection.ImageDigest, Network: selection.Network,
	}, nil
}

func chooseComponent(ref, local, installed string, dependencies map[string]spec.Dependency) (string, Origin, error) {
	localExists := isDirectory(local)
	installedExists := isDirectory(installed)
	_, declared := dependencies[ref]
	switch {
	case localExists && (installedExists || declared):
		return "", "", fmt.Errorf("local component conflicts with declared or installed dependency; rename one identity explicitly")
	case localExists:
		return local, OriginLocal, nil
	case installedExists && !declared:
		return "", "", fmt.Errorf("installed component is not declared in agentworks.yaml")
	case installedExists:
		return installed, OriginInstalled, nil
	case declared:
		return "", "", fmt.Errorf("declared dependency is not materialized; install dependencies first")
	default:
		return "", "", fmt.Errorf("no local or installed component exists")
	}
}

func selectVariant(variants []spec.RuntimeVariant, available []spec.Provider) (spec.RuntimeVariant, error) {
	availableSet := map[spec.Provider]struct{}{}
	for _, provider := range available {
		if !provider.Valid() {
			return spec.RuntimeVariant{}, fmt.Errorf("available provider %q is invalid", provider)
		}
		availableSet[provider] = struct{}{}
	}
	for _, variant := range variants {
		if _, ok := availableSet[variant.Provider]; ok {
			return variant, nil
		}
	}
	providers := make([]string, 0, len(variants))
	for _, variant := range variants {
		providers = append(providers, string(variant.Provider))
	}
	return spec.RuntimeVariant{}, fmt.Errorf("no compatible runtime variant (tool offers %s)", strings.Join(providers, ", "))
}

func planDigest(plan *TeamPlan) (string, error) {
	copy := *plan
	copy.Digest = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", fmt.Errorf("encode resolved plan: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sortedCopy(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	sort.Strings(result)
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func pathBase(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}
