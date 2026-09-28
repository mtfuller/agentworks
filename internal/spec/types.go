// Package spec defines AgentWorks' format-2, vendor-neutral project model.
//
// It intentionally lives beside the format-1 project and artifact packages so the
// new runtime architecture can be built without changing existing behavior.
package spec

const CurrentFormat = 2

// Permission is the maximum authority a run may receive. Permission values are
// ordered from least to most authority.
type Permission string

const (
	PermissionReadonly    Permission = "readonly"
	PermissionReadwrite   Permission = "readwrite"
	PermissionCollaborate Permission = "collaborate"
	PermissionAutonomous  Permission = "autonomous"
)

// Project is the format-2 agentworks.yaml document.
type Project struct {
	Format       int          `yaml:"format"`
	Name         string       `yaml:"name"`
	Teams        []string     `yaml:"teams,omitempty"`
	Workspaces   []string     `yaml:"workspaces,omitempty"`
	Dependencies Dependencies `yaml:"dependencies,omitempty"`
	Runtime      Runtime      `yaml:"runtime,omitempty"`
}

type Dependencies struct {
	Skills map[string]Dependency `yaml:"skills,omitempty"`
	Tools  map[string]Dependency `yaml:"tools,omitempty"`
}

type Dependency struct {
	Source string `yaml:"source"`
	Path   string `yaml:"path,omitempty"`
	Ref    string `yaml:"ref,omitempty"`
}

type Runtime struct {
	StorageLimit  string `yaml:"storage_limit,omitempty"`
	ShutdownGrace string `yaml:"shutdown_grace,omitempty"`
}

// LocalConfig contains machine-specific bindings and is intended to live in
// the gitignored agentworks.local.yaml file.
type LocalConfig struct {
	Workspaces map[string]WorkspaceBinding `yaml:"workspaces,omitempty"`
}

type WorkspaceBinding struct {
	Path string `yaml:"path"`
}

// Team is loaded from teams/<name>/team.yaml.
type Team struct {
	Name         string     `yaml:"name"`
	Description  string     `yaml:"description,omitempty"`
	Agents       []string   `yaml:"agents"`
	DefaultAgent string     `yaml:"default_agent,omitempty"`
	Memory       TeamMemory `yaml:"memory,omitempty"`
}

type TeamMemory struct {
	Team string `yaml:"team,omitempty"`
}

// Agent is loaded from the YAML frontmatter and Markdown body of an AGENT.md.
type Agent struct {
	Name          string     `yaml:"name"`
	Description   string     `yaml:"description,omitempty"`
	Skills        []string   `yaml:"skills,omitempty"`
	Tools         []string   `yaml:"tools,omitempty"`
	Delegates     []string   `yaml:"delegates,omitempty"`
	MaxPermission Permission `yaml:"max_permission,omitempty"`
	Memory        string     `yaml:"memory,omitempty"`
	Instructions  string     `yaml:"-"`
	Path          string     `yaml:"-"`
}

// Tool is loaded from tools/<name>/tool.yaml. MCP is the first supported
// transport, while runtime variants keep host and container execution choices
// explicit and independently selectable.
type Tool struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Transport   Transport   `yaml:"transport"`
	Runtime     ToolRuntime `yaml:"runtime"`
	Auth        []string    `yaml:"auth,omitempty"`
	Path        string      `yaml:"-"`
}

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

type ToolRuntime struct {
	Variants []RuntimeVariant `yaml:"variants"`
}

type Provider string

const (
	ProviderHost      Provider = "host"
	ProviderContainer Provider = "container"
	ProviderRemote    Provider = "remote"
)

type RuntimeVariant struct {
	Provider Provider `yaml:"provider"`
	Command  Command  `yaml:"command,omitempty"`
	Args     []string `yaml:"args,omitempty"`
	Requires []string `yaml:"requires,omitempty"`
	Image    string   `yaml:"image,omitempty"`
	URL      string   `yaml:"url,omitempty"`
}

// Command normalizes either the scalar or sequence command form into argv.
// Args, when present, are appended by RuntimeVariant.Argv.
type Command []string

func (variant RuntimeVariant) Argv() []string {
	argv := append([]string(nil), variant.Command...)
	return append(argv, variant.Args...)
}

const (
	DefaultStorageLimit  = "1GB"
	DefaultShutdownGrace = "8s"
)
