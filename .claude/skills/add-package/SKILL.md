---
name: add-package
description: >
  Scaffold a new internal/ or pkg/ package (with unit tests) in a starterpack-go-cli
  project. Use when adding CLI-specific support code (a new internal/ helper) or a
  reusable library others could import (a new pkg/ package).
---

# Add an internal/ or pkg/ package

## Outcome

A new `internal/<name>/` or `pkg/<name>/` directory with a package file, a
`_test.go` file covering it, and (if it's meant to be used from `cmd/`) at least one
call site wired in.

## Choosing internal/ vs pkg/

- **`internal/`** — code that only makes sense inside this CLI: talks to Cobra types,
  formats terminal output, reads global flags, etc. Go's `internal/` mechanism also
  enforces this isn't importable from outside the module, which is a feature here, not
  a limitation.
- **`pkg/`** — code with no CLI dependency that a *different* program could import
  (business logic, data transforms, a client for some API). If you're unsure, ask: "if
  I deleted `cmd/` entirely, would this code still make sense?" — yes → `pkg/`.

## Contract

- Directory: `internal/<name>/<name>.go` or `pkg/<name>/<name>.go`, package `<name>`
  (lowercase, single word, matching the directory).
- File layout: package → imports (stdlib, then external, then this module's own
  packages) → constants → types → `init()` (rare) → constructors → methods → helpers.
- Exported identifiers get a doc comment starting with the identifier's name (standard
  Go convention — `go vet`/`golint` expect this).
- Errors: return `error`, wrap with `fmt.Errorf("<context>: %w", err)` — don't
  `log.Fatal`/`os.Exit` from library code; that's the caller's decision.
- No global mutable state beyond what `internal/logger` and `internal/color` already
  establish as this project's pattern (a package-level logger/config instance is fine
  if the package needs one, following their example).

## Steps

1. Create `<internal|pkg>/<name>/<name>.go` with the package and its exported API.
2. Create `<internal|pkg>/<name>/<name>_test.go` in the same package (white-box access)
   using table-driven tests and `testify/assert` for assertions, matching the style in
   `internal/logger/logger_test.go` or `pkg/example/example_test.go`.
   - Cover success and error paths.
   - Mock/avoid real filesystem or network access — inject dependencies or use
     `bytes.Buffer`/temp dirs with `t.Cleanup()`, as `internal/logger`'s tests do for
     captured output.
   - Aim for >80% coverage of the new package (`task coverage`).
3. If this package is meant to be used by a command, wire the call site into the
   relevant `cmd/<name>.go` (see the `add-command` skill if the command doesn't exist
   yet) and add/extend an integration test in `tests/integration_test.go` that
   exercises it end-to-end through the CLI.
4. `gofmt -l <internal|pkg>/<name>/` and `task test` before calling it done.
