# AGENTS.md

Guidance for coding agents working in this repository.

## What this project is

`starterpack-go-cli` is a **template**, not an application in its own right. It exists
to be cloned/copied and turned into a real Go CLI tool: the `greet`, `calc`, and
`process` commands are worked examples of the template's patterns (flags, colored
output, logging, a spinner), meant to be deleted once real commands replace them, not
built on top of.

When starting a new project from this template, see the `bootstrap-project` skill
first — it renames the module path and binary so the rest of the template's own
identity doesn't leak into the new project.

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
  CLI (see `pkg/example`). If a function doesn't reference Cobra, flags, or terminal
  output, it likely belongs here instead of `internal/`.
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

Repeatable procedures live in `.claude/skills/`:

- `bootstrap-project` — turn this template into a new, real project (rename module
  path, binary name, root command; strip the example commands).
- `add-command` — scaffold a new Cobra command (flags, tests, registration).
- `add-package` — scaffold a new `internal/` or `pkg/` package with unit tests.

## Definition of done for a change

1. `gofmt -l .` reports nothing changed.
2. `task test` (or `go test ./...`) passes.
3. New commands/flags have `Short`/`Long` help text and are covered by an integration
   test in `tests/integration_test.go`.
4. `README.md`'s command list / project structure section updated if the change adds,
   removes, or renames a command or top-level directory.
