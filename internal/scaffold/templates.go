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
		ID: "reference-file-qa", Kind: artifact.KindAgent,
		Title:       "Reference file Q&A",
		Description: "Answers questions strictly from a set of provided reference files, citing which one backs each answer.",
		spec: kindSpec{
			extra: func(name string) map[string]any { return map[string]any{} },
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Reference material

Put the markdown (or other text) files this agent should answer from under
` + "`resources/`" + `. List them here so it's clear what's in scope, e.g.:

- ` + "`resources/architecture.md`" + ` -- TODO: what this covers
- ` + "`resources/faq.md`" + ` -- TODO: what this covers

## Guidance

- Answer only from the files under ` + "`resources/`" + `; don't fill gaps from outside
  knowledge or guess at anything the files don't state.
- If the answer isn't in the reference material, say so explicitly rather
  than inferring one.
- When files disagree, surface the conflict instead of silently picking a
  side.

## Output format

Give a direct answer, then cite which file(s) it came from (e.g. "per
` + "`resources/architecture.md`" + `"). Keep citations next to the claim they support,
not bundled at the end.
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
		ID: "pptx-style-refresh", Kind: artifact.KindSkill,
		Title:       "PowerPoint style refresh",
		Description: "Applies a defined design style to an existing PowerPoint deck without changing its content, for Microsoft 365 Copilot's PowerPoint skill.",
		spec: kindSpec{
			extra: func(name string) map[string]any { return map[string]any{} },
			bodyTmpl: `# {{.Title}}

{{.Description}}

## When to use this

Invoke this when a deck's content is final but its look needs to match a
defined design style -- a rebrand, a template swap, or bringing an
inconsistent deck in line with one set of rules.

## Design style

Edit ` + "`styles/design-style.md`" + ` with the deck's actual design rules (palette,
type, logo placement, slide-master layout choices). Treat that file as the
source of truth -- don't invent style choices that aren't in it.

## Rules

- Change layout, color, type, and imagery treatment only -- never rewrite,
  summarize, or reorder the deck's actual content.
- Reuse the deck's existing slide layouts/masters where the style allows it
  instead of building one-off slide designs.
- Flag any slide that doesn't fit the defined style cleanly (e.g. a dense
  table, an odd aspect-ratio image) rather than forcing a bad fit silently.

## Output format

After restyling, list which slides changed and what was touched on each, so
the outcome can be reviewed against the original deck.

## Exporting to Microsoft 365 Copilot

` + "`agentworks export <path> --target m365-copilot`" + ` packages this skill as a
declarative agent whose instructions are this file's body -- keep the
sections above self-contained, since ` + "`styles/design-style.md`" + ` itself isn't
exported with it.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"styles/design-style.md", `# Design style

TODO: fill in this deck's actual design rules.

## Color palette

- Primary:
- Secondary:
- Accent:
- Background:

## Typography

- Heading font:
- Body font:

## Logo & branding

- Logo placement:
- Minimum clear space:

## Layout rules

- Preferred slide layouts:
- Rules for tables/charts/images:
`},
				}
			},
			extraDirs: []string{"samples"},
		},
	},
	{
		ID: "xlsx-workbook-updater", Kind: artifact.KindSkill,
		Title:       "Excel workbook updater",
		Description: "Updates an existing Excel workbook's data and formulas while preserving its structure, for Microsoft 365 Copilot's Excel skill.",
		spec: kindSpec{
			extra: func(name string) map[string]any { return map[string]any{} },
			bodyTmpl: `# {{.Title}}

{{.Description}}

## When to use this

Invoke this when a workbook already exists and needs new or corrected data,
without breaking the formulas, named ranges, or formatting other sheets
depend on.

## Workbook map

Edit ` + "`reference/workbook-map.md`" + ` with the workbook's actual layout (what
each sheet is for, what each column means, which cells are formulas vs. raw
input). Treat that file as the source of truth for how the workbook is
structured before making any change.

## Rules

- Only edit the cells/ranges the request actually calls for -- don't
  reformat or restructure sheets outside that scope.
- Never overwrite a formula cell with a hard-coded value; extend a formula's
  pattern into new rows/columns instead of writing one-off values.
- Preserve existing number formats, named ranges, and data validation
  unless the request specifically asks to change them.
- If a change would break a formula or named range elsewhere in the
  workbook, flag it instead of applying it silently.

## Output format

After updating, list which sheets/ranges changed and what changed in each,
so the edit can be checked against the workbook map.

## Exporting to Microsoft 365 Copilot

` + "`agentworks export <path> --target m365-copilot`" + ` packages this skill as a
declarative agent whose instructions are this file's body -- keep the
sections above self-contained, since ` + "`reference/workbook-map.md`" + ` itself isn't
exported with it.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"reference/workbook-map.md", `# Workbook map

TODO: fill in this workbook's actual structure.

## Sheets

- ` + "`Sheet1`" + `: TODO -- purpose of this sheet

## Columns

- ` + "`Sheet1!A`" + `: TODO -- what this column holds, and whether it's raw input or a formula

## Named ranges

- TODO: name -> what it refers to and what depends on it

## Formula conventions

- TODO: any pattern formulas should follow (e.g. "always SUMIFS against the Date column")
`},
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
