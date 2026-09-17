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
real exporters for all four registered targets) is implemented; see Architecture below
for what's real today versus deliberately deferred.

## Guiding scenarios

These are the user scenarios the design should stay accountable to (see the project
brief for full detail — ask the user if it's not in context). Treat them as the source
of truth for what "done" looks like for any given piece of functionality:

1. **Data analyst** — a skill (Python script + tests + sample CSVs) validated locally by
   running its test scripts, then exported as both a ChatGPT skill upload and a
   Microsoft 365 Copilot zip, versioned per export. *(Fully works end to end, see
   `examples/starter-project/skills/csv-analyzer` — `agentworks export ... --target
   chatgpt` and `--target m365-copilot` are both real.)*
2. **Developer** — a tool that fetches/parses a Jira ticket (real API + auth, with a
   simulated API for tests), validated locally, then exported to both Claude Code and
   GitHub Copilot. *(Done end to end — see `examples/starter-project/tools/jira-fetch`:
   a real MCP server (`pip install mcp`, one `fetch_issue` tool) fetching a Jira Cloud
   issue via its REST API v2, with `tests/test_main.py` simulating that API rather than
   calling a real instance. Verified for real (not just researched) against the actual
   `mcp` package before landing: importing it, listing its registered tool, invoking it
   through the SDK's own call path, and starting/stopping the stdio server cleanly.)*
3. **AI researcher** — a domain-specific agent with its own guidance markdown and
   resources, exported to ChatGPT, Claude, and others as drag-and-drop artifacts.
   *(`claude-code`, `github-copilot`, and `m365-copilot` all export a standalone agent
   now (a subagent-file plugin for the first two, a declarative agent for the third) --
   `agentworks export agents/x --target claude-code`. `chatgpt` is a deliberate, closed
   gap, not an open one -- see "ChatGPT: why skills only" below.)*
4. **Engineering leader** — multiple agents/tools/MCP servers composed into pipelines
   ("software factory" workflows), targeted at Claude Code and GitHub Copilot, exported
   as one or more plugins. *(Done, deliberately without an execution engine -- see
   `internal/targets/workflowsteps` and each vendor's `workflow.go`: a workflow exports
   to a real plugin bundling its referenced agents as subagent files, its referenced
   tools as MCP server entries, and a generated orchestrator command; the vendor's own
   agent loop does the executing, AgentWorks just builds the package. `agentworks
   validate` also now checks a workflow's `steps:` resolve to real artifacts.)*
5. **Any user** — guided boilerplate generation for a new tool/agent/workflow, including
   generated sample artifacts to learn the system from. *(`agentworks new`'s interactive
   wizard + `examples/starter-project`.)*
6. **Any user** — a project layout that's navigable in a plain text editor, with local
   test/validate/simulate workflows and room to build genuinely custom, heavier tooling
   when a scenario needs it. *(The `<kind>.md` format + `validate`/`test` commands.)*
7. **Any user** — a polished TUI for building agents/tools/skills, with control over
   whether an export is a standalone skill zip or a full plugin. *(Done — `agentworks
   tui` now drives both from inside the browser: `n` opens the same `huh` wizard
   `agentworks new` uses (embedded as a child Bubble Tea model, not a second program),
   landing on the freshly-created artifact; `e` opens an export form restricted to
   targets that actually support the artifact's kind, with a zip toggle, reusing the
   exact same `Exporter` call `agentworks export` makes. See `internal/tui/actions.go`.)*
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
  Vendor-specific exporters live in their own subpackage and self-register via `init()`
  + `targets.Register`; `cmd/export.go` imports each implemented one (blank import
  unless it also needs to reference the package directly, like `m365copilot.TargetID`
  for the post-export placeholder-data warning). Two format-specific packages are
  shared across vendor packages rather than duplicated:
  - `internal/targets/agentskills` writes the spec-compliant
    ([agentskills.io](https://agentskills.io/specification)) `SKILL.md` shape that
    `claudecode`, `chatgpt`, and `githubcopilot` all build on for skills — extend *that*
    package for a skill-format change, not each vendor package individually.
  - `internal/targets/mcpconfig` builds the `{"mcpServers": {...}}` entry that
    `claudecode` and `githubcopilot` both build on for tools (a tool's `command`
    frontmatter run via `sh -c`, its `auth` list passed through as unresolved `${VAR}`
    references) — extend *that* package for an MCP-registration change.
  - `internal/targets/filecopy` is lower-level still: copy-an-artifact's-files-excluding-
    its-manifest and zip-a-directory, used by both of the above and directly by the
    `claudecode`/`githubcopilot` tool export paths (which don't go through
    `agentskills`, since a tool isn't a skill).
  - `internal/targets/workflowsteps` parses a workflow's `steps:` field and loads each
    referenced agent/tool from the project (root found via `project.FindRoot(a.Dir)`,
    no `Exporter` interface change needed) — both `claudecode` and `githubcopilot`'s
    `workflow.go` call `Resolve` and then write one subagent file per agent step
    (vendor-specific frontmatter/paths — `agents/<name>.md` vs.
    `com.github.copilot/agents/<name>.agent.md` — so that part isn't shared) plus one
    `mcpconfig.ServerFor` entry per tool step. `cmd/validate.go` also calls `Resolve` to
    catch a workflow pointing at an artifact that doesn't exist.

  `m365copilot` is genuinely different from all of this (a declarative agent + Teams app
  package, not a skill directory or an MCP registration) and doesn't use any of these
  shared packages. Hook export (`claudecode`/`hook.go`, `githubcopilot`/`hook.go`)
  is deliberately *not* a third shared package like `mcpconfig`: the two vendors'
  `hooks.json` shapes genuinely differ (Claude Code nests an extra matcher array per
  event and uses a `command` field; Copilot is flatter and uses `bash`), so forcing a
  shared struct would fight the schema difference rather than reflect it.

  Internally, `claudecode` and `githubcopilot` are each split one file per kind
  (`export.go`'s `Export` just dispatches by `a.Kind`) plus a `plugin.go` holding what's
  common to every plugin they produce (the manifest writer, `writeMCPFile`) — `agent.go`
  and `workflow.go` both call the same `write*AgentFile` helper so a subagent file looks
  identical whether it's exported standalone or bundled into a workflow.
- **`internal/tui`** — the Bubble Tea project browser (`model.go`/`app.go`), the
  create/export actions it drives (`actions.go`, `export_form.go`), and the `huh`-based
  create-artifact wizard (`wizard.go`) shared with `cmd/new.go`'s non-interactive-args
  fallback. `huh.Form` implements `tea.Model` itself, so a form is embedded as a child
  model (`Model.activeForm`) rather than run via its own blocking `.Run()` inside the
  browser — see `actions.go`'s `updateForm`/`finishForm` for the pane that hands control
  to/from it (`paneForm`), keyed off the form's own `State` field
  (`StateCompleted`/`StateAborted`).
- **`internal/{logger,color,spinner,version}`** — CLI-support code inherited from the
  starter template (leveled logging, ANSI output helpers, a progress spinner, build
  metadata via ldflags).
- **`pkg/`** — currently unused. Reserved for logic that should be importable by other
  Go programs, not just this CLI; nothing has needed that yet.
- **`tests/`** — black-box integration tests that exec the built CLI. Unit tests live
  next to their package (`internal/artifact/artifact_test.go`, etc.), not here.

### What's real vs. deferred

Implemented: the project/artifact model, scaffolding for all 5 kinds, project-wide
discovery/test-running, the vendor capability matrix, and real exporters for all four
registered targets. `validate` checks each kind's export-readiness, not just the four
generic fields `Artifact.Validate()` covers — a workflow's `steps:` resolve to real
artifacts, a hook's `events`/`command` are set together, a tool with `auth` also has a
`command` (see `cmd/validate.go`'s `validateKindSpecific`). `claude-code`
and `github-copilot` each export *all five* artifact kinds now — skills via the shared
`agentskills` writer, tools via the shared `mcpconfig` MCP-registration builder, agents
as a subagent-file plugin, hooks as a lifecycle-event plugin, and workflows as a bundled
plugin composing the others via `workflowsteps`. `chatgpt` exports skills only, by
deliberate, researched decision (see "ChatGPT: why skills only" below), not because
nobody's gotten to it. `m365-copilot` exports skills and agents as a declarative-agent
app package. Also implemented: the Bubble Tea browser + `new` wizard.

Deliberately deferred (do this later, not by accident while doing something else):
exporting `hook`/`tool`/`workflow` on `m365-copilot` (the registry's capability matrix
already says which vendor could take a kind in principle — the exporter is the gap, not
the model); any workflow *execution* engine — a workflow export produces a real plugin
the vendor's own agent loop runs, AgentWorks never executes a workflow itself, and
that's permanent, not a gap; wiring a `test` action into the TUI (`n`/`e` —
create/export — are wired now; running an artifact's `test:` command from inside the
browser isn't yet); `m365-copilot`'s placeholder developer/privacy/terms URLs becoming
real project-level config in `agentworks.yaml` instead of TODO strings a human has to
find and edit.

`chatgpt` staying skill-only is different from the above: it's a *closed* investigation,
not an open TODO — see the next section for why, so nobody re-opens it without first
re-reading why it was closed.

### ChatGPT: why skills only

Researched (Sept 2026) whether `chatgpt` could export agents/tools/workflows the same
way `claude-code`/`github-copilot` do. Conclusion: there's currently no stable,
file-based target to export *to*, for reasons specific to each kind rather than "not
researched yet":

- **Agent.** Custom GPTs — the direct analog to a Claude Code subagent or an M365
  declarative agent — are being actively retired as of this research (OpenAI announced
  retirement on 2026-09-11; new GPT creation ends 2026-09-25; full retirement
  2026-12-11). Their replacement, Workspace Agents, is a cloud-hosted, always-on,
  enterprise-workspace product with no local file format at all — there's nothing an
  `Exporter` could produce a droppable artifact *for*, even in principle.
- **Tool.** ChatGPT's real external-tool mechanism, the Apps SDK, genuinely is
  MCP-based (same protocol `internal/targets/mcpconfig` already targets) — but
  registration is a hosted, publicly-reachable server URL configured through ChatGPT's
  UI/Developer Mode, not a local stdio process launched from a generated config file the
  way `.mcp.json` works. AgentWorks' `Exporter` interface
  (`Export(a, outDir, opts) (string, error)`, producing a local file/directory) doesn't
  fit "you need an already-deployed, running server, then register its URL by hand."
- **Workflow.** No orchestration/pipeline concept exists beyond a single GPT's/App's own
  instructions.

If this changes — a stable, documented, file-based ChatGPT format emerges — re-open the
investigation then; don't assume today's reasoning still holds without checking, this
area was unusually volatile even within the week it was researched.

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
