package importer

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
)

// pluginRootVar is what Claude Code plugins use to refer to their own install
// directory, in MCP server command/args/env and in hook commands.
const pluginRootVar = "${CLAUDE_PLUGIN_ROOT}"

// artifactDirVar is AgentWorks' equivalent inside a hook command: the
// directory holding the hook artifact's own files. Exporters that can ship
// those files substitute it for the target's real path.
const artifactDirVar = artifact.ArtifactDirVar

// importMarker is the file inside a planned artifact's staging directory that
// records the raw upstream entry it was built from. It is hashed (so `update`
// notices upstream changes) but never copied into the project.
const importMarker = ".agentworks-import.json"

// pluginRootPath matches ${CLAUDE_PLUGIN_ROOT}/some/path (with the variable
// optionally quoted, as in "${CLAUDE_PLUGIN_ROOT}"/scripts/x.sh) and captures
// the path after the slash.
var pluginRootPath = regexp.MustCompile(`\$\{CLAUDE_PLUGIN_ROOT\}"?/([^\s"'` + "`" + `;&|()<>]+)`)

// rootRewriter rewrites references to the plugin root inside commands, args,
// and env values, and remembers which top-level plugin paths they touched so
// those files can be copied alongside the imported artifact.
type rootRewriter struct {
	// replacement is what the variable becomes: "." for an MCP server (its
	// command runs from the artifact's directory) and ${ARTIFACT_DIR} for a
	// hook (whose command runs elsewhere).
	replacement string
	refs        map[string]bool
	bare        bool
	escapes     []string
}

func newRootRewriter(replacement string) *rootRewriter {
	return &rootRewriter{replacement: replacement, refs: map[string]bool{}}
}

// rewrite returns s with the plugin-root variable replaced, recording what it
// referenced. Unrelated strings pass through untouched.
func (r *rootRewriter) rewrite(s string) string {
	if !strings.Contains(s, pluginRootVar) {
		return s
	}
	for _, m := range pluginRootPath.FindAllStringSubmatch(s, -1) {
		rel := path.Clean(m[1])
		top, _, _ := strings.Cut(rel, "/")
		if top == ".." || top == "." || path.IsAbs(rel) {
			r.escapes = append(r.escapes, m[1])
			continue
		}
		r.refs[top] = true
	}
	if strings.Contains(pluginRootPath.ReplaceAllString(s, ""), pluginRootVar) {
		r.bare = true
	}
	return strings.ReplaceAll(s, pluginRootVar, r.replacement)
}

// firstRef returns the first referenced top-level path in command (in
// textual order), or "" if it doesn't reference plugin files.
func firstRef(command string) string {
	m := pluginRootPath.FindStringSubmatch(command)
	if m == nil {
		return ""
	}
	rel := path.Clean(m[1])
	top, _, _ := strings.Cut(rel, "/")
	if top == ".." || top == "." {
		return ""
	}
	return rel
}

// materialize copies every referenced top-level path from the fetched plugin
// into dest (preserving its relative layout) and returns warnings for anything
// that couldn't be brought along.
func (r *rootRewriter) materialize(pluginDir, dest, what string) []string {
	var warnings []string
	for _, esc := range r.escapes {
		warnings = append(warnings, fmt.Sprintf("%s: reference to %s/%s escapes the plugin and was left as-is", what, pluginRootVar, esc))
	}
	if r.bare {
		warnings = append(warnings, fmt.Sprintf("%s: refers to the plugin root itself (%s), which is not copied -- review it", what, pluginRootVar))
	}

	tops := make([]string, 0, len(r.refs))
	for top := range r.refs {
		tops = append(tops, top)
	}
	sort.Strings(tops)

	for _, top := range tops {
		src := filepath.Join(pluginDir, filepath.FromSlash(top))
		info, err := os.Lstat(src)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: references %s/%s, which isn't in the plugin", what, pluginRootVar, top))
			continue
		}
		dst := filepath.Join(dest, filepath.FromSlash(top))
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			warnings = append(warnings, fmt.Sprintf("%s: %s is a symlink and was not copied", what, top))
		case info.IsDir():
			if err := filecopy.CopyDirExcept(src, dst); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: copying %s: %v", what, top, err))
			}
		default:
			if err := filecopy.CopyFile(src, dst); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: copying %s: %v", what, top, err))
			}
		}
	}
	return warnings
}
