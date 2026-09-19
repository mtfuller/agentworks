package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/claudecode"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// envRefPattern matches ${NAME} references inside a value.
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// sensitiveName reports whether an env var or header name suggests its value
// is a credential.
func sensitiveName(name string) bool {
	n := strings.ToLower(name)
	for _, hint := range []string{"authorization", "token", "key", "secret", "password", "credential"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}

// planMCPServers turns each MCP server a plugin declares into one mcp
// artifact -- a clean one-to-one mapping. A server that can't be imported
// safely or faithfully is reported in plan.Unsupported instead of being
// guessed at.
func planMCPServers(root string, src Source, pluginDir, pluginName, ns string, plan *Plan) error {
	servers, err := claudecode.ReadPluginMCPServers(pluginDir)
	if err != nil {
		return err
	}
	for _, s := range servers {
		a, stage, warnings, unsupported, err := buildMCPArtifact(root, src, pluginDir, pluginName, ns, s, plan)
		if err != nil {
			return err
		}
		if unsupported != "" {
			plan.Unsupported = append(plan.Unsupported, fmt.Sprintf(".mcp.json server %q: %s", s.Name, unsupported))
			continue
		}
		plan.Warnings = append(plan.Warnings, warnings...)
		plan.addArtifact(a, stage, "mcp:"+s.Name)
	}
	return nil
}

func buildMCPArtifact(root string, src Source, pluginDir, pluginName, ns string, s claudecode.PluginMCPServer, plan *Plan) (a *artifact.Artifact, stage string, warnings []string, unsupported string, err error) {
	slug, slugErr := slugify(s.Name)
	if slugErr != nil {
		return nil, "", nil, "its name can't be converted to a valid artifact name", nil
	}
	if len(s.Ignored) > 0 {
		warnings = append(warnings, fmt.Sprintf("MCP server %q: ignored unsupported field(s): %s", s.Name, strings.Join(s.Ignored, ", ")))
	}

	rw := newRootRewriter(".")
	extra := map[string]any{}
	auth := map[string]bool{}
	env := map[string]string{}

	// AgentWorks' own bundle export runs a server as
	//   sh -c "cd '${CLAUDE_PLUGIN_ROOT}/mcp/<name>' && <command>"
	// with the server's files under mcp/<name>/. Recognize that shape so
	// re-importing your own published marketplace gives back the original
	// command and exactly that server's files, not a nest of shell quoting.
	wrappedBase := ""
	if base, inner, ok := unwrapBundleCommand(s); ok {
		wrappedBase = base
		s.Command, s.Args = inner, nil
	}
	// The unwrapped command is a whole shell line, not a program path.
	isShellLine := wrappedBase != ""

	switch s.Type {
	case "stdio":
		if s.Command == "" {
			return nil, "", nil, "it has no command", nil
		}
		command := rw.rewrite(s.Command)
		var args []string
		for _, arg := range s.Args {
			args = append(args, rw.rewrite(arg))
		}
		if len(args) == 0 && !isShellLine {
			// A bare command is exec'd by the source but run through `sh -c`
			// here, so a path containing spaces must be quoted to survive.
			command = quoteIfNeeded(command)
		}
		extra["command"] = command
		if len(args) > 0 {
			extra["args"] = args
		}
	case "http", "sse":
		if s.URL == "" {
			return nil, "", nil, fmt.Sprintf("its %s transport has no url", s.Type), nil
		}
		extra["transport"] = s.Type
		extra["url"] = s.URL
	default:
		return nil, "", nil, fmt.Sprintf("unsupported transport %q", s.Type), nil
	}

	for name, value := range s.Env {
		value = rw.rewrite(value)
		refs := envRefPattern.FindAllStringSubmatch(value, -1)
		switch {
		case value == "${"+name+"}":
			auth[name] = true // passthrough of a same-named variable: that's what auth is
		case len(refs) > 0:
			env[name] = value
			if sensitiveName(name) {
				for _, r := range refs {
					auth[r[1]] = true
				}
			}
		case sensitiveName(name) && value != "":
			return nil, "", nil, fmt.Sprintf("env %s holds a literal credential -- not imported; add it back as an environment variable reference by hand", name), nil
		default:
			env[name] = value
		}
	}
	if len(env) > 0 {
		extra["env"] = env
	}

	if len(s.Headers) > 0 {
		headers := map[string]string{}
		for name, value := range s.Headers {
			refs := envRefPattern.FindAllStringSubmatch(value, -1)
			if len(refs) == 0 && sensitiveName(name) && value != "" {
				return nil, "", nil, fmt.Sprintf("header %s holds a literal credential -- not imported; add it back as an environment variable reference by hand", name), nil
			}
			for _, r := range refs {
				auth[r[1]] = true
			}
			headers[name] = value
		}
		extra["headers"] = headers
	}

	if len(auth) > 0 {
		names := make([]string, 0, len(auth))
		for n := range auth {
			names = append(names, n)
		}
		sort.Strings(names)
		extra["auth"] = names
	}

	stage, err = plan.newStage()
	if err != nil {
		return nil, "", nil, "", err
	}
	if wrappedBase != "" {
		if err := filecopy.CopyDirExcept(filepath.Join(pluginDir, filepath.FromSlash(wrappedBase)), stage); err != nil {
			warnings = append(warnings, fmt.Sprintf("MCP server %q: copying %s: %v", s.Name, wrappedBase, err))
		}
	}
	warnings = append(warnings, rw.materialize(pluginDir, stage, fmt.Sprintf("MCP server %q", s.Name))...)
	if err := writeImportMarker(stage, s); err != nil {
		return nil, "", nil, "", err
	}

	desc := fmt.Sprintf("MCP server %q imported from %s.", s.Name, sourceLabel(src, pluginName))
	a = &artifact.Artifact{Frontmatter: artifact.Frontmatter{
		Kind: artifact.KindMCP, Name: slug, Description: desc, Extra: extra,
	}}
	if err := finalizeArtifact(a, "", ns, src); err != nil {
		return nil, "", nil, "", err
	}
	a.Dir = filepath.Join(root, artifact.KindMCP.DirName(), ns, a.Name)
	return a, stage, warnings, "", nil
}

// bundleWrapper matches the command AgentWorks' bundle export generates.
var bundleWrapper = regexp.MustCompile(`^cd '?\$\{CLAUDE_PLUGIN_ROOT\}/(mcp/[^'\s]+)'? && (.+)$`)

// unwrapBundleCommand recognizes `sh -c "cd '${CLAUDE_PLUGIN_ROOT}/mcp/<name>'
// && <command>"` and returns the plugin-relative directory holding the
// server's files and the original command.
func unwrapBundleCommand(s claudecode.PluginMCPServer) (base, inner string, ok bool) {
	if s.Type != "stdio" || (s.Command != "sh" && s.Command != "bash") || len(s.Args) != 2 || s.Args[0] != "-c" {
		return "", "", false
	}
	m := bundleWrapper.FindStringSubmatch(s.Args[1])
	if m == nil || strings.Contains(m[1], "..") {
		return "", "", false
	}
	return m[1], m[2], true
}

// sourceLabel names where an artifact came from for its description.
func sourceLabel(src Source, pluginName string) string {
	if pluginName != "" {
		return "the " + pluginName + " plugin"
	}
	return src.String()
}

// quoteIfNeeded single-quotes s for sh when it contains whitespace or shell
// metacharacters, so a path with a space stays one word.
func quoteIfNeeded(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\$`&|;<>()*?[]{}!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeImportMarker records the raw upstream entry in the staging directory.
// It is hashed with the rest of the staging directory so `update` sees an
// upstream edit even when no copied file changed, and never copied into the
// project (see importMarker).
func writeImportMarker(stage string, entry any) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stage, importMarker), append(data, '\n'), 0o644)
}
