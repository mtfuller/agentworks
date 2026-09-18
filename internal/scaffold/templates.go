package scaffold

import (
	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets/agentcaps"
)

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

// nodePackageJSON is the starter package.json for a Node-based scaffold
// (node-skill/node-mcp). main is the entrypoint file its "start" script runs.
func nodePackageJSON(name, main string) string {
	return `{
  "name": "` + name + `",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "start": "node ` + main + `",
    "test": "node --test tests/**/*.test.js"
  }
}
`
}

// nodeEntrypointPlaceholder is the starter body for a Node-based scaffold's
// entrypoint file (scripts/main.js or src/index.js).
func nodeEntrypointPlaceholder(name string) string {
	return `#!/usr/bin/env node
// ` + name + ` entrypoint. Replace with real logic.

function main() {
  throw new Error("` + name + ` is not implemented yet");
}

main();
`
}

// nodeTestPlaceholder is the starter test file for a Node-based scaffold, run
// via package.json's "test" script (Node's built-in test runner).
func nodeTestPlaceholder(name string) string {
	return `// Tests for ` + name + `. Wire this up with Node's built-in test runner (or
// your tool of choice), then add a ` + "`test:`" + ` command to this artifact's
// frontmatter.

import { test } from "node:test";
import assert from "node:assert/strict";

test("placeholder", () => {
  assert.ok(true);
});
`
}

// nodeTSPackageJSON is the starter package.json for a TypeScript-based
// scaffold (node-ts-skill/node-ts-mcp). entryTS is the TypeScript
// entrypoint the "build" script type-checks and bundles from; outJS is the
// bundled file the "start" script runs -- tsc never emits here (see
// nodeTSConfig), esbuild produces the actual runtime output.
func nodeTSPackageJSON(name, entryTS, outJS string) string {
	return `{
  "name": "` + name + `",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "start": "node ` + outJS + `",
    "build": "tsc --noEmit && esbuild ` + entryTS + ` --bundle --platform=node --format=esm --outfile=` + outJS + `",
    "test": "node --experimental-strip-types --test tests/**/*.test.ts"
  },
  "devDependencies": {
    "typescript": "^5.7.0",
    "esbuild": "^0.24.0",
    "@types/node": "^22.0.0"
  }
}
`
}

// nodeTSConfig is the starter tsconfig.json for a TypeScript-based
// scaffold. "noEmit" is deliberate: tsc here only type-checks ("build:"
// runs it with --noEmit before esbuild's own separate bundle step), it
// never produces the runtime output itself.
func nodeTSConfig() string {
	return `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "noEmit": true,
    "allowImportingTsExtensions": true,
    "types": ["node"]
  }
}
`
}

// nodeTSEntrypointPlaceholder is the starter body for a TypeScript-based
// scaffold's entrypoint file (scripts/main.ts or src/index.ts).
func nodeTSEntrypointPlaceholder(name string) string {
	return `// ` + name + ` entrypoint. Replace with real logic.

function main(): void {
  throw new Error("` + name + ` is not implemented yet");
}

main();
`
}

// nodeTSTestPlaceholder is the starter test file for a TypeScript-based
// scaffold. Node's built-in test runner can't execute ".ts" directly, so
// running this for real needs a TypeScript-aware runner -- package.json's
// own "test" script already uses Node's "--experimental-strip-types" flag
// for that; swap it for "tsx --test" or similar if you'd rather not depend
// on an experimental flag.
func nodeTSTestPlaceholder(name string) string {
	return `// Tests for ` + name + `. Node's built-in test runner can't execute ".ts"
// directly -- see package.json's "test" script -- then add a ` + "`test:`" + `
// command to this artifact's frontmatter once these are real.

import { test } from "node:test";
import assert from "node:assert/strict";

test("placeholder", () => {
  assert.ok(true);
});
`
}

var templates = []Template{
	{
		ID: "code-reviewer", Kind: artifact.KindAgent,
		Title:       "Code reviewer",
		Description: "Reviews a code change for correctness, security, and style.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"tools": []string{agentcaps.ReadFiles}, "model": agentcaps.ModelBalanced}
			},
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
			files: func(name string) []extraFile {
				return []extraFile{{"evals/example.yaml", evalExampleContent(name)}}
			},
			extraDirs: []string{"resources", "evals"},
		},
	},
	{
		ID: "researcher", Kind: artifact.KindAgent,
		Title:       "Researcher",
		Description: "Investigates a question and reports findings with sources.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"tools": []string{agentcaps.WebSearch, agentcaps.ReadFiles}, "model": agentcaps.ModelBalanced}
			},
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
			files: func(name string) []extraFile {
				return []extraFile{{"evals/example.yaml", evalExampleContent(name)}}
			},
			extraDirs: []string{"resources", "evals"},
		},
	},
	{
		ID: "reference-file-qa", Kind: artifact.KindAgent,
		Title:       "Reference file Q&A",
		Description: "Answers questions strictly from a set of provided reference files, citing which one backs each answer.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"tools": []string{agentcaps.ReadFiles}, "model": agentcaps.ModelFast}
			},
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
			files: func(name string) []extraFile {
				return []extraFile{{"evals/example.yaml", evalExampleContent(name)}}
			},
			extraDirs: []string{"resources", "evals"},
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
					{"evals/example.yaml", evalExampleContent(name)},
				}
			},
			extraDirs: []string{"samples", "evals"},
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
					{"evals/example.yaml", evalExampleContent(name)},
				}
			},
			extraDirs: []string{"samples", "evals"},
		},
	},
	{
		ID: "node-skill", Kind: artifact.KindSkill,
		Title:       "Node.js skill",
		Description: "A skill implemented in Node.js instead of Python, for logic that's easier to write in JavaScript or needs an npm package.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{"entrypoint": "scripts/main.js"}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Usage

Describe what invokes this skill and what it produces.

## Implementation

See ` + "`scripts/main.js`" + `. Run ` + "`npm install`" + ` in this directory if you add
dependencies to ` + "`package.json`" + `, or wire it into a ` + "`build:`" + ` command in this
file's frontmatter (` + "`agentworks build`" + `) if there's a compile/bundle step too. Add
real tests under ` + "`tests/`" + ` and a ` + "`test:`" + ` command to this file's frontmatter
once they exist, so ` + "`agentworks test`" + ` can run them.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"package.json", nodePackageJSON(name, "scripts/main.js")},
					{"scripts/main.js", nodeEntrypointPlaceholder(name)},
					{"tests/main.test.js", nodeTestPlaceholder(name)},
					{"evals/example.yaml", evalExampleContent(name)},
				}
			},
			extraDirs: []string{"samples", "evals"},
		},
	},
	{
		ID: "node-ts-skill", Kind: artifact.KindSkill,
		Title:       "TypeScript skill",
		Description: "A Node.js skill written in TypeScript and bundled with esbuild, for logic that benefits from static types.",
		spec: kindSpec{
			extra: func(name string) map[string]any {
				return map[string]any{
					"entrypoint": "scripts/main.ts",
					"build":      "npm install && npx tsc --noEmit && npx esbuild scripts/main.ts --bundle --platform=node --format=esm --outfile=dist/main.js",
				}
			},
			bodyTmpl: `# {{.Title}}

{{.Description}}

## Usage

Describe what invokes this skill and what it produces.

## Implementation

See ` + "`scripts/main.ts`" + `. ` + "`build:`" + ` in this file's frontmatter (` + "`agentworks build`" + `)
type-checks it with ` + "`tsc`" + ` and bundles it with ` + "`esbuild`" + ` into ` + "`dist/main.js`" + `
-- run it after editing before trying the skill for real. Add real tests under
` + "`tests/`" + ` (Node's built-in test runner can't execute ` + "`.ts`" + ` directly -- see
` + "`package.json`" + `'s own ` + "`test`" + ` script) and a ` + "`test:`" + ` command to this
file's frontmatter once they exist, so ` + "`agentworks test`" + ` can run them.
`,
			files: func(name string) []extraFile {
				return []extraFile{
					{"package.json", nodeTSPackageJSON(name, "scripts/main.ts", "dist/main.js")},
					{"tsconfig.json", nodeTSConfig()},
					{"scripts/main.ts", nodeTSEntrypointPlaceholder(name)},
					{"tests/main.test.ts", nodeTSTestPlaceholder(name)},
					{"evals/example.yaml", evalExampleContent(name)},
				}
			},
			extraDirs: []string{"samples", "evals"},
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
}
