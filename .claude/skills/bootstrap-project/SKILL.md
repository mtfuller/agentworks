---
name: bootstrap-project
description: >
  Turn this starterpack-go-cli template into the start of a new, real Go CLI project:
  rename the module path and binary, replace the example commands, and update every
  file that hardcodes the template's own name. Use this once, at the start of a new
  project built from this template — not for ordinary feature work afterward.
---

# Bootstrap a new project from this template

## Outcome

The repo builds and runs under the new module path / binary name with no leftover
references to `starterpack-go-cli` or `mtfuller`, and the example commands (`greet`,
`calc`, `process`) are either replaced with real commands or removed.

## Before you start

Ask the user (if not already given) for:
- The new **module path** (e.g. `github.com/acme/widget-cli`) — this becomes every
  import path in the repo, so get it right before editing.
- The new **binary name** (often the last path segment, but not always).
- The new **one-line description** for the root command's `Short` text.
- Whether to keep the example commands as a reference or delete them outright — default
  to deleting `greet`/`calc`/`process` and `pkg/example` once real commands exist,
  since they exist only to demonstrate the template's patterns.

## Steps

1. **Rename the module.** Edit `go.mod`'s `module` line, then run
   `go build ./...` once as a first-pass compile check — the compiler will point at
   every remaining hardcoded import.
2. **Rewrite every import path.** Every `.go` file that imports this module's own
   packages does so via the full path (e.g.
   `github.com/mtfuller/starterpack-go-cli/internal/color`). Find them all:
   `grep -rl "mtfuller/starterpack-go-cli" --include="*.go" .`
   and replace the old module path with the new one throughout — `cmd/*.go`,
   `pkg/example/example.go`, `tests/integration_test.go`, `main.go`.
3. **Rename the binary.** `Taskfile.yml`'s `vars.BINARY_NAME` and the `LDFLAGS`
   version-package path (`github.com/mtfuller/starterpack-go-cli/internal/version...`)
   both need updating.
4. **Update the root command.** `cmd/root.go`: `rootCmd.Use` (the invoked binary name),
   `Short`, and `Long` (currently references "starterpack-go-cli" by name and lists the
   template's own feature bullets — replace with the new project's description).
5. **Decide the fate of the example commands** (`cmd/greet.go`, `cmd/calc.go`,
   `cmd/process.go`, `pkg/example/`) per the user's answer above. If deleting, also
   remove their entries from `tests/integration_test.go` and the README's command list.
6. **Update project-identity files**: `README.md` (title, clone URL, description,
   command list), `LICENSE` (copyright holder/year, if the template's is a placeholder),
   `.github/copilot-instructions.md` and `.github/instructions/*.instructions.md`
   (project summary section — the *conventions* sections underneath describe patterns
   the new project should generally keep), and this repo's `AGENTS.md` (project
   description at the top; conventions/workflow sections stay valid as-is).
7. **Verify.** `grep -rl "starterpack-go-cli\|mtfuller" --include="*.go" --include="*.yml" --include="*.md" .`
   should return nothing outside of things deliberately left as historical references
   (e.g. a changelog entry) — confirm the search is actually empty before continuing.
8. `gofmt -l .`, `task build`, `task test` — confirm the renamed project builds, and
   the (possibly trimmed) test suite passes.
9. Commit as the project's initial real commit, distinct from the template's own
   history if the user wants a clean start (ask before rewriting history).
