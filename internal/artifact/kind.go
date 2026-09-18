// Package artifact defines AgentWorks' vendor-agnostic artifact model: the
// in-memory representation of an agent, skill, mcp server, or hook, and
// the on-disk <kind>.md (YAML frontmatter + Markdown body) format it's
// loaded from and saved to.
package artifact

import "fmt"

// Kind identifies which of the four artifact types an artifact is.
type Kind string

const (
	KindAgent Kind = "agent"
	KindSkill Kind = "skill"
	KindMCP   Kind = "mcp"
	KindHook  Kind = "hook"
)

// Kinds returns every known artifact kind, in a stable order.
func Kinds() []Kind {
	return []Kind{KindAgent, KindSkill, KindMCP, KindHook}
}

// Valid reports whether k is one of the known kinds.
func (k Kind) Valid() bool {
	for _, known := range Kinds() {
		if k == known {
			return true
		}
	}
	return false
}

// FileName is the name of the manifest file inside an artifact's directory,
// e.g. "skill.md" for a skill.
func (k Kind) FileName() string {
	return string(k) + ".md"
}

// DirName is the project subdirectory that holds artifacts of this kind,
// e.g. "skills" for a skill.
func (k Kind) DirName() string {
	if k == KindMCP {
		return "mcp"
	}
	return string(k) + "s"
}

// ParseKind validates s as a Kind, returning an error listing the valid
// kinds if it isn't one.
func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	if !k.Valid() {
		return "", fmt.Errorf("unknown artifact kind %q (want one of: %s)", s, joinKinds())
	}
	return k, nil
}

func joinKinds() string {
	kinds := Kinds()
	out := string(kinds[0])
	for _, k := range kinds[1:] {
		out += ", " + string(k)
	}
	return out
}
