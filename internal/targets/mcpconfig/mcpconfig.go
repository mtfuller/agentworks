// Package mcpconfig builds MCP ("Model Context Protocol") server
// registrations for a tool artifact. Both of AgentWorks' current
// tool-exportable targets -- Claude Code's project .mcp.json and GitHub
// Copilot's Agent Plugins mcp.json -- use the same
// {"mcpServers": {"name": {...}}} shape, so this is the one place that
// turns a tool artifact into that entry; each target just decides where to
// write the result and whether it needs a $schema field.
package mcpconfig

import (
	"fmt"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// Server is a single stdio MCP server entry.
type Server struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// File is an mcp.json/.mcp.json document: a map of server name to Server.
type File struct {
	Schema     string            `json:"$schema,omitempty"`
	MCPServers map[string]Server `json:"mcpServers"`
}

// ServerFor builds a stdio Server entry that runs a tool artifact's
// declared `command` frontmatter field via `sh -c` -- the same execution
// style `agentworks test` uses for `test:` -- so AgentWorks doesn't need to
// know or care what language/runtime the tool is written in.
//
// Each name in the tool's `auth` frontmatter field (a list of required
// environment variable names) is passed through as an unresolved ${VAR}
// reference in Env, never as a literal value: both Claude Code and Agent
// Plugins expand ${VAR} from the actual environment at startup, so no
// secret is ever baked into generated config.
func ServerFor(a *artifact.Artifact) (Server, error) {
	command := a.ExtraString("command")
	if command == "" {
		return Server{}, fmt.Errorf("%s has no \"command\" set in its frontmatter -- add one describing how to run it before exporting", a.Dir)
	}

	var env map[string]string
	if auth := a.ExtraStringSlice("auth"); len(auth) > 0 {
		env = make(map[string]string, len(auth))
		for _, name := range auth {
			env[name] = "${" + name + "}"
		}
	}

	return Server{
		Type:    "stdio",
		Command: "sh",
		Args:    []string{"-c", command},
		Env:     env,
	}, nil
}

// ServerForDir is ServerFor, but for a tool bundled alongside others under a
// shared plugin root (see claudecode/githubcopilot's ExportBundle): the
// tool's own files can't all sit at the plugin root the way they do when
// it's exported standalone (two tools' own src/ dirs would collide), so
// they're namespaced under dir instead, and the command is wrapped to cd
// into dir first so the tool's own relative paths still resolve exactly as
// the artifact's author wrote them.
func ServerForDir(a *artifact.Artifact, dir string) (Server, error) {
	s, err := ServerFor(a)
	if err != nil {
		return Server{}, err
	}
	if dir != "" && dir != "." {
		command := a.ExtraString("command")
		s.Args = []string{"-c", fmt.Sprintf("cd %s && %s", shellQuote(dir), command)}
	}
	return s, nil
}

// shellQuote wraps s in single quotes for safe use as one sh word,
// escaping any embedded single quotes. Artifact/bundle names are already
// restricted to lowercase letters, digits, and hyphens, so in practice
// there's nothing to escape -- this is defensive, not load-bearing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
