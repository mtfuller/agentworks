// Package mcpconfig builds MCP ("Model Context Protocol") server
// registrations for an mcp artifact. Claude Code's project .mcp.json,
// GitHub Copilot's Agent Plugins mcp.json, Cursor's .cursor/mcp.json and
// Gemini CLI's extension manifest all use the same
// {"mcpServers": {"name": {...}}} shape, so this is the one place that turns
// an mcp artifact into that entry; each target just decides where to write
// the result and whether it needs a $schema field or a per-vendor tweak.
package mcpconfig

import (
	"fmt"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// Transport values an mcp artifact may declare via its `transport` field.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
	TransportSSE   = "sse"
)

// Server is one MCP server entry: a local stdio process (Command/Args/Env)
// or a remote http/sse endpoint (URL/Headers).
type Server struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// File is an mcp.json/.mcp.json document: a map of server name to Server.
type File struct {
	Schema     string            `json:"$schema,omitempty"`
	MCPServers map[string]Server `json:"mcpServers"`
}

// TransportOf returns the artifact's declared transport, defaulting to
// stdio when the field is unset.
func TransportOf(a *artifact.Artifact) string {
	if t := a.ExtraString("transport"); t != "" {
		return t
	}
	return TransportStdio
}

// IsRemote reports whether the artifact is a remote (http/sse) server with
// no local process to run.
func IsRemote(a *artifact.Artifact) bool {
	t := TransportOf(a)
	return t == TransportHTTP || t == TransportSSE
}

// CommandLine returns the shell command line that starts a stdio mcp
// artifact: its `command` alone (run via `sh -c`, so it may itself be a full
// command line like "python3 src/main.py"), or, when `args:` is set,
// `command` followed by each arg shell-quoted -- so args can contain spaces
// without the author hand-quoting them. Returns "" if no command is set.
func CommandLine(a *artifact.Artifact) string {
	command := a.ExtraString("command")
	if command == "" {
		return ""
	}
	args := a.ExtraStringSlice("args")
	if len(args) == 0 {
		return command
	}
	parts := []string{command}
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// placeholderMarker is what the scaffolds put where an author must supply a
// real value (e.g. the npx-wrapper's package name).
const placeholderMarker = "REPLACE-WITH-"

// Placeholder returns the first command/arg/url still holding a scaffold's
// "REPLACE-WITH-..." marker, or "" if there is none. A scaffold has to
// validate out of the box, so this is surfaced by `doctor` (and skips the
// smoke test) rather than failing `validate`.
func Placeholder(a *artifact.Artifact) string {
	candidates := append([]string{a.ExtraString("command"), a.ExtraString("url")}, a.ExtraStringSlice("args")...)
	for _, c := range candidates {
		if strings.Contains(c, placeholderMarker) {
			return c
		}
	}
	return ""
}

// Validate checks an mcp artifact's connection fields for consistency:
// a known transport, a command for stdio or a url for http/sse, and no
// literal credentials in headers. It is the single source of truth shared
// by `agentworks validate` and the exporters.
func Validate(a *artifact.Artifact) error {
	switch t := TransportOf(a); t {
	case TransportStdio:
		if a.ExtraString("url") != "" {
			return fmt.Errorf("%s: \"url\" is only for http/sse servers -- set \"transport\" or remove it", a.Dir)
		}
		if a.ExtraString("command") == "" && (len(a.ExtraStringSlice("auth")) > 0 || len(a.ExtraStringSlice("args")) > 0) {
			return fmt.Errorf("%s: declares \"auth\"/\"args\" but no \"command\" -- nothing will use them", a.Dir)
		}
	case TransportHTTP, TransportSSE:
		if a.ExtraString("url") == "" {
			return fmt.Errorf("%s: transport %q needs a \"url\"", a.Dir, t)
		}
		if a.ExtraString("command") != "" {
			return fmt.Errorf("%s: transport %q is remote, but \"command\" is set -- use transport: stdio to run a local process", a.Dir, t)
		}
		for name, value := range a.ExtraStringMap("headers") {
			if looksSensitiveHeader(name) && !strings.Contains(value, "${") {
				return fmt.Errorf("%s: header %q has a literal value -- reference an environment variable (e.g. \"Bearer ${TOKEN}\") and list it under \"auth\" so no secret is written into exported config", a.Dir, name)
			}
		}
	default:
		return fmt.Errorf("%s: unknown transport %q (want one of: %s, %s, %s)", a.Dir, t, TransportStdio, TransportHTTP, TransportSSE)
	}
	return nil
}

func looksSensitiveHeader(name string) bool {
	n := strings.ToLower(name)
	for _, hint := range []string{"authorization", "token", "key", "secret", "password"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}

// ServerFor builds the registration entry for an mcp artifact.
//
// A stdio server runs its `command` via `sh -c` (see CommandLine), the same
// execution style `agentworks test` uses for `test:`, so AgentWorks doesn't
// need to know what language the server is written in. When `args:` is
// present, `command` is instead exec'd directly with those args, which is
// the natural shape for wrappers like `npx -y some-server`.
//
// A remote (http/sse) server is just its `url` and `headers`.
//
// Environment: each name in `auth` (required secret variables) is passed
// through as an unresolved ${VAR} reference, never a literal value -- both
// Claude Code and Agent Plugins expand ${VAR} from the real environment at
// startup, so no secret is baked into generated config. `env` (non-secret
// literals) is merged in alongside; `auth` wins on a name collision.
func ServerFor(a *artifact.Artifact) (Server, error) {
	if err := Validate(a); err != nil {
		return Server{}, err
	}

	if IsRemote(a) {
		return Server{
			Type:    TransportOf(a),
			URL:     a.ExtraString("url"),
			Headers: a.ExtraStringMap("headers"),
			Env:     envFor(a),
		}, nil
	}

	command := a.ExtraString("command")
	if command == "" {
		return Server{}, fmt.Errorf("%s has no \"command\" set in its frontmatter -- add one describing how to run it before exporting", a.Dir)
	}

	s := Server{Type: TransportStdio, Env: envFor(a)}
	if args := a.ExtraStringSlice("args"); len(args) > 0 {
		s.Command = command
		s.Args = args
	} else {
		s.Command = "sh"
		s.Args = []string{"-c", command}
	}
	return s, nil
}

func envFor(a *artifact.Artifact) map[string]string {
	env := map[string]string{}
	for k, v := range a.ExtraStringMap("env") {
		env[k] = v
	}
	for _, name := range a.ExtraStringSlice("auth") {
		env[name] = "${" + name + "}"
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// ServerForDir is ServerFor, but for an mcp server bundled alongside others
// under a shared plugin root (see claudecode/githubcopilot's ExportBundle):
// its own files can't all sit at the plugin root the way they do when it's
// exported standalone (two servers' own src/ dirs would collide), so
// they're namespaced under dir instead, and a stdio command is wrapped to
// cd into dir first so its own relative paths still resolve exactly as the
// artifact's author wrote them. Remote servers have no files and are
// returned unchanged.
func ServerForDir(a *artifact.Artifact, dir string) (Server, error) {
	s, err := ServerFor(a)
	if err != nil {
		return Server{}, err
	}
	if IsRemote(a) || dir == "" || dir == "." {
		return s, nil
	}
	s.Command = "sh"
	s.Args = []string{"-c", fmt.Sprintf("cd %s && %s", shellQuote(dir), CommandLine(a))}
	return s, nil
}

// shellQuote wraps s in single quotes for safe use as one sh word,
// escaping any embedded single quotes. Artifact/bundle names are already
// restricted to lowercase letters, digits, and hyphens, so for directory
// names there's nothing to escape; for args this is what keeps spaces and
// metacharacters from being interpreted.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteArg quotes an argument only when it contains characters the shell
// would interpret, so the common `-y some-package@1.2` stays readable.
func quoteArg(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-.,/:@%+=", r))
	}) {
		return s
	}
	return shellQuote(s)
}
