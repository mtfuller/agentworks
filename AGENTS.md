# AGENTS.md

Guidance for coding agents working in this repository.

## What this project is

**AgentWorks** is a local-first, vendor-agnostic tool for building agent contexts,
skills, tools, hooks, and workflows: author them once as plain files, then export
target-specific artifacts for the AI harnesses people actually use (Claude Code,
ChatGPT, GitHub Copilot, Microsoft 365 Copilot, and others).

The repo was bootstrapped from `starterpack-go-cli` (a Go/Cobra CLI template) — the
logging, color output, and Taskfile are that template's conventions, kept because
they're a reasonable foundation, not because they're AgentWorks-specific. The core
model (project manifest, artifact kinds, scaffolding, the vendor target registry, and
the `claude-code` skill exporter) is implemented; see Architecture below for what's
real today versus deliberately deferred.

## Guiding scenarios

These are the user scenarios the design should stay accountable to (see the project
brief for full detail — ask the user if it's not in context). Treat them as the source
of truth for what "done" looks like for any given piece of functionality:

1. **Data analyst** — a skill (Python script + tests + sample CSVs) validated locally by
   running its test scripts, then exported as both a ChatGPT skill upload and a
   Microsoft 365 Copilot zip, versioned per export. *(Scaffold + local test-running
   work today, see `examples/starter-project/skills/csv-analyzer`; ChatGPT/M365 export
   is the main gap — only `claude-code` has a real exporter so far.)*
2. **Developer** — a tool that fetches/parses a Jira ticket (real API + auth, with a
   simulated API for tests), validated locally, then exported to both Claude Code and
   GitHub Copilot. *(Tool scaffolding + local testing work; both exporters are gaps.)*
3. **AI researcher** — a domain-specific agent with its own guidance markdown and
   resources, exported to ChatGPT, Claude, and others as drag-and-drop artifacts.
   *(Agent scaffolding works; export for any target is a gap — even `claude-code` only
   exports skills today.)*
4. **Engineering leader** — multiple agents/tools/MCP servers composed into pipelines
   ("software factory" workflows), targeted at Claude Code and GitHub Copilot, exported
   as one or more plugins. *(Workflow artifacts can be scaffolded and reference other
   artifacts by name in `steps:`; there's no execution engine or plugin exporter yet.)*
5. **Any user** — guided boilerplate generation for a new tool/agent/workflow, including
   generated sample artifacts to learn the system from. *(`agentworks new`'s interactive
   wizard + `examples/starter-project`.)*
6. **Any user** — a project layout that's navigable in a plain text editor, with local
   test/validate/simulate workflows and room to build genuinely custom, heavier tooling
   when a scenario needs it. *(The `<kind>.md` format + `validate`/`test` commands.)*
7. **Any user** — a polished TUI for building agents/tools/skills, with control over
   whether an export is a standalone skill zip or a full plugin. *(`agentworks tui`
   browses the project; it doesn't yet drive scaffolding/export itself — CLI-only for
   those actions so far.)*
8. **Any user** — early, explicit visibility into which capabilities are portable across
   every target vendor versus specific to a subset, before investing effort in either.
   *(`agentworks targets`.)*

Implication for design: the artifact model (agents, skills, tools, hooks, workflows)
must be defined independently of any vendor's format, with per-target exporters/
transforms layered on top — never model something in a single vendor's native shape
and reverse-engineer the rest.

## Architecture (keep these layers intact)

```
main.go → cmd/ (Cobra commands, CLI surface) → internal/tui (Bubble Tea browser + `new` wizard)
                                              → internal/{artifact,project,scaffold,targets}
```

- **`cmd/`** — one file per Cobra command (`init`, `new`, `list`, `validate`, `test`,
  `targets`, `export`, `tui`, `version`). Commands parse flags/args, call into the
  packages below, and format output. Keep business logic out of `Run`/`RunE` — a
  command file should read as "gather input, call one function, print the result."
- **`internal/artifact`** — the vendor-agnostic artifact model: `Kind`
  (agent/skill/tool/hook/workflow), `Frontmatter` (common fields explicit, kind-specific
  fields round-trip through `Extra` so there's one parser for all five kinds), and
  `Parse`/`Render`/`Load`/`Save` for the `<kind>.md` (YAML frontmatter + Markdown body)
  file format. This is the one place that understands that file format — nothing else
  should hand-parse it.
- **`internal/project`** — `agentworks.yaml` (the project manifest), `Init` (scaffold a
  new project), `FindRoot` (walk upward for the manifest, like git finds `.git`), and
  `Discover` (walk the 5 kind directories and load every artifact).
- **`internal/scaffold`** — `New(root, kind, name, opts)` writes a new artifact's
  directory + starter files. This is the single code path both `cmd/new.go` and the TUI
  wizard call — never generate an artifact's files by hand in either front end.
- **`internal/targets`** — the static vendor registry (which artifact kinds each vendor
  can consume — this is what `agentworks targets` prints) and the `Exporter` interface.
  Vendor-specific exporters live in their own subpackage (e.g.
  `internal/targets/claudecode`) and self-register via `init()` + `targets.Register`;
  `cmd/export.go` blank-imports each implemented one.
- **`internal/tui`** — the Bubble Tea project browser (`model.go`/`app.go`) and the
  `huh`-based interactive wizard (`wizard.go`) that both `cmd/new.go` (non-interactive
  runs skip it) and the browser's future "create artifact" action call.
- **`internal/{logger,color,spinner,version}`** — CLI-support code inherited from the
  starter template (leveled logging, ANSI output helpers, a progress spinner, build
  metadata via ldflags).
- **`pkg/`** — currently unused. Reserved for logic that should be importable by other
  Go programs, not just this CLI; nothing has needed that yet.
- **`tests/`** — black-box integration tests that exec the built CLI. Unit tests live
  next to their package (`internal/artifact/artifact_test.go`, etc.), not here.

### What's real vs. deferred

Implemented: the project/artifact model, scaffolding for all 5 kinds, project-wide
discovery/validation/test-running, the vendor capability matrix, a real `claude-code`
skill exporter (directory or `--zip`), and the Bubble Tea browser + `new` wizard.

Deliberately deferred (do this later, not by accident while doing something else):
exporters for `chatgpt`/`github-copilot`/`m365-copilot` and for non-skill kinds on
`claude-code`; an actual workflow *execution* engine (a workflow's `steps:` today is
just documentation an exporter could read, not something AgentWorks runs); wiring
export/test actions into the TUI itself (it's browse-only for now).

## Conventions

- Go 1.24+. `gofmt` formatting. Package names lowercase, single word.
- Commands follow the Cobra pattern: `Use`/`Short`/`Long` fields, flags bound in
  `init()`, `AddCommand()` registered in the same `init()`. Prefer `RunE` (return an
  error) over `Run` + `os.Exit` so Cobra can format the error consistently.
- User-facing output goes through `internal/color` (`color.Success`, `color.Error`,
  `color.Warning`, `color.Info`) — never raw `fmt.Println` for something the user reads
  as a result. Diagnostic/debug output goes through `internal/logger`
  (`logger.Debug/Info/Warn/Error`), controlled by the global `--verbose`/`--log-level`
  flags wired in `cmd/root.go`.
- Errors: always wrap with context (`fmt.Errorf("...: %w", err)`) and return them
  rather than calling `os.Exit` inside command logic.
- Full guidance: `.github/instructions/STYLEGUIDE.instructions.md` (code conventions)
  and `.github/instructions/TESTING.instructions.md` (test conventions) — both apply
  regardless of which coding tool is reading them, and go deeper than this file.

## Common commands

- `task build` — build the binary (embeds version/commit/date via ldflags).
- `task run` — `go run main.go`.
- `task test` / `task test-unit` / `task test-integration` — see Taskfile.yml.
- `task coverage` — coverage report (`coverage.html`).
- `go test ./... -v` / `go test -race ./...` — direct Go invocations work too; Task is
  a convenience wrapper, not a requirement.
- No `task` binary available? Read `Taskfile.yml` and run the underlying `go`/`gofmt`
  commands directly — the tasks are thin wrappers.

## Workflows

Repeatable procedures live in `.claude/skills/` (inherited from the starter template):

- `bootstrap-project` — turn the template into a new, real project. Already run for
  this repo; only relevant again if re-templating from scratch.
- `add-command` — scaffold a new Cobra command (flags, tests, registration).
- `add-package` — scaffold a new `internal/` or `pkg/` package with unit tests.

## Definition of done for a change

1. `gofmt -l .` reports nothing changed.
2. `task test` (or `go test ./...`) passes.
3. New commands/flags have `Short`/`Long` help text and are covered by an integration
   test in `tests/integration_test.go`.
4. `README.md`'s command list / project structure section updated if the change adds,
   removes, or renames a command or top-level directory.
5. New functionality is checked against the guiding scenarios above — note which
   scenario(s) it serves, and whether it assumes a single vendor's format where a
   vendor-agnostic model should sit instead.
6. If the change touches `internal/scaffold`'s output shape, regenerate the relevant
   part of `examples/starter-project` (via the actual CLI, then hand-edit as needed) so
   it keeps demonstrating what `agentworks new` really produces.
