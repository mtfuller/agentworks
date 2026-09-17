// Package scaffold generates the starter directory and files for a new
// artifact, so `agentworks new` and the TUI's new-artifact wizard produce
// the exact same result from one code path.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// Options customizes a scaffolded artifact.
type Options struct {
	Description string
	Version     string   // defaults to "0.1.0" if empty
	Targets     []string // defaults to none
	// Template, if set, scaffolds from a built-in Template instead of the
	// kind's generic default (see templates.go / GetTemplate).
	Template string
}

// extraFile is a supporting file created alongside <kind>.md.
type extraFile struct {
	relPath string
	content string
}

// kindSpec describes how to scaffold one artifact kind: its default extra
// frontmatter fields, body template, and supporting files/directories.
type kindSpec struct {
	extra     func(name string) map[string]any
	bodyTmpl  string
	extraDirs []string // directories to create even if they start empty
	files     func(name string) []extraFile
}

var specs = map[artifact.Kind]kindSpec{
	artifact.KindSkill: {
		extra: func(name string) map[string]any {
			return map[string]any{"entrypoint": "scripts/main.py"}
		},
		bodyTmpl: `# {{.Title}}

{{.Description}}

## Usage

Describe what invokes this skill and what it produces.

## Implementation

See ` + "`scripts/main.py`" + `. Add real tests under ` + "`tests/`" + ` and a ` + "`test:`" + `
command to this file's frontmatter once they exist, so ` + "`agentworks test`" + ` can run them.
`,
		files: func(name string) []extraFile {
			return []extraFile{
				{"scripts/main.py", "#!/usr/bin/env python3\n\"\"\"" + name + " entrypoint. Replace with real logic.\"\"\"\n\n\ndef main() -> None:\n    raise NotImplementedError(\"" + name + " is not implemented yet\")\n\n\nif __name__ == \"__main__\":\n    main()\n"},
				{"tests/test_main.py", "\"\"\"Tests for " + name + ". Wire this up with pytest (or your tool of choice),\nthen add a `test:` command to skill.md's frontmatter.\n\"\"\"\n\n\ndef test_placeholder():\n    assert True\n"},
			}
		},
		extraDirs: []string{"samples"},
	},
	artifact.KindTool: {
		extra: func(name string) map[string]any {
			return map[string]any{
				"entrypoint": "src/main",
				"command":    "",
				"auth":       []string{},
			}
		},
		bodyTmpl: `# {{.Title}}

{{.Description}}

## Interface

Describe the tool's inputs/outputs (arguments, request/response shape, etc).

## Implementation

Add source under ` + "`src/`" + ` and tests under ` + "`tests/`" + `. Set a ` + "`test:`" + `
command in this file's frontmatter once tests exist, so ` + "`agentworks test`" + ` can run them.

## Running as an MCP server

Set ` + "`command`" + ` in this file's frontmatter to the shell command that starts this
tool, and list any required environment variables under ` + "`auth`" + `. Claude Code and
GitHub Copilot both expose tools via MCP; ` + "`agentworks export ... --target claude-code`" + `
or ` + "`--target github-copilot`" + ` uses these fields to generate the server registration.
`,
		extraDirs: []string{"src", "tests"},
	},
	artifact.KindAgent: {
		extra: func(name string) map[string]any {
			return map[string]any{}
		},
		bodyTmpl: `# {{.Title}}

{{.Description}}

## Guidance

Write the agent's instructions/system prompt here. Put longer reference
material the agent should be able to consult under ` + "`resources/`" + `.
`,
		extraDirs: []string{"resources"},
	},
	artifact.KindHook: {
		extra: func(name string) map[string]any {
			return map[string]any{"events": []string{}, "command": ""}
		},
		bodyTmpl: `# {{.Title}}

{{.Description}}

Set ` + "`events`" + ` and ` + "`command`" + ` in this file's frontmatter to describe
what triggers this hook and what it runs.
`,
	},
	artifact.KindWorkflow: {
		extra: func(name string) map[string]any {
			return map[string]any{"steps": []map[string]string{}}
		},
		bodyTmpl: `# {{.Title}}

{{.Description}}

List the agents/tools this workflow composes, in order, under ` + "`steps`" + ` in
this file's frontmatter, e.g.:

` + "```yaml" + `
steps:
  - agent: researcher
  - tool: jira-fetch
` + "```" + `
`,
	},
}

// New scaffolds a new artifact of the given kind and name under root
// (a project's root directory), returning the artifact it created.
func New(root string, kind artifact.Kind, name string, opts Options) (*artifact.Artifact, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("unknown artifact kind %q", kind)
	}

	dir := filepath.Join(root, kind.DirName(), name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists", dir)
	}

	spec, ok := specs[kind]
	if !ok {
		return nil, fmt.Errorf("no scaffold defined for kind %q", kind)
	}
	if opts.Template != "" {
		t, ok := GetTemplate(kind, opts.Template)
		if !ok {
			return nil, fmt.Errorf("no %q template for kind %s (see 'agentworks templates')", opts.Template, kind)
		}
		spec = t.spec
	}

	version := opts.Version
	if version == "" {
		version = "0.1.0"
	}

	body, err := renderBody(spec.bodyTmpl, name, opts.Description)
	if err != nil {
		return nil, err
	}

	a := &artifact.Artifact{
		Frontmatter: artifact.Frontmatter{
			Kind:        kind,
			Name:        name,
			Description: opts.Description,
			Version:     version,
			Targets:     opts.Targets,
			Extra:       spec.extra(name),
		},
		Body: body,
		Dir:  dir,
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	for _, d := range spec.extraDirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", d, err)
		}
	}
	if spec.files != nil {
		for _, f := range spec.files(name) {
			path := filepath.Join(dir, f.relPath)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
			}
			if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
				return nil, fmt.Errorf("writing %s: %w", path, err)
			}
		}
	}

	if err := a.Save(); err != nil {
		return nil, err
	}
	return a, nil
}

func renderBody(tmpl, name, description string) (string, error) {
	t, err := template.New("body").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing body template: %w", err)
	}
	if description == "" {
		description = "TODO: describe what this does."
	}
	data := struct{ Title, Description string }{
		Title:       title(name),
		Description: description,
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("rendering body template: %w", err)
	}
	return sb.String(), nil
}

// title turns a kebab-case artifact name into a human-readable heading,
// e.g. "csv-analyzer" -> "Csv Analyzer".
func title(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
