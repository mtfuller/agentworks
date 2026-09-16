# AGENTS.md

Guidance for coding agents working in this repository.

## What this project is

**AgentWorks** is a local-first, vendor-agnostic tool for building agent contexts,
skills, tools, hooks, and workflows: author them once as plain files, then export
target-specific artifacts for the AI harnesses people actually use (Claude Code,
ChatGPT, GitHub Copilot, Microsoft 365 Copilot, and others).

The repo was bootstrapped from `starterpack-go-cli` (a Go/Cobra CLI template) — the
`cmd`/`internal`/`pkg` layering, logging, color output, and Taskfile below are that
template's conventions, kept because they're a reasonable foundation, not because
they're AgentWorks-specific. The project itself (artifact model, skill/agent/tool
schemas, target exporters) is **not implemented yet**; today the CLI only has its base
command surface (`version`, global flags, logging).

## Guiding scenarios

These are the user scenarios the design should stay accountable to (see the project
brief for full detail — ask the user if it's not in context). Treat them as the source
of truth for what "done" looks like for any given piece of functionality:

1. **Data analyst** — a skill (Python script + tests + sample CSVs) validated locally by
   running its test scripts, then exported as both a ChatGPT skill upload and a
   Microsoft 365 Copilot zip, versioned per export.
2. **Developer** — a tool that fetches/parses a Jira ticket (real API + auth, with a
   simulated API for tests), validated locally, then exported to both Claude Code and
   GitHub Copilot.
3. **AI researcher** — a domain-specific agent with its own guidance markdown and
   resources, exported to ChatGPT, Claude, and others as drag-and-drop artifacts.
4. **Engineering leader** — multiple agents/tools/MCP servers composed into pipelines
   ("software factory" workflows), targeted at Claude Code and GitHub Copilot, exported
   as one or more plugins.
5. **Any user** — guided boilerplate generation for a new tool/agent/workflow, including
   generated sample artifacts to learn the system from.
6. **Any user** — a project layout that's navigable in a plain text editor, with local
   test/validate/simulate workflows and room to build genuinely custom, heavier tooling
   when a scenario needs it.
7. **Any user** — a polished TUI for building agents/tools/skills, with control over
   whether an export is a standalone skill zip or a full plugin.
8. **Any user** — early, explicit visibility into which capabilities are portable across
   every target vendor versus specific to a subset, before investing effort in either.

Implication for design: the artifact model (agents, skills, tools, hooks, workflows)
must be defined independently of any vendor's format, with per-target exporters/
transforms layered on top — never model something in a single vendor's native shape
and reverse-engineer the rest.

## Architecture (keep these layers intact)

```
main.go → cmd/ (Cobra commands, CLI surface) → internal/ (CLI-specific logic)
                                              → pkg/ (reusable libraries, no CLI dependency)
```

- **`cmd/`** — one file per Cobra command. Commands parse flags/args, call into
  `internal/`/`pkg/`, and format output. Keep business logic out of `Run`/`RunE`.
- **`internal/`** — CLI-specific support code: `logger` (leveled logging), `color`
  (ANSI output helpers), `spinner` (progress animation), `version` (build metadata via
  ldflags). Not importable outside this module — that's the point.
- **`pkg/`** — logic generic enough to be imported by other Go programs, not just this
  CLI. This is where the artifact model, validators, and target exporters should live
  once they exist — if a function doesn't reference Cobra, flags, or terminal output,
  it likely belongs here instead of `internal/`.
- **`tests/`** — black-box integration tests that exec the built CLI. Unit tests live
  next to their package (`internal/logger/logger_test.go`, etc.), not here.

## Conventions

- Go 1.21+. `gofmt` formatting. Package names lowercase, single word.
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
