package importer

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/claudecode"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// hookGroup is the set of handlers that become one hook artifact: everything
// that runs the same bundled script, or a single inline command.
type hookGroup struct {
	key      string // stable identity, recorded as the lockfile subpath
	baseName string
	handlers []claudecode.PluginHook
}

// planHooks turns a plugin's hook handlers into hook artifacts. Handlers that
// run the same bundled script are grouped into one artifact (its handlers list
// carries each event, matcher, and timeout); each inline command becomes its
// own. A handler AgentWorks can't represent faithfully -- a non-command type,
// or a group guarded by an `if` -- is reported in plan.Unsupported, never
// imported in a broader form than the author wrote.
func planHooks(root string, src Source, pluginDir, pluginName, ns string, plan *Plan) error {
	hooks, err := claudecode.ReadPluginHooks(pluginDir)
	if err != nil {
		return err
	}

	var groups []*hookGroup
	byKey := map[string]*hookGroup{}
	for _, h := range hooks {
		if h.Unsupported != "" {
			plan.Unsupported = append(plan.Unsupported, "hooks: "+h.Unsupported)
			continue
		}
		key, base := hookIdentity(h)
		g := byKey[key]
		if g == nil {
			g = &hookGroup{key: key, baseName: base}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.handlers = append(g.handlers, h)
	}

	used := map[string]bool{}
	for _, a := range plan.Artifacts {
		if a.Kind == artifact.KindHook {
			used[a.Name] = true
		}
	}

	for _, g := range groups {
		slug, err := slugify(g.baseName)
		if err != nil {
			slug = "hook"
		}
		name := uniqueSlug(slug, used)
		used[name] = true

		a, stage, warnings, err := buildHookArtifact(root, src, pluginDir, pluginName, ns, name, g, plan)
		if err != nil {
			return err
		}
		plan.Warnings = append(plan.Warnings, warnings...)
		plan.addArtifact(a, stage, "hook:"+g.key)
	}
	return nil
}

// hookIdentity decides which artifact a handler belongs to. A handler whose
// command runs a bundled script is identified by that script; an inline
// command by its event, matcher, and text.
//
// A hook exported by AgentWorks keeps its files under hook-files/<name>/, so
// that directory -- and the original hook name -- identify it.
func hookIdentity(h claudecode.PluginHook) (key, base string) {
	if script := firstRef(h.Command); script != "" {
		if rest, ok := strings.CutPrefix(script, "hook-files/"); ok {
			dir, _, _ := strings.Cut(rest, "/")
			return "hook-files:" + dir, dir
		}
		stem := strings.TrimSuffix(path.Base(script), path.Ext(script))
		return "script:" + script, stem
	}
	matcher := h.Matcher
	if matcher == "" {
		matcher = "all"
	}
	return fmt.Sprintf("inline:%s|%s|%s", h.Event, h.Matcher, h.Command), h.Event + "-" + matcher
}

// uniqueSlug returns base, or base-2, base-3, ... until it isn't in used.
func uniqueSlug(base string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		if candidate := fmt.Sprintf("%s-%d", base, i); !used[candidate] {
			return candidate
		}
	}
}

// hookFilesBase matches the directory AgentWorks' own claude-code export puts a
// hook's bundled files in, and captures it: hook-files/<name>.
var hookFilesBase = regexp.MustCompile(`\$\{CLAUDE_PLUGIN_ROOT\}"?/(hook-files/[^/\s"']+)`)

func buildHookArtifact(root string, src Source, pluginDir, pluginName, ns, name string, g *hookGroup, plan *Plan) (*artifact.Artifact, string, []string, error) {
	rw := newRootRewriter(artifactDirVar)

	// Files exported by AgentWorks live under hook-files/<name>/; treat that
	// directory as the artifact's own root (${ARTIFACT_DIR}) rather than
	// importing the whole hook-files/ tree of every hook in the plugin.
	base := ""
	if m := hookFilesBase.FindStringSubmatch(g.handlers[0].Command); m != nil {
		base = m[1]
	}
	handlers := make([]map[string]any, 0, len(g.handlers))
	events := map[string]bool{}
	var eventList []string
	for _, h := range g.handlers {
		command := h.Command
		if base != "" {
			// Literal replacement: ${ARTIFACT_DIR} would otherwise be read as a
			// (nonexistent) named group reference and expand to nothing.
			command = regexp.MustCompile(`\$\{CLAUDE_PLUGIN_ROOT\}/`+regexp.QuoteMeta(base)).ReplaceAllLiteralString(command, artifactDirVar)
		}
		entry := map[string]any{"event": h.Event, "command": rw.rewrite(command)}
		if h.Matcher != "" {
			entry["matcher"] = h.Matcher
		}
		if h.Timeout > 0 {
			entry["timeout"] = h.Timeout
		}
		handlers = append(handlers, entry)
		if !events[h.Event] {
			events[h.Event] = true
			eventList = append(eventList, h.Event)
		}
	}

	stage, err := plan.newStage()
	if err != nil {
		return nil, "", nil, err
	}
	var warnings []string
	if base != "" {
		if err := filecopy.CopyDirExcept(filepath.Join(pluginDir, filepath.FromSlash(base)), stage); err != nil {
			warnings = append(warnings, fmt.Sprintf("hook %q: copying %s: %v", name, base, err))
		}
	}
	warnings = append(warnings, rw.materialize(pluginDir, stage, fmt.Sprintf("hook %q", name))...)
	if err := writeImportMarker(stage, g.handlers); err != nil {
		return nil, "", nil, err
	}

	desc := fmt.Sprintf("Hook %q imported from %s, running on %s.", name, sourceLabel(src, pluginName), strings.Join(eventList, ", "))
	a := &artifact.Artifact{Frontmatter: artifact.Frontmatter{
		Kind: artifact.KindHook, Name: name, Description: desc,
		Extra: map[string]any{"handlers": handlers},
	}}
	if err := finalizeArtifact(a, "", ns, src); err != nil {
		return nil, "", nil, err
	}
	a.Dir = filepath.Join(root, artifact.KindHook.DirName(), ns, a.Name)
	return a, stage, warnings, nil
}
