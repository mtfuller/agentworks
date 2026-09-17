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

`agentworks init` creates a project manifest plus one directory per artifact kind:

```
myproject/
├── agentworks.yaml       # project manifest: name, description, default targets, publisher (optional)
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
artifacts by name).

## Commands

| Command | What it does |
| --- | --- |
| `agentworks init [path]` | Scaffold a new project (`agentworks.yaml` + the 5 kind directories). |
| `agentworks new <kind> [name]` | Scaffold a new agent/skill/tool/hook/workflow. Give `--description` (and kind/name) for a non-interactive run; leave any out in a terminal and a short wizard fills in the rest. |
| `agentworks list [kind]` | Table of the project's discovered artifacts. |
| `agentworks validate [path]` | Parse and validate one artifact or the whole project. Beyond the generic checks (name/description/kind), also catches export-readiness gaps per kind: a workflow's `steps:` must resolve to real artifacts, a hook's `events`/`command` must be set together, and a tool declaring `auth` must also declare `command`. |
| `agentworks test [path]` | Run the `test:` command an artifact declares in its frontmatter (any language — AgentWorks just shells out to it). |
| `agentworks targets` | Print the capability matrix: which artifact kinds each vendor target supports, and whether a real exporter exists yet. |
| `agentworks export <path> --target <id>` | Export an artifact to a vendor's native format. `claude-code` and `github-copilot` have a real exporter for all five kinds: skills (the shared [Agent Skills](https://agentskills.io/specification) format, also used by `chatgpt`), tools and workflow tool-steps as an MCP server registration (`.mcp.json` / Agent Plugins' `mcp.json`, `auth` env vars passed through as `${VAR}` references, never literal secrets), agents as a subagent file (`agents/<name>.md` / `com.github.copilot/agents/<name>.agent.md`), hooks as a lifecycle-event handler (`hooks/hooks.json` / `com.github.copilot/hooks/hooks.json`), and workflows as a bundled plugin composing all of the above plus a generated orchestrator command (the vendor's own agent loop runs it; AgentWorks doesn't execute anything itself). `m365-copilot` exports skills and agents as a declarative agent in a Microsoft 365 app package zip. Its `manifest.json` developer/privacy/terms fields come from an optional `publisher:` block in `agentworks.yaml` (`name`/`website`/`privacy_url`/`terms_url`/`accent_color`) when a project sets one; otherwise they're clearly-labeled placeholders, and the CLI warns you after export so it's not a silent gap. `agentworks targets` shows the full matrix. |
| `agentworks tui` | Full-screen Bubble Tea browser: drill from kind → artifact → its rendered frontmatter and body. Press `n` to scaffold a new artifact (the same wizard `agentworks new` uses), `e` to export the current one to a vendor target with a zip toggle, or `t` to run its declared `test:` command — all three run right there, no dropping back to the CLI. |
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
│   ├── scaffold/                # `new` boilerplate generation, one starter per kind
│   ├── targets/                 # vendor registry (capability matrix) + Exporter interface
│   │   ├── agentskills/         # shared Agent Skills (agentskills.io) SKILL.md writer
│   │   ├── mcpconfig/           # shared MCP server-entry builder (for tool/workflow export)
│   │   ├── filecopy/            # shared copy-artifact-files / zip-a-directory helpers
│   │   ├── workflowsteps/       # shared `steps:` parser -- resolves agent/tool references
│   │   ├── claudecode/          # "claude-code": all 5 kinds, each a real Claude Code plugin
│   │   ├── chatgpt/             # the "chatgpt" skill exporter (wraps agentskills)
│   │   ├── githubcopilot/       # "github-copilot": all 5 kinds, each a real Agent Plugin
│   │   └── m365copilot/         # the "m365-copilot" declarative agent + app package exporter
│   ├── tui/                     # Bubble Tea browser + `n`ew/`e`xport/`t`est actions
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
