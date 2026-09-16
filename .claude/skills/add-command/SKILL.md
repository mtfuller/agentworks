---
name: add-command
description: >
  Scaffold a new Cobra subcommand in a starterpack-go-cli project (flags, help text,
  unit + integration tests, registration). Use when asked to add a CLI command,
  subcommand, or "a new `<tool> <verb>`" entry point.
---

# Add a Cobra command

## Outcome

`./<binary> <name> --help` shows the new command; it's wired into `rootCmd` and
covered by an integration test.

## Contract

- One file per command: `cmd/<name>.go`, package `cmd`.
- `var <name>Cmd = &cobra.Command{...}` with `Use`, `Short`, and `Long` all set —
  `Short` is a one-line summary (shown in `--help` listings), `Long` can be multi-line.
- Prefer `RunE func(cmd *cobra.Command, args []string) error` over `Run` — return
  wrapped errors (`fmt.Errorf("...: %w", err)`) instead of calling `os.Exit`; Cobra's
  top-level `Execute()` in `cmd/root.go` already formats the error via `color.Error`
  and exits 1.
- Validate positional args with a Cobra `Args` validator (`cobra.ExactArgs(1)`,
  `cobra.MaximumNArgs(1)`, `cobra.RangeArgs(min, max)`, ...) rather than hand-rolled
  `len(args)` checks.
- Command-specific flags: `<name>Cmd.Flags().<Type>VarP(&var, "flag", "f", default,
  "help text")` inside `init()`. Flags shared across every command belong on
  `rootCmd.PersistentFlags()` in `cmd/root.go`, not duplicated per command.
- User-facing output via `internal/color` (`color.Success`/`Error`/`Warning`/`Info`),
  not raw `fmt.Println`. Debug/trace detail via `internal/logger`
  (`logger.Debug`/`Info`/`Warn`/`Error`), which already respects the global
  `--verbose`/`--log-level` flags.
- Business logic that doesn't need Cobra/flags/terminal output goes in `pkg/` (or
  `internal/` if it's CLI-specific plumbing, not a reusable library) and gets called
  from `RunE` — keep the command file itself thin.
- Register with `rootCmd.AddCommand(<name>Cmd)` inside the same file's `init()`.

## Steps

1. Create `cmd/<name>.go` following the pattern above. `cmd/greet.go` is the simplest
   worked example (one required arg, one flag, calls into `pkg/example`); `cmd/calc.go`
   shows an enum-style flag (`--operation`) with validation; `cmd/process.go` shows the
   spinner pattern for a long-running operation.
2. If the command needs new business logic, add it to `pkg/<name>/` (reusable outside
   this CLI) or `internal/<name>/` (CLI-specific) — see the `add-package` skill — rather
   than inlining it in `cmd/<name>.go`.
3. Add a unit test only if the command file itself has non-trivial logic beyond
   wiring (flag parsing helpers, output formatting). Most command-level testing
   belongs in the integration suite instead.
4. Add integration test cases to `tests/integration_test.go`: run the built/`go run`
   CLI via `os/exec`, capture stdout/stderr, assert exit code and expected output
   substrings — for both the success path and at least one failure path (missing
   arg, invalid flag value). Follow the existing table-driven style in that file.
5. `gofmt -l cmd/<name>.go` and `task test` (or `go test ./...`) before calling it done.
6. Update `README.md`'s command list and "Available Commands" examples if this is a
   user-visible command (skip for internal/hidden commands).
