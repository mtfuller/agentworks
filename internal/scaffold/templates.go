package scaffold

import "github.com/mtfuller/agentworks/internal/artifact"

// Template is a named, curated starting point for a kind -- an alternative
// to that kind's generic default kindSpec (see specs), selected by ID
// instead of applied automatically. Built-in only for now: there's no
// project-defined custom template yet (see AGENTS.md).
type Template struct {
	ID          string
	Kind        artifact.Kind
	Title       string
	Description string
	spec        kindSpec
}

// Templates returns every built-in template, in a stable order (grouped by
// kind, in artifact.Kinds() order).
func Templates() []Template {
	out := make([]Template, len(templates))
	copy(out, templates)
	return out
}

// TemplatesForKind returns the templates available for one kind, in
// registration order.
func TemplatesForKind(k artifact.Kind) []Template {
	var out []Template
	for _, t := range templates {
		if t.Kind == k {
			out = append(out, t)
		}
	}
	return out
}

// GetTemplate looks up a template by kind and ID.
func GetTemplate(kind artifact.Kind, id string) (Template, bool) {
	for _, t := range templates {
		if t.Kind == kind && t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

var templates = []Template{
	{
		ID: "code-reviewer", Kind: artifact.KindAgent,
		Title:       "Code reviewer",
		Description: "Reviews a code change for correctness, security, and style.",
		spec: kindSpec{
			extra: func(name string) map[string]any { return map[string]any{} },
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Review checklist

- **Correctness**: does the change do what it claims to, including edge cases?
- **Security**: any injection, auth, or secret-handling concerns?
- **Style**: does it match the surrounding codebase's existing conventions?
- **Tests**: is the change covered, and do existing tests still make sense?

## Output format

For each finding: file, line (if applicable), what's wrong, and why it matters.
Group by severity (blocking vs. nice-to-have) so the author can triage quickly.
`,
			extraDirs: []string{"resources"},
		},
	},
	{
		ID: "researcher", Kind: artifact.KindAgent,
		Title:       "Researcher",
		Description: "Investigates a question and reports findings with sources.",
		spec: kindSpec{
			extra: func(name string) map[string]any { return map[string]any{} },
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Guidance

- State the question you're answering before diving in.
- Prefer primary sources; note when you're relying on a summary of one.
- Distinguish what you found from what you're inferring.

## Output format

A short summary up front, then supporting detail with a source per claim that
needs one. Flag anything you couldn't confirm rather than guessing.
`,
			extraDirs: []string{"resources"},
		},
	},
	{
		ID: "checklist", Kind: artifact.KindSkill,
		Title:       "Checklist",
		Description: "Walks through a fixed step-by-step procedure.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"entrypoint": "scripts/main.py"}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

## When to use this

Describe the trigger -- what request or situation should invoke this skill.

## Steps

1. TODO: first step
2. TODO: second step
3. TODO: third step

Keep steps in the order they must happen; note anywhere a step can be skipped
and under what condition.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"scripts/main.py", "#!/usr/bin/env python3\n\"\"\"" + name + " entrypoint. Replace with real logic.\"\"\"\n\n\ndef main() -> None:\n    raise NotImplementedError(\"" + name + " is not implemented yet\")\n\n\nif __name__ == \"__main__\":\n    main()\n"},
					{"tests/test_main.py", "\"\"\"Tests for " + name + ". Wire this up with pytest (or your tool of choice),\nthen add a `test:` command to skill.md's frontmatter.\n\"\"\"\n\n\ndef test_placeholder():\n    assert True\n"},
				}
			},
			extraDirs: []string{"samples"},
		},
	},
	{
		ID: "document-analyzer", Kind: artifact.KindSkill,
		Title:       "Document analyzer",
		Description: "Extracts or summarizes information from a document.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"entrypoint": "scripts/main.py"}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Input

Describe the document type(s) this handles (format, typical size, where it
usually comes from).

## Extraction rules

What fields/sections to pull out, and how to handle ones that are missing or
ambiguous in the source document.

## Output format

Describe the exact shape of what gets returned (a table, a JSON block, prose
with headings) so results are consistent across runs.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"scripts/main.py", "#!/usr/bin/env python3\n\"\"\"" + name + ": reads a document path from argv and analyzes it.\"\"\"\n\nimport sys\n\n\ndef main() -> None:\n    if len(sys.argv) < 2:\n        raise SystemExit(\"usage: main.py <path-to-document>\")\n    raise NotImplementedError(\"" + name + " is not implemented yet\")\n\n\nif __name__ == \"__main__\":\n    main()\n"},
				}
			},
			extraDirs: []string{"samples"},
		},
	},
	{
		ID: "api-wrapper", Kind: artifact.KindTool,
		Title:       "API wrapper",
		Description: "Wraps a REST API's endpoints as an MCP tool.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "src/main.py",
					"command":    "python3 src/main.py",
					"auth":       []string{"API_BASE_URL", "API_TOKEN"},
				}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Interface

List the endpoints this exposes and their inputs/outputs.

## Implementation

` + "`src/main.py`" + ` reads ` + "`API_BASE_URL`" + `/` + "`API_TOKEN`" + ` from the environment (see
this file's ` + "`auth`" + ` list) and shows the request-calling shape -- wire it into
whatever MCP server framework you're using for the actual stdio loop.

## Running as an MCP server

` + "`command`" + ` is already set to run this file directly. Claude Code and GitHub
Copilot both expose tools via MCP; ` + "`agentworks export ... --target claude-code`" + `
or ` + "`--target github-copilot`" + ` uses ` + "`command`" + `/` + "`auth`" + ` to generate the server
registration, passing each ` + "`auth`" + ` entry through as an env var reference, never
a literal secret.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"src/main.py", `#!/usr/bin/env python3
"""` + name + `: a thin wrapper around a REST API.

Reads API_BASE_URL and API_TOKEN from the environment and exposes one
function per endpoint you want to call. This shows the API-calling shape --
wire it into a real MCP server loop for the actual protocol handling.
"""

import os
import requests


BASE_URL = os.environ["API_BASE_URL"]
TOKEN = os.environ["API_TOKEN"]


def _get(path: str, **params) -> dict:
    resp = requests.get(
        f"{BASE_URL}{path}",
        headers={"Authorization": f"Bearer {TOKEN}"},
        params=params,
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def main() -> None:
    raise NotImplementedError("` + name + ` is not implemented yet -- replace main() with real endpoint calls")


if __name__ == "__main__":
    main()
`},
				}
			},
			extraDirs: []string{"tests"},
		},
	},
	{
		ID: "cli-wrapper", Kind: artifact.KindTool,
		Title:       "CLI wrapper",
		Description: "Wraps a local command-line tool as an MCP tool.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "src/main.py",
					"command":    "python3 src/main.py",
					"auth":       []string{},
				}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Interface

Name the underlying CLI this wraps and describe the arguments/flags this
tool exposes to the agent.

## Implementation

` + "`src/main.py`" + ` shows the subprocess-calling shape -- wire it into whatever MCP
server framework you're using for the actual stdio loop. Set ` + "`auth`" + ` in this
file's frontmatter if the underlying CLI needs credentials via env vars.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"src/main.py", `#!/usr/bin/env python3
"""` + name + `: a thin wrapper around a local CLI tool.

Shows the subprocess-calling shape -- wire it into a real MCP server loop
for the actual protocol handling.
"""

import subprocess


def run(*args: str) -> str:
    result = subprocess.run(args, capture_output=True, text=True, check=True)
    return result.stdout


def main() -> None:
    raise NotImplementedError("` + name + ` is not implemented yet -- replace main() with a real run(...) call")


if __name__ == "__main__":
    main()
`},
				}
			},
			extraDirs: []string{"tests"},
		},
	},
	{
		ID: "pre-commit-lint", Kind: artifact.KindHook,
		Title:       "Pre-commit lint",
		Description: "Runs a lint/format check before a tool use is allowed to proceed.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"events": []string{"PreToolUse"}, "command": "echo 'replace with a real lint command'"}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

Set ` + "`command`" + ` in this file's frontmatter to the real lint/format command for
this project (e.g. ` + "`gofmt -l .`" + `, ` + "`eslint .`" + `). A non-zero exit blocks the
tool use it's attached to -- keep it fast, since it runs on every matching event.
`,
		},
	},
	{
		ID: "notify-webhook", Kind: artifact.KindHook,
		Title:       "Notify webhook",
		Description: "Posts a notification to a webhook URL on a lifecycle event.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"events":  []string{"SessionStart", "SessionEnd"},
					"command": `curl -sf -X POST "$WEBHOOK_URL" -H "Content-Type: application/json" -d "{\"event\":\"$AGENTWORKS_EVENT\"}"`,
				}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

Set ` + "`WEBHOOK_URL`" + ` in your environment before this hook runs. Adjust ` + "`events`" + `
to whichever lifecycle points should trigger a notification, and the payload
in ` + "`command`" + ` to whatever your receiving endpoint expects.
`,
		},
	},
	{
		ID: "research-then-act", Kind: artifact.KindWorkflow,
		Title:       "Research then act",
		Description: "An agent investigates, then a tool acts on what it found.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"steps": []map[string]string{}}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

This template pairs a research-style agent with a tool that acts on its
findings -- e.g. an agent that investigates an issue, then a tool that files
a ticket for it. Create both artifacts first, then set ` + "`steps`" + ` in this
file's frontmatter to reference them by name, in order:

` + "```yaml" + `
steps:
  - agent: researcher
  - tool: your-tool-name
` + "```" + `
`,
		},
	},
	{
		ID: "fetch-then-review", Kind: artifact.KindWorkflow,
		Title:       "Fetch then review",
		Description: "A tool fetches data, then an agent reviews it.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"steps": []map[string]string{}}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

This template pairs a data-fetching tool with a reviewing agent -- e.g. a
tool that pulls a pull request's diff, then an agent that reviews it. Create
both artifacts first, then set ` + "`steps`" + ` in this file's frontmatter to
reference them by name, in order:

` + "```yaml" + `
steps:
  - tool: your-tool-name
  - agent: code-reviewer
` + "```" + `
`,
		},
	},
}
