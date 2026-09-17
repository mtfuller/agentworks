// Package workflowsteps parses a workflow artifact's `steps:` frontmatter
// and resolves each entry against the real agent/tool artifacts in its
// project, so every export target that bundles a workflow (currently
// claude-code and github-copilot) shares one implementation of "what does
// this workflow actually point at."
package workflowsteps

import (
	"fmt"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
)

// Step is one resolved entry from a workflow's `steps:` list.
type Step struct {
	Kind     artifact.Kind // artifact.KindAgent or artifact.KindTool
	Name     string
	Artifact *artifact.Artifact
}

// Resolve parses a's `steps:` frontmatter field -- a YAML list of
// {agent: <name>} or {tool: <name>} entries, the shape documented in the
// workflow scaffold template -- and loads each referenced artifact from
// a's project (found via project.FindRoot(a.Dir)).
func Resolve(a *artifact.Artifact) ([]Step, error) {
	raw, _ := a.Extra["steps"].([]any)
	if len(raw) == 0 {
		return nil, nil
	}

	root, err := project.FindRoot(a.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolving project root for %s: %w", a.Dir, err)
	}

	steps := make([]Step, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: step %d is not a mapping (want {agent: <name>} or {tool: <name>})", a.Dir, i)
		}

		kind, name, err := stepKindAndName(m)
		if err != nil {
			return nil, fmt.Errorf("%s: step %d: %w", a.Dir, i, err)
		}

		dir := filepath.Join(root, kind.DirName(), name)
		loaded, err := artifact.Load(dir, kind)
		if err != nil {
			return nil, fmt.Errorf("%s: step %d references %s %q, but %w", a.Dir, i, kind, name, err)
		}
		steps = append(steps, Step{Kind: kind, Name: name, Artifact: loaded})
	}
	return steps, nil
}

func stepKindAndName(m map[string]any) (artifact.Kind, string, error) {
	if v, ok := m["agent"].(string); ok && v != "" {
		return artifact.KindAgent, v, nil
	}
	if v, ok := m["tool"].(string); ok && v != "" {
		return artifact.KindTool, v, nil
	}
	return "", "", fmt.Errorf("expected an \"agent\" or \"tool\" key, got %v", m)
}
