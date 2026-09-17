# AgentWorks

A local-first, vendor-agnostic tool for building agent contexts, skills, tools, hooks,
and workflows — author them once as plain files, then export target-specific artifacts
for the AI harnesses you actually use (Claude Code, ChatGPT, GitHub Copilot,
Microsoft 365 Copilot, and others).

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

## Commands

| Command | What it does |
| --- | --- |
| `agentworks init [path]` | Scaffold a new project (`agentworks.yaml`, the 5 kind directories, and an `AGENTS.md` + `.agents/skills/agentworks-cli/SKILL.md` teaching a coding agent how to drive this CLI in the project). |
| `agentworks new <kind> [name]` | Scaffold a new agent/skill/tool/hook/workflow. Give `--description` (and kind/name) for a non-interactive run; leave any out in a terminal and a short wizard fills in the rest. Pass `--from-template <id>` to start from a curated built-in template instead of the generic blank scaffold (see `agentworks templates`) — its own description covers you if you don't pass `--description`. |
| `agentworks templates [kind]` | Table of the built-in starter templates `--from-template` can scaffold from (two per kind, three for agent, four for skill: e.g. a tool's `api-wrapper`/`cli-wrapper`, a workflow's `research-then-act`/`fetch-then-review`, a skill's `pptx-style-refresh`/`xlsx-workbook-updater` for Microsoft 365 Copilot's PowerPoint/Excel skills). Pass a kind to filter. |
| `agentworks list [kind]` | Table of the project's discovered artifacts. |
| `agentworks validate [path]` | Parse and validate one artifact or the whole project. Beyond the generic checks (name/description/kind), also catches export-readiness gaps per kind: a workflow's `steps:` must resolve to real artifacts, a hook's `events`/`command` must be set together, and a tool declaring `auth` must also declare `command`. Also lints description quality — too long (over the [Agent Skills spec](https://agentskills.io/specification)'s 1024-character limit), too short/vague, redundant with the name, or overlapping heavily with another same-kind artifact's description (checked project-wide) — printed as warnings that don't fail the command unless `--strict` is passed. |
| `agentworks test [path]` | Run the `test:` command an artifact declares in its frontmatter (any language — AgentWorks just shells out to it). |
| `agentworks eval [path]` | Behavior-test a skill/agent: for each case under its `evals/` directory, pipe the case's `prompt` to the artifact's `eval_runner` command (or the project's `agentworks.yaml` `eval.default_runner` if it doesn't set its own) and check the runner's stdout against the case's `assert` rules (`contains`/`not_contains`/`matches`/`not_matches`/`max_length`/`min_length`). AgentWorks never calls a model itself here — `eval_runner` is your own shell command (a script calling whatever model/API you want, `claude -p`, or anything else reading a prompt on stdin and printing a response on stdout), the same "orchestrate, don't execute" split `agentworks test` and workflow export already follow. With no path, runs every artifact with an `evals/` directory; artifacts without one, or without a runner configured, are skipped rather than failed. Pass `--case <name>` to re-run a single case. |
| `agentworks targets` | Print the capability matrix: which artifact kinds each vendor target supports, and whether a real exporter exists yet. |
| `agentworks export <path> --target <id>` | Export an artifact to a vendor's native format. `claude-code` and `github-copilot` have a real exporter for all five kinds: skills (the shared [Agent Skills](https://agentskills.io/specification) format, also used by `chatgpt`), tools and workflow tool-steps as an MCP server registration (`.mcp.json` / Agent Plugins' `mcp.json`, `auth` env vars passed through as `${VAR}` references, never literal secrets), agents as a subagent file (`agents/<name>.md` / `com.github.copilot/agents/<name>.agent.md`), hooks as a lifecycle-event handler (`hooks/hooks.json` / `com.github.copilot/hooks/hooks.json`), and workflows as a bundled plugin composing all of the above plus a generated orchestrator command (the vendor's own agent loop runs it; AgentWorks doesn't execute anything itself). `m365-copilot` exports skills and agents as a declarative agent in a Microsoft 365 app package zip. Its `manifest.json` developer/privacy/terms fields come from an optional `publisher:` block in `agentworks.yaml` (`name`/`website`/`privacy_url`/`terms_url`/`accent_color`) when a project sets one; otherwise they're clearly-labeled placeholders, and the CLI warns you after export so it's not a silent gap. `agentworks targets` shows the full matrix. Pass `--all` (every artifact in the project) or `--kind <kind>` (every artifact of one kind) instead of a path to export the whole project in one call, each artifact to its own output; a kind the target can't consume is skipped with a warning rather than failing the run. Pass several paths (or one path with `--bundle <name>`) to package multiple artifacts into a single plugin instead of one per artifact -- only `claude-code`/`github-copilot` support this, since it's their native format that's actually meant to bundle several components together; a tool member's own `src/`-relative command is namespaced under `tools/<name>/` so multiple tools' files don't collide. |
| `agentworks add <url>` | Import a published skill or Claude Code plugin into this project — the reverse of `export`. Accepts an `owner/repo` GitHub shorthand, a full `github.com` repo/tree/blob URL, a `raw.githubusercontent.com` file URL, or a direct `.zip`/`.tar.gz` archive URL (including agentskills.codes's download links). A bare Agent Skill (`SKILL.md` at its root) becomes one skill artifact, supporting files included. A Claude Code plugin (`.claude-plugin/plugin.json` at its root) decomposes into one artifact per skill/agent it contains; tools and hooks inside a fetched plugin aren't supported yet and are reported, not silently dropped. A name that doesn't fit AgentWorks' slug rules is converted automatically, with the original preserved in a `source:` provenance block alongside where it came from. `--name` overrides the derived name (single-skill imports only); `--dry-run` shows what would be imported without writing anything. With no URL and no argument, it launches the marketplace search TUI in an interactive terminal. |
| `agentworks tui` | Full-screen Bubble Tea browser: drill from kind → artifact → its rendered frontmatter and body. Press `n` to scaffold a new artifact (the same wizard `agentworks new` uses), `e` to export the current one to a vendor target with a zip toggle, `t` to run its declared `test:` command, `a` to search and import from agentskills.codes plus the Claude Code and GitHub Copilot marketplaces, or `b` to browse/search the built-in starter templates and create straight from one — all run right there, no dropping back to the CLI. |
| `agentworks version` | Print version/commit/build-date info. |

Global flags: `-p, --project` (path inside the project to operate on, default `.`,
resolved upward like `git` finds a repo root), `-v, --verbose`, `-l, --log-level`.

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
│   │   └── m365copilot/         # the "m365-copilot" declarative agent + app package exporter
│   ├── importer/                 # the reverse of targets/: fetch + decompose a skill/plugin into local artifacts
│   ├── marketplace/               # agentskills.codes + well-known marketplace.json search, resolving to importer.Source
│   ├── tui/                     # Bubble Tea browser + `n`ew/`e`xport/`t`est/`a`dd/`b`rowse-templates actions
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
