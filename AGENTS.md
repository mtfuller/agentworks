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
   tui` now drives all three from inside the browser: `n` opens the same `huh` wizard
   `agentworks new` uses (embedded as a child Bubble Tea model, not a second program),
   landing on the freshly-created artifact; `e` opens an export form restricted to
   targets that actually support the artifact's kind, with a zip toggle, reusing the
   exact same `Exporter` call `agentworks export` makes; `t` runs the artifact's
   declared `test:` command via `tea.ExecProcess` (suspends the alt-screen, hands the
   real terminal to the child process, resumes after). See `internal/tui/actions.go`
   and `internal/tui/test_action.go`.)*
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
  `eval`, `doctor`, `run`, `targets`, `export`, `tui`, `version`). Commands parse flags/args, call into the
  packages below, and format output. Keep business logic out of `Run`/`RunE` — a
  command file should read as "gather input, call one function, print the result."
- **`internal/artifact`** — the vendor-agnostic artifact model: `Kind`
  (agent/skill/tool/hook/workflow), `Frontmatter` (common fields explicit, kind-specific
  fields round-trip through `Extra` so there's one parser for all five kinds), and
  `Parse`/`Render`/`Load`/`Save` for the `<kind>.md` (YAML frontmatter + Markdown body)
  file format. This is the one place that understands that file format — nothing else
  should hand-parse it. `lint.go`'s `LintDescription`/`LintOverlap` are heuristic
  description-quality checks (not structural validation, which stays in `Validate()`)
  used by `cmd/validate.go`.
- **`internal/project`** — `agentworks.yaml` (the project manifest), `Init` (scaffold a
  new project), `FindRoot` (walk upward for the manifest, like git finds `.git`), and
  `Discover` (walk the 5 kind directories and load every artifact). `Manifest.Publisher`
  is optional project-level publishing identity (name/website/privacy/terms/accent
  color) — currently only `internal/targets/m365copilot` reads it, for its app
  manifest's developer block; there's no CLI flag for it, a project sets it by hand-
  editing a `publisher:` block into `agentworks.yaml`.
- **`internal/scaffold`** — `New(root, kind, name, opts)` writes a new artifact's
  directory + starter files. This is the single code path both `cmd/new.go` and the TUI
  wizard call — never generate an artifact's files by hand in either front end.
- **`internal/evalspec`** — the `evals/*.yaml` case file format (`Case`/`Assertions`,
  `LoadDir`) and `Evaluate`, a pure function checking a runner's output string against
  a case's deterministic assertions (`contains`/`not_contains`/`matches`/`not_matches`/
  `max_length`/`min_length`). It doesn't run anything — `cmd/eval.go` is what pipes a
  case's `prompt` to an artifact's declared `eval_runner` command (or the project's
  `agentworks.yaml` `eval.default_runner`) and hands the runner's stdout back to
  `Evaluate`. Same "AgentWorks never executes anything itself" principle already
  applied to workflow export, extended to evals: `eval_runner` is the project's own
  shell command, not a vendor SDK call AgentWorks makes on its behalf.
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
  - `internal/targets/agentcaps` is AgentWorks' vendor-agnostic vocabulary for what an
    agent may do (`tools:`, a closed set like `read-files`/`web-search`) and how capable
    a model it needs (`model:`, a tier: `fast`/`balanced`/`powerful`), plus the mapping
    functions that turn them into each target's real shape: `ForClaudeCode` builds
    Claude Code's confirmed `tools:`/`model:` subagent frontmatter (see
    https://code.claude.com/docs/en/sub-agents), `ForM365Capabilities` builds Microsoft
    365's confirmed declarative-agent `capabilities` array entries (`WebSearch`,
    `CodeInterpreter`). Both `claudecode/agent.go` and `githubcopilot/agent.go`'s
    `writeClaudeAgentFile`/`writeCopilotAgentFile` are the single source of truth their
    respective standalone-agent and workflow-bundled-agent exporters both call, so
    enriching those two functions enriches both export paths at once. There's
    deliberately no `ForGitHubCopilot` — see "What's real vs. deferred" below.

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

  `cursor` and `geminicli` follow the same one-file-per-kind `export.go` dispatch shape,
  but neither has a `plugin.go` — Cursor's and Gemini CLI's real formats are loose,
  project-scoped files/directories, not an installable plugin, so there's no shared
  manifest writer to factor out (Gemini CLI's `manifest.go` is its own thing, a
  `gemini-extension.json` writer, not a plugin manifest). Both still reuse
  `mcpconfig.ServerFor` for tool export and `filecopy.CopyArtifactFiles` for a skill's
  supporting files — see "Cursor and Gemini CLI: what's real vs. deferred" below for the
  full per-kind mapping and researched reasoning.
- **`internal/tui`** — the Bubble Tea project browser (`model.go`/`app.go`), the
  create/export actions it drives (`actions.go`, `export_form.go`), the test action
  (`test_action.go`), and the `huh`-based create-artifact wizard (`wizard.go`) shared
  with `cmd/new.go`'s non-interactive-args fallback. `huh.Form` implements `tea.Model`
  itself, so a form is embedded as a child model (`Model.activeForm`) rather than run
  via its own blocking `.Run()` inside the browser — see `actions.go`'s
  `updateForm`/`finishForm` for the pane that hands control to/from it (`paneForm`),
  keyed off the form's own `State` field (`StateCompleted`/`StateAborted`). The test
  action doesn't use that machinery: it hands a plain `*exec.Cmd` to `tea.ExecProcess`,
  which is bubbletea's own mechanism for suspending the alt-screen and handing the real
  terminal to a child process — no `paneForm` involved, control returns via a
  `testFinishedMsg` handled in `Model.Update`.
- **`internal/mcpclient`** — a minimal MCP (Model Context Protocol) client over the
  stdio JSON-RPC transport: `StartProcess` runs a tool artifact's declared `command`
  (via `sh -c`, the same convention `mcpconfig.ServerFor`/`cmd/test.go` use) as a real
  child process and wires up a `Client` speaking `initialize`/`notifications/
  initialized`, `tools/list` (with cursor pagination), and `tools/call` over it.
  `Client.OnTrace`/`OnStderr` hooks expose every raw JSON-RPC line and the server's
  stderr for `internal/inspector`'s log pane; `client_test.go` is fully hermetic
  (an `io.Pipe`-backed fake server, no real subprocess). Deliberately out of scope:
  the rest of the MCP spec (resources, prompts, sampling, roots) — `agentworks run`
  inspects tools, not a general-purpose MCP client.
- **`internal/inspector`** — the Bubble Tea UI behind `agentworks run`: connects to a
  tool artifact's MCP server (via `internal/mcpclient`) and drives a full-screen
  browse/call/inspect loop (`model.go`/`app.go`) — a filterable tool list and a detail/
  result viewport side by side, a `huh.Form` generated per-tool from its `inputSchema`
  (`schemaform.go` — string/enum/integer/number/boolean/array/object each get an
  appropriate field, embedded as a child model the same way `internal/tui/actions.go`
  does for create/export), a call-history pane, and a raw JSON-RPC/stderr traffic log
  (`logpane.go`/`history.go`) — the actual debugging payoff over a plain REPL, reachable
  even from the connection-failure screen so the real cause (e.g. a missing Python
  package) isn't hidden behind a generic protocol error. A separate package from
  `internal/tui` rather than another pane in its browser: this is a different
  lifecycle (connect once, then browse/call/inspect until quit), not another step in
  its tabbed kind → detail flow.
- **`internal/{logger,color,spinner,version}`** — CLI-support code inherited from the
  starter template (leveled logging, ANSI output helpers, a progress spinner, build
  metadata via ldflags).
- **`pkg/`** — currently unused. Reserved for logic that should be importable by other
  Go programs, not just this CLI; nothing has needed that yet.
- **`tests/`** — black-box integration tests that exec the built CLI. Unit tests live
  next to their package (`internal/artifact/artifact_test.go`, etc.), not here.

### What's real vs. deferred

Implemented: the project/artifact model, scaffolding for all 5 kinds, project-wide
discovery/test-running, the vendor capability matrix, and real exporters for all six
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
app package. `cursor` and `gemini-cli` each export skill/agent/tool/hook as loose,
project-scoped files rather than a plugin (see "Cursor and Gemini CLI: what's real vs.
deferred" below) — workflow is unsupported on both, same as `chatgpt`/`m365-copilot`.
Also implemented: the Bubble Tea browser + `new` wizard.

Deliberately deferred (do this later, not by accident while doing something else):
exporting `hook`/`tool`/`workflow` on `m365-copilot` (the registry's capability matrix
already says which vendor could take a kind in principle — the exporter is the gap, not
the model); any workflow *execution* engine — a workflow export produces a real plugin
the vendor's own agent loop runs, AgentWorks never executes a workflow itself, and
that's permanent, not a gap.

As of this pass, both smaller items that used to be listed here are done: the TUI's `t`
key runs an artifact's `test:` command (see `internal/tui/test_action.go` — `n`/`e`/`t`
now cover create/export/test from inside the browser); and `m365-copilot`'s developer/
privacy/terms URLs come from an optional `publisher:` block in `agentworks.yaml`
(`internal/project.Manifest.Publisher`, read via `internal/targets/m365copilot/
publisher.go`'s `publisherFor`) when a project sets one, falling back to the old
clearly-labeled placeholders field-by-field otherwise. `cmd/export.go`'s post-export
warning only fires when `m365copilot.UsesPlaceholderPublisher` says a required field
(name, privacy URL, or terms URL — accent color is cosmetic, not warned about) is still
a placeholder, so a project that's configured it stops seeing the nag.

Also done as of this pass: `agentworks init` now writes an `AGENTS.md` plus
`.agents/skills/agentworks-cli/SKILL.md` into every new project (see
`internal/project/agentdocs.go`), so a coding agent working inside a scaffolded project
knows to drive it via the CLI rather than hand-writing `<kind>.md` files or hand-editing
exported vendor output. Neither file is overwritten if a project already has one
(`writeAgentDocs`/`writeIfAbsent`), so re-running init-adjacent tooling never clobbers
hand edits.

Also done as of this pass: the two items from [BRAINSTORM.md](BRAINSTORM.md)'s
Distribution section. `agentworks export` now handles more than one artifact per call —
`--all`/`--kind` export every matching artifact in the project individually (skipping,
with a warning, any kind the target can't consume, rather than failing the whole run —
see `cmd/export.go`'s `runBulkExport`); passing several paths, or one path with
`--bundle <name>`, packages them into a single plugin instead (`targets.BundleExporter`,
an optional capability only `claude-code`/`github-copilot` implement, since theirs are
the two plugin formats actually meant to bundle several components together — see
`cmd/export.go`'s `runBundleExport` and each target's `bundle.go`). A bundle's tool
members are namespaced under `tools/<name>/` in the merged output (their own `command`
wrapped with `cd tools/<name> && ...` via `mcpconfig.ServerForDir`) so more than one
tool's `src/` doesn't collide; hook members merge into one `hooks.json`, appending
matchers per event rather than overwriting when two hooks share an event. A workflow
can't be a bundle member — it already has its own bundling-shaped export (ordered steps
+ orchestrator command), so nesting one inside a bundle is a clear error instead.

Also done as of this pass: BRAINSTORM.md's Ecosystem item, "a skill/tool registry —
pull, not just push." `agentworks add <url>` (`internal/importer`) is the reverse of
`export` — it fetches a skill or Claude Code plugin (GitHub repo/tarball or a direct
archive URL, no local `git` binary or `api.github.com` rate limits involved — see
`fetch.go`'s use of `codeload.github.com`) and decomposes it back into local artifacts.
A bare Agent Skill maps 1:1 (`agentskills.Read`, the mirror of `agentskills.Write`); a
Claude Code plugin decomposes its `skills/*/SKILL.md` and `agents/*.md` the same way
(`claudecode.ReadAgentFile` mirrors `writeClaudeAgentFile`). Everything is resolved into
an `importer.Plan` before anything is written (`Prepare`/`Apply`), so a collision on
artifact 4 of 5 never leaves the first 3 written with no way back. `internal/marketplace`
searches agentskills.codes's real public API plus the well-known Claude Code
(`anthropics/claude-plugins-official`) and GitHub Copilot (`github/awesome-copilot`)
`marketplace.json` catalogs, surfaced through `agentworks tui`'s `a` key (or
`agentworks add` with no argument, in an interactive terminal).

Deliberately deferred here, for the same "genuinely not feasible yet, not overlooked"
reason as the chatgpt case below: importing **tools** (`.mcp.json`) and **hooks**
(`hooks/hooks.json`) *from inside a fetched plugin*. Collapsing a merged hooks.json or
mcp.json back into "the original N artifacts" is inherently lossy/ambiguous — the same
many-to-one problem `internal/targets/claudecode/bundle.go`'s export side has, in
reverse — unlike skills and agents, which are a clean one-file-per-artifact mapping.
Reported per plugin as "not supported yet" (`Plan.Unsupported`), not silently dropped
and not a hard failure. Also deferred: decomposing GitHub Copilot's own plugin layout
(`com.github.copilot/`) — skills already reach both vendors via the shared SKILL.md
path, so this isn't a large gap; and non-GitHub git hosts (gitlab, bitbucket), npm- and
command-sourced marketplace entries, and a `--force`/overwrite flag for re-importing
over an existing artifact (a name collision is a hard error, matching `scaffold.New`'s
existing behavior).

Also done as of this pass: BRAINSTORM.md's "Starter templates" item. A `Template`
(`internal/scaffold/templates.go`) is just a named, curated `kindSpec` — the exact same
shape `specs` already uses for each kind's generic default — selected instead of it via
`scaffold.Options.Template`, so `scaffold.New` stays the one code path `agentworks new`
and the TUI both go through. Fifteen built-in templates ship (two per kind, except
agent/tool (three) and skill (five) -- e.g. a tool's `api-wrapper`/`cli-wrapper`, a
skill's `pptx-style-refresh`/`xlsx-workbook-updater` for Microsoft 365 Copilot's
PowerPoint/Excel skills), discoverable via `agentworks templates [kind]` (a static
table, no interactivity — mirrors `agentworks targets`) and `agentworks new
--from-template <id>`, or interactively via `agentworks tui`'s `b` key, which opens a
browse/search pane (`internal/tui/templates.go`) and, on enter, pre-fills the *same*
create form manual creation uses (`newArtifactForm`/`commitCreate`) rather than a
separate form path. Unlike the marketplace pane, template data is local and static, so
there's no fetch, no async `tea.Cmd`, and no spinner — the list is simply populated once
in `Model.New()`. The two workflow templates deliberately leave `steps:` empty rather
than prefilling placeholder agent/tool names that don't exist yet in a fresh project —
matching the plain default workflow scaffold's own approach of documenting the pattern
in prose instead of live frontmatter, so a template-scaffolded workflow never fails
`agentworks validate` out of the box. Deliberately deferred: project-defined custom
templates (a `templates/` directory the project itself contributes) — built-in only for
now, confirmed with the user rather than assumed.

Also done as of this pass: description-quality linting. `agentworks validate` now also
runs `Artifact.LintDescription()` and (project-wide only) `artifact.LintOverlap()` from
`internal/artifact/lint.go` — heuristic warnings, not the structural errors
`Validate()`/`validateKindSpecific` produce, so they never fail the command by default.
They catch a description that's over the
[Agent Skills spec](https://agentskills.io/specification)'s 1024-character limit, under
20 characters (the spec's own "poor example" shape, e.g. "Helps with PDFs."), redundant
with the artifact's name (its significant words are a subset of the name's), or
overlapping heavily (≥70% Jaccard similarity of significant words) with another
same-kind artifact's description in the project. `cmd/validate.go`'s new `--strict` flag
promotes these warnings to failures for a CI gate that wants them enforced. Serves
guiding scenario 6 (local validate workflows) directly — a description is how an agent
*finds* an artifact in the first place, and that was previously untested by anything in
AgentWorks. `agentworks init`'s generated `AGENTS.md`/`agentworks-cli` `SKILL.md`
(`internal/project/agentdocs.go`) mention it too, so a coding agent working inside a
scaffolded project knows to heed the warnings.

Also done as of this pass: agents are no longer just a name + prompt. An agent
artifact can set `tools:`/`model:` frontmatter in `internal/targets/agentcaps`'s
vocabulary; `agentworks validate` rejects an unrecognized value in either
(`cmd/validate.go`'s `validateKindSpecific`, `KindAgent` case) rather than silently
exporting nothing for it. `claude-code` maps them to that vendor's confirmed
`tools:`/`model:` subagent fields; `m365-copilot` maps `tools:` to that vendor's
confirmed declarative-agent `capabilities` array (only `web-search`/`code-execution`
have a real M365 equivalent — declarative agents have no local filesystem/shell
concept, so the other three tools intentionally map to nothing there, not a bug).
`github-copilot`'s mapping is still deferred, same treatment as the ChatGPT decision
below: its custom-agent frontmatter spec doesn't publicly confirm tools/model fields
exist, so AgentWorks doesn't guess at one (see `internal/targets/githubcopilot/
agent.go`'s comment). Re-open once a confirmed spec exists.

Also done as of this pass: a lightweight behavior-eval mechanism, `agentworks eval`
(see `internal/evalspec`, `cmd/eval.go`). A skill or agent can have an `evals/`
directory of YAML case files (`prompt` + deterministic `assert` rules); scaffolding a
new agent or skill now creates a starter one automatically. `agentworks eval` pipes
each case's prompt to the artifact's declared `eval_runner` frontmatter command (or
the project's `agentworks.yaml` `eval.default_runner`) and checks the runner's stdout
— AgentWorks itself never calls a model, extending the same principle already applied
to workflow export (the vendor's own agent loop executes; AgentWorks only
orchestrates) to behavior testing. `agentworks validate` also parses any `evals/`
directory it finds (regardless of kind) so a malformed case file fails at validate
time, not only when `eval` runs it. Deliberately out of scope for this pass, not
overlooked: an LLM-graded rubric assertion type (a case's response graded by a second
model call rather than a deterministic check) and assertions on an agent's actual
tool-call trace rather than just its final text output — both would need a runner
protocol richer than "a prompt in, text out," which is a real design question, not
just unfinished work.

Also done as of this pass: two Node.js scaffold templates, `node-skill` and `node-tool`
(`internal/scaffold/templates.go`), for artifacts whose logic outgrows a quick script
and is more naturally written in JavaScript (or needs an npm package) than Python.
Each is a normal standalone `package.json` (`"type": "module"`, a `start` script, a
`test` script wired to Node's built-in test runner) plus a throwing placeholder
entrypoint and test, generated by three small helpers
(`nodePackageJSON`/`nodeEntrypointPlaceholder`/`nodeTestPlaceholder`) shared between the
two templates. Serves guiding scenario 5 (guided boilerplate) and scenario 6 (room to
build genuinely custom, heavier tooling when a scenario needs it) directly. Deliberately
*not* built: any cross-artifact build orchestration
(a monorepo tool, a root workspace, a "build all" command) — every artifact is already
a self-contained, independently-exportable leaf directory with its own declared
`command:`/`test:` string that `cmd/test.go`/export shell out to generically regardless
of language, exactly like the existing Python skills/tools, so there's no shared
dependency graph across artifacts to orchestrate. Each Node artifact gets its own
`node_modules` on `npm install` inside its own directory; that's the accepted cost of
keeping artifacts portable/copyable rather than wiring them into a shared workspace
root, matching how the Python templates never introduced a shared virtualenv either.

Also done as of this pass: a generic `build:` frontmatter field and `agentworks build`
(`cmd/build.go`), a straight mirror of `cmd/test.go` -- shells out to a declared
`build:` command from the artifact's own directory, same "no path runs every artifact
that declares one" behavior, added to `doctor`'s `shellCommandFields` so a bad
interpreter is caught before the command actually runs. Exists for the same reason
Node artifacts need `npm install`/a compile step before they're runnable: something
between "scaffolded" and "testable/exportable" that AgentWorks shouldn't guess at or
run automatically (kept fully independent of `test`/`export`, same as those two are
independent of each other). Alongside it, a real bug fix in
`internal/targets/filecopy.CopyDirExcept` (used by every exporter): it copied
*everything* in an artifact's directory except the manifest, which meant a
`node_modules` (or `__pycache__`/`.venv`/`.git`) present at export time got zipped and
shipped into every vendor package. Fixed with a small fixed skip-list checked during
the directory walk, language-agnostic rather than Node-specific.

That combination is also the answer to sharing code between several small Node
artifacts without reopening the "no shared workspace" decision two paragraphs up: put
shared logic in its own ordinary npm package under `packages/<name>/` at the project
root (invisible to `Discover`, which only ever walks the five kind directories), have
an artifact depend on it via a `file:` reference during development (an npm symlink,
immediate edit loop, no publishing needed), then give that artifact a `build:` command
that bundles the shared code in (e.g. an esbuild step) before `agentworks export` ships
it — since a `file:` symlink's target won't exist once the artifact directory is
copied out on its own. This stays a documented convention (see README.md, "Sharing
code between Node artifacts"), not new CLI surface: `packages/` needs no code change
to stay outside the artifact model, and bundling is just another `build:` command like
any other.

Also done as of this pass: two more Node scaffold templates, `node-ts-skill` and
`node-ts-tool` (`internal/scaffold/templates.go`), giving TypeScript the same
guided-boilerplate treatment as the plain-JS `node-skill`/`node-tool` next to them.
Structurally identical to those two (same `extraDirs`/`files` shape), plus a
`tsconfig.json` (`noEmit: true` -- it only type-checks) and an eagerly-set `build:`
command wired to `tsc --noEmit && esbuild ... --bundle`, generated by four small
TS-flavored siblings of the existing `nodePackageJSON`/`nodeEntrypointPlaceholder`/
`nodeTestPlaceholder` helpers. `build:` is set by default here (unlike `test:`, which
these templates still leave for the user to wire up once real tests exist, matching
every other template) because it isn't optional the way installing an untouched
placeholder's dependencies is -- a `.ts` entrypoint literally cannot run without being
compiled first, the same reasoning `node-tool` already applies to eagerly setting
`command:`. `entrypoint:` points at the TypeScript source (what `doctor` checks exists,
and what a human reads), while a tool's `command:` points at the bundled
`dist/index.js` (what actually runs) -- `doctor` only resolves a declared command's
*interpreter* against `PATH`, never checks the target file exists, so this is safe
immediately after scaffolding, before `agentworks build` has ever run. Manually
verified end-to-end, not just `TestEveryTemplateScaffoldsAndValidates`: scaffolded
both, ran `npm install && agentworks build`, confirmed the bundled output runs (and
throws the placeholder's error as expected) and `npm test`
(`node --experimental-strip-types --test`) passes, and confirmed
`agentworks export` on the tool ships `dist/index.js` but never `node_modules`.

Also done as of this pass: `internal/tui`'s browser gained tabs, replacing its old
two-pane kind → artifact drill-down with one `paneBrowse` pane -- a tab per
`artifact.Kind` (`tab`/`shift+tab` to switch, a small hand-rolled `tabBar` in the new
`tabs.go`, since `bubbles` has no tab widget of its own) showing that kind's artifacts
directly, no separate "pick a kind" step first. The templates pane (`b`) got the same
treatment: one `list.Model` per kind instead of a single list mixing all 17 templates
with the kind only visible in each row's description; pressing `b` also now carries
the browse pane's current tab into templates, so looking at "skills" and pressing `b`
lands on the "skill" tab rather than always the first kind. Alongside this: a project's
`agentworks.yaml` `targets:` default (already written by `agentworks init --target`,
and already used as a fallback by `cmd/new.go`'s non-interactive path) now actually
reaches every *interactive* creation flow too -- `agentworks new`'s wizard, the TUI's
`n`, and the TUI's create-from-template -- which previously always opened Targets as a
blank multi-select regardless of what the project already declared. Fixed by pre-
filling `NewArtifactAnswers.Targets` before the form is built rather than falling back
to it afterward (confirmed against `huh`'s own source, `field_multiselect.go`, that a
pre-populated bound slice is genuinely sufficient to pre-check matching options -- no
library gap, just three wiring gaps). `agentworks init` itself gained a matching
interactive prompt (`huh.NewMultiSelect`, same options `internal/tui/wizard.go` already
builds from `targets.All()`) for when `--target` isn't passed in a real terminal,
mirroring `new`'s existing "flags for scripts, wizard for terminal" split. Also, since
color had never really been used in this package (`titleStyle`/`helpStyle`/
`statusStyle` were `Bold`/`Faint`/`Padding` only, no `Foreground` anywhere): a small
semantic palette in the new `styles.go` (`accentColor`/`mutedColor`/`success`/`error`/
`warn`/`infoColor`, `AdaptiveColor` for light/dark terminals), used by the tab bar, a
`statusLevel` paired with `statusMsg` so the footer status line is actually colored by
outcome instead of every message looking identical, the help footer's keys rendered
distinctly from their descriptions, and `renderArtifact`'s metadata block given muted
labels. Deliberately not built: swapping the hand-rolled help line for `bubbles/help`
(a bigger structural change for a handful of static hints, not clearly worth it) or any
per-project/per-user theme configuration (a fixed palette is enough until someone
actually asks for a different one).

Also done as of this pass: BRAINSTORM.md's "Tool development experience" section,
both items. `agentworks doctor [path]` (`cmd/doctor.go`) is a static, side-effect-free
preflight check — a `doctorChecks` sibling of `validate`'s own checks, run per artifact:
every declared shell command (`command:`/`test:`/`eval_runner:`) has its interpreter/
binary resolved against `PATH` (`exec.LookPath`), a declared `entrypoint:` is checked to
actually exist, and a tool's `auth:` variables are checked for presence in the
environment — as a warning, not a failure (`--strict` promotes it), since they're only
needed to actually call the tool, not to discover what it offers. `agentworks run
<tool>` (`cmd/run.go`) is the live counterpart: starts the tool's declared `command` as
a real MCP server and opens `internal/inspector`'s full-screen UI (see Architecture
above) to browse its tools, fill in and submit a call from a form built off each tool's
`inputSchema`, and inspect the result, call history, and raw JSON-RPC/stderr traffic —
serving the same "test a tool for real, not just its mocked logic" gap `agentworks
test` always left. `run` only accepts `tool` artifacts (skills/agents/hooks/workflows
aren't MCP servers) and, unlike `export`'s `${VAR}` placeholders, actually executes with
the real environment, so a missing `auth:` variable is surfaced as a header warning
(from the same check `doctor` runs) rather than a silent failure. Manually verified
end-to-end (not just unit-tested) against a hand-rolled stdio MCP server standing in for
a real tool: connect, list tools, fill and submit a call, see the result rendered in the
split-pane layout, inspect the raw wire traffic and call history panes, and a clean
shutdown with no orphaned process — plus the connection-failure path specifically,
confirming a broken tool's actual stderr traceback (not just a generic "connection
closed" protocol error) is reachable via the log pane from the failure screen. Also
verified: a real subprocess's window size genuinely matters here — the two-column
layout collapses to a single visible column if the terminal reports a 0×0 size (only
relevant to non-standard/scripted terminal environments; a real interactive terminal
always reports its actual size), which is why `internal/inspector`'s `applySizes` is a
documented, deliberate no-op rather than a silent one when `m.width`/`m.height` are
still zero. Deliberately out of scope for this pass, not overlooked: an MCP inspector
for anything beyond `tools/call` (resources, prompts, sampling, roots) — see
`internal/mcpclient`'s own doc comment — and combining `doctor`'s static checks with a
live connectivity probe; they're kept as two separate, differently-costed operations
(one instant and side-effect-free, one that actually starts a process) rather than
merged into one that's sometimes slow and sometimes not.

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

### Cursor and Gemini CLI: what's real vs. deferred

Researched (Sept 2026, against cursor.com/docs and geminicli.com/
github.com/google-gemini/gemini-cli) whether Cursor and Gemini CLI could be added as
export targets the same way `chatgpt`/`m365-copilot` were. Unlike ChatGPT, both have a
real, confirmed, file-based format for most kinds — see
`internal/targets/cursor`/`internal/targets/geminicli`. Neither has a plugin/bundle
format like Claude Code or GitHub Copilot, though: every kind maps to a loose,
project-scoped file or directory instead of an installable package.

**Cursor** (`internal/targets/cursor`):
- **Skill** → a project rule, `.cursor/rules/<name>.mdc` (YAML frontmatter
  `description`/`alwaysApply: false` + Markdown body), since Cursor has no native Agent
  Skills/SKILL.md support. The skill's own supporting files are copied alongside it so
  its instructions can still reference them by the same relative paths.
- **Agent** → a real subagent file, `.cursor/agents/<name>.md`. Only `name`/`description`
  are populated — deliberately deferred, not overlooked: Cursor's docs don't confirm a
  `tools:` allowlist field on subagents at all, and `model:` only accepts `inherit` or a
  literal, fast-moving model ID with no documented stable tier alias, so there's nothing
  honest for `agentcaps` to map to yet (the same call already made for GitHub Copilot's
  agent export). Re-open once Cursor documents either.
- **Tool** → `.cursor/mcp.json`, confirmed identical in shape to Claude Desktop's
  `claude_desktop_config.json` — reuses `mcpconfig.ServerFor` directly.
- **Hook** → `.cursor/hooks.json`. AgentWorks' hook artifact events are already
  vendor-shaped, not translated (see `claudecode/hook.go` above) — a hook targeting
  Cursor is expected to use Cursor's own event names (`beforeShellExecution`,
  `afterFileEdit`, `preToolUse`, ...), unrelated to Claude Code's or Gemini CLI's.
- **Workflow** → unsupported. No plugin/bundle/orchestration format exists; everything
  above is loose per-project config, not an installable package.

**Gemini CLI** (`internal/targets/geminicli`):
- **Skill** → a Gemini CLI **extension** directory (`gemini-extension.json` manifest +
  `GEMINI.md` context file + supporting files) — a real, distributable, installable
  unit, the closest analog Gemini CLI has to a Claude Code plugin-wrapped skill.
- **Agent** → a real subagent file, `.gemini/agents/<name>.md`, with a real `tools:`
  array and `model:` mapping (`agentcaps.ForGeminiCLI`) — unlike Cursor, Gemini CLI
  documents both a real built-in tool-name vocabulary (`read_file`, `write_file`,
  `run_shell_command`, `web_search`, ...) and real evergreen model aliases
  (`gemini-flash-lite-latest`/`gemini-flash-latest`/`gemini-pro-latest` — Google's own
  stable, deliberately hot-swapped pointers, the same kind of tier alias `ForClaudeCode`
  already uses for `haiku`/`sonnet`/`opus`), so there's something honest to map to.
- **Tool** → an extension's `gemini-extension.json` with only its `mcpServers` field
  populated (no `GEMINI.md` needed for a bare tool) — same server shape as
  Cursor/Claude Code, just nested under the manifest instead of a bare top-level file.
- **Hook** → a `.gemini/settings.json` *fragment* (just the `hooks` key), not a complete
  file. Gemini CLI hooks live only in `settings.json`, shared with unrelated user/project
  settings — there's no extension-scoped hook format to write a self-contained artifact
  to, so the output is meant to be merged in by hand, the same "AgentWorks produces an
  artifact, never edits your live project" posture a bare tool's `.mcp.json`-shaped
  output already has elsewhere. Event names are raw-passthrough, same reasoning as
  Cursor's hooks above.
- **Workflow** → unsupported. No orchestration/composition primitive exists beyond a
  single extension's own commands.

If either vendor ships a real plugin/bundle format, or documents a stable
tools/model mapping for Cursor agents, re-open the relevant gap then — don't assume
today's reasoning still holds without checking.

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
