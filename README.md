# AgentWorks

A local-first, vendor-agnostic tool for building agent contexts, skills, tools, hooks,
and workflows — author them once as plain files, then export target-specific artifacts
for the AI harnesses you actually use (Claude Code, ChatGPT, GitHub Copilot,
Microsoft 365 Copilot, Cursor, Gemini CLI, and others).

## Why

The industry has started standardizing pieces of the agent-tooling stack (e.g.
`AGENTS.md`), but most of it — skills, tools, hooks, agent definitions, workflows — is
still vendor-specific, and that's likely to stay true for a while. AgentWorks doesn't
bet on any one vendor winning; it lets you build against a vendor-agnostic core model
and generate whatever vendor-specific format you need from it, so switching or
supporting multiple harnesses doesn't mean maintaining N copies by hand.

See [AGENTS.md](AGENTS.md) for the guiding scenarios and how the project is organized
for agentic development.

## Quick Start

### Prerequisites

- Go 1.24 or higher
- [Task](https://taskfile.dev) (optional, for build automation)

### Installation

```bash
git clone git@github.com:mtfuller/agentworks.git
cd agentworks
task build
./agentworks --help
```

### Try it

```bash
./agentworks init my-project
cd my-project
../agentworks new skill csv-analyzer --description "Analyze a CSV and flag rows that stand out." --target claude-code
../agentworks list
../agentworks validate
../agentworks targets
../agentworks export skills/csv-analyzer --target claude-code --out dist
../agentworks tui
```

Or look at [`examples/starter-project`](examples/starter-project) — a checked-in
project with one of each artifact kind, including two real, tested implementations (a
CSV-outlier skill and a Jira-fetching MCP server tool) — to see a finished example
without building one yourself.

## A project, on disk

`agentworks init` creates a project manifest, one directory per artifact kind, and an
`AGENTS.md` + `.agents/skills/agentworks-cli/` skill so a coding agent working in the
project immediately knows how to drive the CLI (neither is overwritten if it already
exists):

```
myproject/
├── agentworks.yaml       # project manifest: name, description, default targets, publisher (optional)
├── AGENTS.md             # guidance for coding agents working in this project
├── .agents/
│   └── skills/
│       └── agentworks-cli/
│           └── SKILL.md  # how to scaffold/validate/test/export with agentworks
├── agents/
├── skills/
├── tools/
├── hooks/
└── workflows/
```

Each artifact is a directory containing one `<kind>.md` file — YAML frontmatter plus a
Markdown body, the same shape as Claude Code's own `SKILL.md` — alongside whatever
supporting files it needs (scripts, tests, samples, resources). It's navigable in a
plain text editor; no build step is needed to read or edit it. For example:

```
skills/csv-analyzer/
├── skill.md
├── scripts/main.py
├── tests/test_main.py
└── samples/sample.csv
```

```yaml
---
kind: skill
name: csv-analyzer
description: Analyze a CSV file and flag rows that stand out.
version: 0.1.0
targets: [claude-code, chatgpt, m365-copilot]
entrypoint: scripts/main.py
test: python3 -m unittest discover -s tests -p "test_*.py"
---

# CSV Analyzer

<instructions / prompt body the vendor sees>
```

`agents/`, `tools/`, `hooks/`, and `workflows/` follow the same `<kind>.md` shape, each
with a few kind-specific frontmatter fields (a tool's `entrypoint`/`test`/`command`/
`auth` -- `command` and `auth` are what `agentworks export` turns into an MCP server
registration -- a hook's `events`/`command`, a workflow's `steps` referencing other
artifacts by name). An agent can also set `tools:` (a list) and `model:` (a tier) --
a small, closed, vendor-agnostic vocabulary (`agentworks validate` lists the
recognized values) that `agentworks export` maps to each target's real shape: Claude
Code's `tools:`/`model:` subagent frontmatter, and Microsoft 365's declarative-agent
`capabilities` array. GitHub Copilot's custom-agent spec doesn't confirm these fields
yet, so that mapping is deliberately deferred (see AGENTS.md).

A skill or agent can also have an `evals/` directory (a starter one is scaffolded
automatically) of `<kind>.md`-adjacent YAML case files for `agentworks eval` -- see
that command below.

An artifact can optionally be namespace-scoped -- `agentworks new skill
team-a/csv-analyzer` sets `namespace: team-a` in its frontmatter and nests it at
`skills/team-a/csv-analyzer/skill.md` instead of `skills/csv-analyzer/skill.md` -- so
two teams (or two projects merged into one registry) can each have their own
`csv-analyzer` without colliding. Reference it the same qualified way everywhere else
an artifact is named: a workflow's `steps:`, `agentworks validate`/`export`'s path
argument, `--bundle` members. An unnamespaced artifact is unaffected either way.

`agentworks add`/`export` write an `agentworks.lock` at the project root -- see
"Drift and supply-chain safety" below.

## Sharing code between Node artifacts

Every artifact -- including a `node-skill`/`node-tool` -- is a self-contained,
independently-copyable leaf directory (its own `package.json`, its own
`node_modules`), so `agentworks export`/`agentworks add` can move one in or out
without dragging along a shared workspace root. That's deliberate, not an oversight
(see AGENTS.md) -- but it still leaves the question of how to share real logic
between several small Node artifacts without duplicating it everywhere. The
recommended pattern:

1. Put shared code in its own ordinary npm package under `packages/<name>/` at the
   project root. AgentWorks never looks there -- `list`/`validate`/`test`/`build`/
   `export` only ever walk `agents/ skills/ tools/ hooks/ workflows/` -- so it's just
   a normal npm package with its own `package.json` and its own tests (plain `npm
   test`, run directly rather than through `agentworks test`, since it isn't an
   artifact).
2. From a Node artifact, depend on it like any other package during development, via
   a relative `file:` reference in that artifact's own `package.json`:
   ```json
   "dependencies": {
     "@myproject/shared-lib": "file:../../packages/shared-lib"
   }
   ```
   `npm install` resolves this to a symlink, so edits to the shared package are
   picked up immediately -- nothing to publish for local dev.
3. A `file:` dependency is a symlink into a path that only exists inside this
   project, so it won't survive the artifact directory being copied out by
   `export`/`add`. Give the artifact a `build:` command that bundles the shared code
   in first (e.g. `npm install && npx esbuild src/index.js --bundle --platform=node
   --outfile=dist/index.js`), and point its `command:`/`entrypoint:` at the bundled
   output rather than the raw source. Run `agentworks build <path>` before
   `agentworks export` -- export itself doesn't need anything special, it already
   just copies whatever's in the directory (`node_modules` is never included, whether
   or not you use this pattern).

The `node-ts-skill`/`node-ts-tool` templates (`agentworks new tool <name>
--from-template node-ts-tool`) already set this up out of the box -- TypeScript
source, a `tsconfig.json`, and a `build:` command that type-checks with `tsc` and
bundles with `esbuild` into a single `dist/` output, which is exactly what step 3
above needs. See `agentworks templates` for the full list.

## Commands

| Command | What it does |
| --- | --- |
| `agentworks init [path]` | Scaffold a new project (`agentworks.yaml`, the 5 kind directories, and an `AGENTS.md` + `.agents/skills/agentworks-cli/SKILL.md` teaching a coding agent how to drive this CLI in the project). |
| `agentworks new <kind> [name]` | Scaffold a new agent/skill/tool/hook/workflow. Give `--description` (and kind/name) for a non-interactive run; leave any out in a terminal and a short wizard fills in the rest. Pass `--from-template <id>` to start from a curated built-in template instead of the generic blank scaffold (see `agentworks templates`) — its own description covers you if you don't pass `--description`. |
| `agentworks templates [kind]` | Table of the built-in starter templates `--from-template` can scaffold from (two per kind for hook/workflow, three for agent, four for tool, six for skill: e.g. a tool's `api-wrapper`/`cli-wrapper`/`node-tool`/`node-ts-tool`, a workflow's `research-then-act`/`fetch-then-review`, a skill's `pptx-style-refresh`/`xlsx-workbook-updater` for Microsoft 365 Copilot's PowerPoint/Excel skills, or `node-skill`/`node-ts-skill`/`node-tool`/`node-ts-tool` for a Node.js or TypeScript implementation instead of the Python default -- the TypeScript variants bundle with esbuild via a `build:` command). Pass a kind to filter. |
| `agentworks list [kind]` | Table of the project's discovered artifacts. |
| `agentworks validate [path]` | Parse and validate one artifact or the whole project. Beyond the generic checks (name/description/kind), also catches export-readiness gaps per kind: a workflow's `steps:` must resolve to real artifacts, a hook's `events`/`command` must be set together, and a tool declaring `auth` must also declare `command`. Also lints description quality — too long (over the [Agent Skills spec](https://agentskills.io/specification)'s 1024-character limit), too short/vague, redundant with the name, or overlapping heavily with another same-kind artifact's description (checked project-wide) — and flags a hook/tool `command` that will run arbitrary shell code (see "Drift and supply-chain safety") — all printed as warnings that don't fail the command unless `--strict` is passed. |
| `agentworks build [path]` | Run the `build:` command an artifact declares in its frontmatter (any language — AgentWorks just shells out to it, e.g. installing dependencies, compiling, or bundling before the artifact can run or be exported). With no path, runs every artifact that declares one. |
| `agentworks test [path]` | Run the `test:` command an artifact declares in its frontmatter (any language — AgentWorks just shells out to it). |
| `agentworks eval [path]` | Behavior-test a skill/agent: for each case under its `evals/` directory, pipe the case's `prompt` to the artifact's `eval_runner` command (or the project's `agentworks.yaml` `eval.default_runner` if it doesn't set its own) and check the runner's stdout against the case's `assert` rules (`contains`/`not_contains`/`matches`/`not_matches`/`max_length`/`min_length`). AgentWorks never calls a model itself here — `eval_runner` is your own shell command (a script calling whatever model/API you want, `claude -p`, or anything else reading a prompt on stdin and printing a response on stdout), the same "orchestrate, don't execute" split `agentworks test` and workflow export already follow. With no path, runs every artifact with an `evals/` directory; artifacts without one, or without a runner configured, are skipped rather than failed. Pass `--case <name>` to re-run a single case. |
| `agentworks doctor [path]` | A static, side-effect-free preflight check: resolves every declared shell command's (`command:`/`test:`/`build:`/`eval_runner:`) interpreter/binary against `PATH`, checks a declared `entrypoint:` file actually exists, and checks a tool's `auth:` environment variables are set (a warning, not a failure, unless `--strict` — they're only needed to actually call the tool, not to discover what it offers). With no path, checks every artifact in the project. Run this before `agentworks run` if you're not sure the command/environment is even set up. |
| `agentworks run <tool>` | Start a tool artifact's declared `command` as a real MCP server and open a full-screen inspector: browse the tools it exposes, fill in and submit a call from a form generated off each tool's `inputSchema`, and see the result — the same way an agent actually would, instead of only unit-testing the tool's logic with mocked calls. Also shows a call-history pane and the raw JSON-RPC/stderr traffic (reachable even from a connection-failure screen, so the real cause isn't hidden behind a generic protocol error). Only tool artifacts qualify; unlike `export`'s `${VAR}` placeholders, this actually executes with your real environment. Needs an interactive terminal. |
| `agentworks targets` | Print the capability matrix: which artifact kinds each vendor target supports, and whether a real exporter exists yet. |
| `agentworks export <path> --target <id>` | Export an artifact to a vendor's native format. `claude-code` and `github-copilot` have a real exporter for all five kinds: skills (the shared [Agent Skills](https://agentskills.io/specification) format, also used by `chatgpt`), tools and workflow tool-steps as an MCP server registration (`.mcp.json` / Agent Plugins' `mcp.json`, `auth` env vars passed through as `${VAR}` references, never literal secrets), agents as a subagent file (`agents/<name>.md` / `com.github.copilot/agents/<name>.agent.md`), hooks as a lifecycle-event handler (`hooks/hooks.json` / `com.github.copilot/hooks/hooks.json`), and workflows as a bundled plugin composing all of the above plus a generated orchestrator command (the vendor's own agent loop runs it; AgentWorks doesn't execute anything itself). `m365-copilot` exports skills and agents as a declarative agent in a Microsoft 365 app package zip. Its `manifest.json` developer/privacy/terms fields come from an optional `publisher:` block in `agentworks.yaml` (`name`/`website`/`privacy_url`/`terms_url`/`accent_color`) when a project sets one; otherwise they're clearly-labeled placeholders, and the CLI warns you after export so it's not a silent gap. `cursor` and `gemini-cli` have a real exporter for skills/agents/tools/hooks (no workflow -- neither vendor has an orchestration/bundle format), each writing loose, project-scoped files rather than a plugin: Cursor a project rule (`.cursor/rules/<name>.mdc`), a subagent file (`.cursor/agents/<name>.md`, name/description only -- Cursor's docs don't confirm a tools/model mapping yet), `.cursor/mcp.json`, and `.cursor/hooks.json`; Gemini CLI a distributable extension directory (`gemini-extension.json` + `GEMINI.md` for a skill, or just an `mcpServers`-only manifest for a bare tool), a subagent file (`.gemini/agents/<name>.md`, with a real tools/model mapping), and a `.gemini/settings.json` hooks fragment meant to be merged by hand (Gemini CLI hooks live only in a shared settings file, not an extension-scoped format). See AGENTS.md, "Cursor and Gemini CLI: what's real vs. deferred," for the full reasoning. `agentworks targets` shows the full matrix. Pass `--all` (every artifact in the project) or `--kind <kind>` (every artifact of one kind) instead of a path to export the whole project in one call, each artifact to its own output; a kind the target can't consume is skipped with a warning rather than failing the run. Pass several paths (or one path with `--bundle <name>`) to package multiple artifacts into a single plugin instead of one per artifact -- only `claude-code`/`github-copilot` support this, since it's their native format that's actually meant to bundle several components together; a tool member's own `src/`-relative command is namespaced under `tools/<name>/` so multiple tools' files don't collide. |
| `agentworks add <url>` | Import a published skill or Claude Code plugin into this project — the reverse of `export`. Accepts an `owner/repo` GitHub shorthand, a full `github.com` repo/tree/blob URL, a `raw.githubusercontent.com` file URL, or a direct `.zip`/`.tar.gz` archive URL (including agentskills.codes's download links). A bare Agent Skill (`SKILL.md` at its root) becomes one skill artifact, supporting files included. A Claude Code plugin (`.claude-plugin/plugin.json` at its root) decomposes into one artifact per skill/agent it contains; tools and hooks inside a fetched plugin aren't supported yet and are reported, not silently dropped. A name that doesn't fit AgentWorks' slug rules is converted automatically, with the original preserved in a `source:` provenance block alongside where it came from. `--name` overrides the derived name (single-skill imports only); `--dry-run` shows what would be imported without writing anything. If any imported content declares a shell `command` (see "Drift and supply-chain safety"), it's printed and you're asked to confirm — `--yes` skips that prompt for scripted use. Every import is pinned in `agentworks.lock`. With no URL and no argument, it launches the marketplace search TUI in an interactive terminal. |
| `agentworks update [path...]` | Check artifacts imported with `add` for upstream changes: re-fetches each locked source and compares its content hash against what was pinned at import time. Report-only by default; `--apply` overwrites a changed artifact with the fresh content (refusing rather than silently renaming/moving it if upstream itself renamed the artifact) and updates the pin, subject to the same shell-command confirmation gate as `add` (`--yes` to skip it). With no path, checks every import in `agentworks.lock`. |
| `agentworks status [path]` | Fully offline check of `dist/` output against `agentworks.lock`'s export records: `in sync`, `stale` (source artifact changed, re-export), `modified` (dist was hand-edited since the last export — re-exporting discards it), or `missing`. |
| `agentworks marketplace` | Publish this project as a plugin marketplace repo a team can point Claude Code or GitHub Copilot at directly — see "Becoming a plugin marketplace repo" below. |
| `agentworks tui` | Full-screen Bubble Tea browser: drill from kind → artifact → its rendered frontmatter and body. Press `n` to scaffold a new artifact (the same wizard `agentworks new` uses), `e` to export the current one to a vendor target with a zip toggle, `t` to run its declared `test:` command, `a` to search and import from agentskills.codes plus the Claude Code and GitHub Copilot marketplaces, or `b` to browse/search the built-in starter templates and create straight from one — all run right there, no dropping back to the CLI. |
| `agentworks version` | Print version/commit/build-date info. |

Global flags: `-p, --project` (path inside the project to operate on, default `.`,
resolved upward like `git` finds a repo root), `-v, --verbose`, `-l, --log-level`.

## Drift and supply-chain safety

`agentworks add` and `agentworks export` both write to a single `agentworks.lock` at
the project root:

- **Imports** (`agentworks add`) are pinned by a content hash of the raw fetched
  source, plus where it came from (repo/ref/path). `agentworks update` re-fetches
  that same source later and reports (or, with `--apply`, applies) any drift — the
  "did an imported skill change upstream without me noticing" gap.
- **Exports** (`agentworks export`) are pinned by a hash of both the source
  artifact(s) and the exported output. `agentworks status` reads this back
  fully offline to tell a hand-edited `dist/` (edits export would silently discard)
  apart from a merely stale one (the source changed since the last export). `export`
  itself also warns before overwriting a hand-edited output.

Separately, `agentworks validate`, `agentworks add`, and `agentworks export` all scan
a hook/tool's `command` field — arbitrary shell that runs with your own permissions
the moment it's triggered/invoked — for both the fact that it exists and a small
denylist of shapes that are almost always hostile (piping a download into a shell,
a base64-decoded payload, a raw `/dev/tcp` reverse shell, `sudo`, setting a setuid
bit). `validate` and `export` only warn; `add` (and `update --apply`) require an
explicit `y`/`--yes` before writing anything that triggers a warning. This is a scan
and a confirmation gate, not a sandbox — it catches sloppy or obviously hostile
commands, not a determined obfuscator.

## Becoming a plugin marketplace repo

```bash
agentworks marketplace
```

turns the project itself into a repo a team can point Claude Code or GitHub
Copilot at directly, instead of exporting one artifact at a time. It exports
every eligible artifact into committed plugin directories under `plugins/`
(unlike `export`'s disposable, gitignored `dist/`), then writes a
`marketplace.json` listing them — `.claude-plugin/marketplace.json` for
Claude Code, `.github/plugin/marketplace.json` for GitHub Copilot, both by
default (pass `--target claude-code` or `--target github-copilot` to
generate just one). A team adds either as a plugin marketplace source (e.g.
Claude Code's `/plugin marketplace add <repo>`) and installs whatever
plugins it lists.

Skills, agents, tools, and hooks are grouped into one bundled plugin per
namespace — an unnamespaced artifact lands in one plugin named after the
project, and each `namespace:` (see "A project, on disk" above) gets its
own. Pass `--single` to collapse everything into one plugin regardless of
namespace. Workflows always export as their own plugin, since a workflow
can't be a bundle member (same rule `export --bundle` follows). Re-running
`agentworks marketplace` regenerates `plugins/` and both `marketplace.json`
files from scratch, so it stays in sync as artifacts are added, removed,
renamed, or re-namespaced — nothing from a previous run is left behind.
Exports are recorded in `agentworks.lock` exactly like `agentworks export`,
so `agentworks status` reports on them too.

## Development

### Running Tests

```bash
task test              # unit + integration
task test-unit
task test-integration
task coverage           # coverage.html
```

### Building

```bash
task build              # ./agentworks
task install             # to GOPATH/bin
```

### Project Structure

```
.
├── cmd/                        # Cobra commands (one file per command)
├── internal/
│   ├── artifact/               # vendor-agnostic artifact model (Kind, Frontmatter, <kind>.md parsing)
│   ├── project/                # agentworks.yaml manifest, project discovery
│   ├── scaffold/                # `new` boilerplate generation, one starter per kind, plus built-in named templates.go
│   ├── evalspec/                # `evals/*.yaml` case format + deterministic assertion checker for `agentworks eval`
│   ├── targets/                 # vendor registry (capability matrix) + Exporter interface
│   │   ├── agentskills/         # shared Agent Skills (agentskills.io) SKILL.md writer
│   │   ├── mcpconfig/           # shared MCP server-entry builder (for tool/workflow export)
│   │   ├── filecopy/            # shared copy-artifact-files / zip-a-directory helpers
│   │   ├── workflowsteps/       # shared `steps:` parser -- resolves agent/tool references
│   │   ├── agentcaps/           # vendor-agnostic agent tools/model vocabulary + per-vendor mapping
│   │   ├── claudecode/          # "claude-code": all 5 kinds, each a real Claude Code plugin
│   │   ├── chatgpt/             # the "chatgpt" skill exporter (wraps agentskills)
│   │   ├── githubcopilot/       # "github-copilot": all 5 kinds, each a real Agent Plugin
│   │   ├── m365copilot/         # the "m365-copilot" declarative agent + app package exporter
│   │   ├── cursor/              # "cursor": skill/agent/tool/hook, each a loose .cursor/ file (no plugin format)
│   │   └── geminicli/           # "gemini-cli": skill/tool as an extension, agent/hook as loose .gemini/ files
│   ├── importer/                 # the reverse of targets/: fetch + decompose a skill/plugin into local artifacts
│   ├── marketplace/               # agentskills.codes + well-known marketplace.json search (resolving to importer.Source), plus writing this project's own marketplace.json for `agentworks marketplace`
│   ├── tui/                     # Bubble Tea browser + `n`ew/`e`xport/`t`est/`a`dd/`b`rowse-templates actions
│   ├── mcpclient/                # minimal MCP stdio JSON-RPC client (initialize/tools-list/tools-call) for `agentworks run`
│   ├── inspector/                # Bubble Tea MCP inspector UI (tool list, call form, history, raw traffic log) behind `agentworks run`
│   ├── color/ logger/ spinner/ version/   # CLI-support packages from the starter template
├── examples/starter-project/    # a finished example project, one artifact of each kind
├── tests/                       # black-box CLI integration tests
├── main.go
├── Taskfile.yml
└── README.md
```

## Adding New Commands

To add a new CLI command, create a new file in the `cmd/` directory (or use the
`add-command` skill in `.claude/skills/`):

```go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/mtfuller/agentworks/internal/color"
)

var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "Description of my command",
    Run: func(cmd *cobra.Command, args []string) {
        color.Success("My command executed!")
    },
}

func init() {
    rootCmd.AddCommand(myCmd)
}
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
