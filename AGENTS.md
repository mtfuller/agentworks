# AGENTS.md

Guidance for coding agents working in this repository.

## What this project is

**AgentWorks** is a local-first, vendor-agnostic tool for building agent contexts,
skills, MCP servers, and hooks: author them once as plain files, then export
target-specific artifacts for the AI harnesses people actually use (Claude Code,
ChatGPT, GitHub Copilot, Cursor, Gemini CLI).

The repo was bootstrapped from `starterpack-go-cli` (a Go/Cobra CLI template) — the
logging, color output, and Taskfile are that template's conventions, kept because
they're a reasonable foundation, not because they're AgentWorks-specific. The core
model (project manifest, artifact kinds, scaffolding, the vendor target registry, and
real exporters for all five registered targets) is implemented; see Architecture below
for what's real today versus deliberately deferred. The project has no external users
yet, so breaking changes (renamed kinds, changed frontmatter) don't need migration
paths.

## Guiding scenarios

These are the user scenarios the design should stay accountable to (see the project
brief for full detail — ask the user if it's not in context). Treat them as the source
of truth for what "done" looks like for any given piece of functionality:

1. **Data analyst** — a skill (Python script + tests + sample CSVs) validated locally by
   running its test scripts, then exported as a ChatGPT skill upload, versioned per
   export. *(Works end to end, see `examples/starter-project/skills/csv-analyzer`.)*
2. **Developer** — an MCP server that fetches/parses a Jira ticket (real API + auth, with
   a simulated API for tests), validated locally, then exported to Claude Code, GitHub
   Copilot, Cursor, and Gemini CLI. *(Done — see `examples/starter-project/mcp/jira-fetch`:
   a real MCP server (`pip install mcp`, one `fetch_issue` tool) whose
   `tests/test_main.py` simulates the Jira API rather than calling a real instance.)*
3. **AI researcher** — a domain-specific agent with its own guidance markdown and
   resources, exported to Claude Code, GitHub Copilot, Cursor, and Gemini CLI as native
   subagent files. *(`agentworks export agents/x --target claude-code`. `chatgpt` is a
   deliberate, closed gap — see "ChatGPT: why skills only" below.)*
4. **Engineering leader** — several agents and MCP servers that work together as a
   pipeline, exported as one or more plugins. There is deliberately no "workflow" kind:
   an agent whose instructions describe how it delegates to other agents and calls MCP
   servers is the same thing, and the vendor's own agent loop executes it — AgentWorks
   never runs anything itself. *(See `examples/starter-project/agents/software-factory`;
   `export` bundles a whole project, or one plugin per namespace, and `marketplace`
   publishes it as a plugin marketplace repo.)*
5. **Any user** — guided boilerplate generation for a new agent/skill/MCP server/hook,
   including generated sample artifacts to learn the system from. *(`agentworks new`'s
   interactive wizard, `--from-template`, and `examples/starter-project`. The mcp
   scaffolds are working servers, so `test` and `run` pass on a fresh scaffold.)*
6. **Any user** — a project layout that's navigable in a plain text editor, with local
   test/validate/simulate workflows and room to build genuinely custom, heavier tooling
   when a scenario needs it. *(The `<kind>.md` format + `validate`/`test`/`doctor`/`run`.)*
7. **Any user** — a polished TUI for building agents/MCP servers/skills, with control
   over whether an export is a standalone skill zip or a full plugin. *(`agentworks tui`:
   `n` creates, `e` exports, `t` tests, `p` browses marketplace plugins, `b` browses
   templates; see `internal/tui`.)*
8. **Any user** — early, explicit visibility into which capabilities are portable across
   every target vendor versus specific to a subset, before investing effort in either.
   *(`agentworks targets`.)*
9. **Team** — the project's checks run in CI and can be consumed by other tools.
   *(`--json` on the reporting commands, `validate --strict`, `doctor`,
   `marketplace --check`, `status --fail-on-drift`, `action.yml`, `init --ci`.)*

Implication for design: the artifact model (agents, skills, MCP servers, hooks) must be
defined independently of any vendor's format, with per-target exporters/transforms
layered on top — never model something in a single vendor's native shape and
reverse-engineer the rest.

## Architecture (keep these layers intact)

```
main.go → cmd/ (Cobra commands, CLI surface) → internal/tui (Bubble Tea browser + `new` wizard)
                                              → internal/{artifact,project,scaffold,export,targets,importer,...}
```

- **`cmd/`** — one file per Cobra command. Commands parse flags/args, call into the
  packages below, and format output. Keep business logic out of `Run`/`RunE` — a
  command file should read as "gather input, call one function, print the result."
  `output.go` holds the `--json` plumbing (see Conventions).
- **`internal/artifact`** — the vendor-agnostic artifact model: `Kind`
  (agent/skill/mcp/hook; the mcp kind lives in `mcp/` rather than `mcps/`, see
  `Kind.DirName`), `Frontmatter` (common fields explicit, kind-specific fields
  round-trip through `Extra` so there's one parser for all four kinds), and
  `Parse`/`Render`/`Load`/`Save` for the `<kind>.md` (YAML frontmatter + Markdown body)
  file format. This is the one place that understands that file format — nothing else
  should hand-parse it. `lint.go`'s `LintDescription`/`LintOverlap` are heuristic
  description-quality checks (not structural validation, which stays in `Validate()`);
  `fields.go` is the **field registry**: every supported frontmatter field per kind, with
  its type and doc. `validate` uses it to warn about unknown keys (they're ignored on
  export, so a typo does nothing) and to reject a wrongly-typed field, and
  `docs/reference/frontmatter.md` is generated from it (`FieldsMarkdown`; regenerate with
  `UPDATE_GOLDEN=1 go test ./internal/artifact`). **A new frontmatter field must be added
  to the registry** or it will warn as unknown; the embedded authoring skills must mention
  every kind-specific field (a test enforces it). `x-` keys are the users' own and exempt.
  `hook.go`'s `HookHandlers` is the single reader of a hook's handlers (the explicit
  `handlers:` list or the `events`+`command` shorthand); every hook exporter, `validate`,
  `doctor`, and the security lint go through it. `security.go` scans a hook/mcp `command`: `LintSecurityNotice` (it runs shell code —
  informational, never fails `--strict`) versus `LintSecurityRisks` (hostile-looking
  shapes — these count).
- **`internal/project`** — `agentworks.yaml` (the project manifest: `format`, name,
  targets, an optional `eval` block; `Load` refuses a `format` newer than `CurrentFormat`,
  so bump that only for a change an older binary couldn't read correctly), `Init` (scaffold a new project, plus the embedded `AGENTS.md` and
  `.agents/skills/` from `embedded/`, and `WriteCIWorkflow` for `init --ci`), `FindRoot`
  (walk upward for the manifest, like git finds `.git`), and `Discover` (walk the kind
  directories and load every artifact).
- **`internal/scaffold`** — `New(root, kind, name, opts)` writes a new artifact's
  directory + starter files, either from the kind's default `kindSpec` or a named
  built-in `Template` (`templates.go`, `mcp_templates.go`). This is the single code path
  both `cmd/new.go` and the TUI wizard call — never generate an artifact's files by hand
  in either front end. `mcp_servers.go` holds the starter MCP server sources (Python,
  Node, TypeScript): complete, dependency-free stdio servers, so a scaffold passes
  `agentworks test` immediately. `TestScaffoldedMCPServersAreWorking` runs them.
- **`internal/evalspec`** — the `evals/*.yaml` case format, the response model, and the
  checks. `LoadDir` parses strictly (`KnownFields`, so a typo can't make a vacuous case) and
  refuses a case that asserts nothing or a duplicate name. `ParseResponse` reads a runner's
  stdout under the **text** protocol (the response) or the **json** protocol (`{text,
  tool_calls, activated, usage}`), tracking whether the trace keys were *reported* so an
  unreported trace can't satisfy a "not called" assertion. `Evaluate(case, response,
  subject)` covers text, tool-call, tool-argument, activation, and `should_trigger`
  assertions; `rubric` is graded by a judge command (`judge.go`: `JudgeRequest` in,
  `JudgeVerdict` out, and a verdict with no `pass` is an error). `junit.go` writes the
  report. It doesn't run anything — `cmd/eval.go` does: it resolves the runner, protocol,
  and judge (artifact frontmatter over `agentworks.yaml`'s `eval:` block), runs each case
  `runs` times under a timeout (in its own process group, killed whole on timeout so a
  model call isn't orphaned), and aggregates against `pass_threshold`. AgentWorks never
  calls a model itself: the runner and the judge are the project's own commands.
- **`internal/targets`** — the static vendor registry (which artifact kinds each vendor
  can consume — this is what `agentworks targets` prints) and the `Exporter` interface.
  Vendor-specific exporters live in their own subpackage and self-register via `init()`
  + `targets.Register`; `internal/export` blank-imports each. Shared packages:
  - `agentskills` writes the spec-compliant
    ([agentskills.io](https://agentskills.io/specification)) `SKILL.md` shape that
    `claudecode`, `chatgpt`, and `githubcopilot` build on for skills — extend *that*
    package for a skill-format change, not each vendor package individually.
  - `mcpconfig` is the one place an mcp artifact becomes a `{"mcpServers": {...}}` entry
    (`ServerFor`/`ServerForDir`), and the one place its frontmatter is validated
    (`Validate`, called by `agentworks validate` and by every exporter). It handles
    `transport` stdio/http/sse, `command` (via `sh -c`) or `command`+`args` (exec'd
    directly), `env` (non-secret literals), `auth` (secret names, passed through as
    unresolved `${VAR}` references — never values), and `url`/`headers` for remote
    servers. `CommandLine` is the single source of the shell line that `run`, `test`, and
    the bundle exporters execute. Gemini CLI keys remote servers differently (`httpUrl`
    for streamable HTTP, `url` for SSE, no `type`), so `geminicli/manifest.go` converts.
  - `filecopy` copies an artifact's files (excluding its manifest and a fixed skip-list:
    `node_modules`, `__pycache__`, `.venv`, `.git`) and zips a directory.
  - `agentcaps` is AgentWorks' vendor-agnostic vocabulary for what an agent may do
    (`tools:`, a closed set like `read-files`/`web-search`) and how capable a model it
    needs (`model:`, a tier: `fast`/`balanced`/`powerful`), plus the mapping functions
    that turn them into each target's real shape: `ForClaudeCode` and `ForGeminiCLI`.
    There is deliberately no `ForGitHubCopilot` or `ForCursor` — see "Deferred" below.

  Hook export (`claudecode`/`hook.go`, `githubcopilot`/`hook.go`, ...) is deliberately
  *not* a shared package like `mcpconfig`: the vendors' `hooks.json` shapes genuinely
  differ (Claude Code nests hooks under a matcher block per event and uses `command`;
  Copilot is flat and uses `bash`/`timeoutSec`; Cursor is flat with `command`/`timeout`;
  Gemini CLI nests under a matcher group and its `timeout` is **milliseconds**, the others'
  seconds), so a shared struct would fight the schema rather than reflect it. All four
  support `matcher` and a timeout (checked against each vendor's hooks docs, Sept 2026;
  re-verify in the conformance work). Hook events are vendor-shaped, not translated: a hook
  targeting Cursor uses Cursor's event names.

  `claudecode` and `githubcopilot` are each split one file per kind (`export.go`'s
  `Export` dispatches by `a.Kind`) plus a `plugin.go` for what every plugin they produce
  shares (the manifest writer, `writeMCPFile`) and a `bundle.go` implementing
  `BundleExporter` (one plugin holding many artifacts; an mcp member is filed under
  `mcp/<qualified-name>/` with its command wrapped in `cd` so two servers' `src/` don't
  collide, and hooks merge into one `hooks.json`). `cursor` and `geminicli` have the same
  per-kind dispatch but no `plugin.go` or bundle: their real formats are loose,
  project-scoped files, not installable plugins. Because several artifacts land in the
  *same* file there (`.cursor/mcp.json`, `.cursor/hooks.json`, `.gemini/settings.json`),
  those exporters **merge into an existing file** rather than overwrite it, keyed so
  re-exporting is idempotent (an mcp entry is replaced by name; an identical hook action
  isn't duplicated). Known gap: an entry for a since-deleted artifact isn't removed.
- **`internal/export`** — export orchestration (`export.Run`), shared by `cmd/export.go`
  and the TUI's export form: resolves targets (`--target` → manifest `targets:` → error),
  runs artifacts' `build:` commands first, bundles into one plugin (or one per namespace,
  or skills as `.zip`/`.skill`), falls back to one output per artifact for vendors with
  no plugin format, and records every export in the lockfile.
- **`internal/lockfile`** — `agentworks.lock`: import pins (content hash + source) and
  export records (source hash + output hash), read back fully offline by `agentworks
  status`.
- **`internal/importer`** and **`internal/marketplace`** — the reverse of `targets`.
  `agentworks add <url>` fetches a skill or Claude Code plugin (GitHub repo/tarball or a
  direct archive URL, via `codeload.github.com`, no local `git` or API rate limits) and
  decomposes it into local artifacts: a bare Agent Skill maps 1:1 (`agentskills.Read`),
  a plugin's `skills/*/SKILL.md` and `agents/*.md` the same way. Everything is resolved
  into an `importer.Plan` before anything is written, so a collision on artifact 4 of 5
  never leaves the first 3 written. Sources: GitHub (codeload tarball), a direct archive URL,
  `npm:` (registry tarball verified against `dist.integrity`/`shasum`), and any git remote
  (`fetch_sources.go`: a shallow clone with the local `git`, transports limited to
  https/ssh via `GIT_ALLOW_PROTOCOL` because git's `ext::` helper runs commands, prompts
  disabled, `.git` removed so content hashes deterministically). Two plugin layouts share
  `planPlugin` through a `pluginLayout` (Claude Code's `.claude-plugin/`, and Copilot's
  `plugin.json` + `com.github.copilot/`); MCP servers read the same either way. A plugin's MCP servers become mcp artifacts and its hook
  handlers hook artifacts (`mcp.go`, `hooks.go`): neither has a directory in the fetched
  source (each is an entry inside a JSON file), so each is built in a *staging directory*
  (`Plan.newStage`) holding the files it references plus a marker of the raw entry
  (`importMarker`); that directory is what's hashed for the lockfile and copied into the
  project. `${CLAUDE_PLUGIN_ROOT}` references are rewritten (`rewrite.go`): to `.` for an
  MCP server (it runs from its own directory) and to `${ARTIFACT_DIR}` for a hook. A literal
  credential in a server's env or headers is never written: that server is reported, by
  field name only, and skipped. AgentWorks' own claude-code bundles are recognized
  (`hook-files/<name>/`, and the `sh -c "cd '${CLAUDE_PLUGIN_ROOT}/mcp/<name>' && …"` wrapper)
  so importing your own marketplace round-trips (`roundtrip_test.go`). A lockfile entry
  pins the resolved commit and a `local_sha256` of the artifact as written, which is how
  `update --apply` refuses to overwrite your edits without `--force`; `update` re-plans the
  source under the artifact's existing namespace and matches by the entry's subpath
  (`mcp:<server>`, `hook:<identity>` for staged artifacts). Imports are namespaced (`Source.DefaultNamespace()`:
  the GitHub owner). `marketplace` searches agentskills.codes plus the Claude Code
  (`anthropics/claude-plugins-official`) and GitHub Copilot (`github/awesome-copilot`)
  catalogs — only permissively licensed plugins, license verified rather than assumed —
  and also *writes* this project's own `marketplace.json` files for `agentworks
  marketplace` (`.claude-plugin/marketplace.json` and `.github/plugin/marketplace.json`,
  the two locations Claude Code and Copilot CLI document). `marketplace --check` builds
  into a scratch directory and compares, by passing `publishMarketplaceTarget` a nil
  lockfile (which suppresses recording).
- **`internal/tui`** — the Bubble Tea project browser (`model.go`/`app.go`), a tab per
  artifact kind (`tabs.go`), the create/export actions (`actions.go`, `export_form.go`),
  the test action (`test_action.go`), the marketplace and template panes, and the
  `huh`-based create wizard (`wizard.go`) shared with `cmd/new.go`. `huh.Form` implements
  `tea.Model`, so a form is embedded as a child model (`Model.activeForm`) rather than
  run via its own blocking `.Run()`. The test action hands a plain `*exec.Cmd` to
  `tea.ExecProcess`, which suspends the alt-screen and hands the terminal to the child.
- **`internal/mcpclient`** — an MCP client. `Client` does the protocol (request/response
  correlation, `initialize`, `tools/list` and `tools/call`, `resources/list`/`read`,
  `prompts/list`/`get`, cursor pagination) over a `Transport` interface (`transport.go`):
  stdio (`StartProcess` runs `mcpconfig.CommandLine` via `sh -c`), streamable HTTP
  (`DialHTTP`: a POST per message, reply as JSON or an SSE stream, session id and protocol
  version echoed, best-effort DELETE on close), and legacy SSE (`DialSSE`: a GET stream that
  announces the POST endpoint). `Target`/`Open`/`Probe` (`target.go`) are the one way `run`,
  `test`, and the inspector connect to a local or remote server. Two security properties are
  tested: **credentials never leak** (headers are never traced or put in errors, a URL's
  query is stripped from errors, `ExpandEnv` drops a header with an unset variable rather
  than sending it half-expanded) and **headers never go to a different host** (cross-host
  redirects are refused, and an SSE `endpoint` on another host is rejected). Out of scope:
  sampling, roots, server-initiated requests, and the resumable GET stream.
- **`internal/inspector`** — the Bubble Tea UI behind `agentworks run`: a filterable tool
  list and detail/result viewport, a `huh.Form` generated per tool from its `inputSchema`
  (`schemaform.go`), a call-history pane, and a raw JSON-RPC/stderr log — reachable even
  from the connection-failure screen so the real cause (a missing package) isn't hidden
  behind a generic protocol error. A server that offers resources or prompts gets a section
  for each (`sections.go`, keys `1`/`2`/`3`); a prompt's arguments are collected by the same
  form builder by describing them as a JSON Schema (`promptAsTool`). A tools-only server
  looks exactly as it always did. A separate package from `internal/tui`: a different
  lifecycle (connect once, then browse/call/inspect until quit). The model is tested by
  feeding it messages against a fake MCP server, so no terminal is needed.
- **`internal/{logger,color,spinner,version}`** — CLI-support code inherited from the
  starter template. `color.SetOutput` redirects the message helpers (used to send them to
  stderr under `--json`).
- **`pkg/`** — currently unused. Reserved for logic that should be importable by other
  Go programs; nothing has needed that yet.
- **`tests/`** — black-box integration tests that exec the CLI. Unit tests live next to
  their package, not here.

### What's real vs. deferred

Implemented: the project/artifact model; scaffolding for all four kinds; project-wide
discovery, `build`, `test`, `eval`, `doctor`, `run`; the capability matrix; real
exporters for all five registered targets (`claude-code` and `github-copilot` for every
kind; `cursor` and `gemini-cli` for skill/agent/mcp/hook; `chatgpt` for skills only);
import (`add`/`update`); the marketplace publisher; and the CI surface (`--json`,
`marketplace --check`, `status --fail-on-drift`, `action.yml`).

**Removed on purpose** (don't re-add without the user asking):
- **Workflows.** A bespoke kind on top of agents/skills. An agent whose instructions
  describe delegation to other agents and MCP servers covers it, and the vendor's own
  agent loop executes that. AgentWorks never runs a workflow itself.
- **`m365-copilot`.** An outlier (a Teams-style declarative-agent app package with its own
  manifest, publisher identity, and icon handling) that shared nothing with the other
  exporters.
- **The `tool` kind name.** Renamed `mcp`: what it always was is an MCP server registration,
  and a remote server has no "tool" code at all.

**Deliberately deferred** (do this later, not by accident while doing something else):
- **`command`-sourced marketplace entries** (`{"source":"command"}`): they'd run an
  arbitrary command to produce the plugin, which is the opposite of the import trust model.
- **A Copilot plugin.json's own `mcpServers` field** (only `mcp.json` is read for Copilot).
- **Hook types other than `command`** (`prompt`, `agent`, `http`, `mcp_tool` in Claude
  Code) and hooks guarded by an `if` condition: reported in `Plan.Unsupported` on import,
  never approximated, because importing them without their condition or type would make a
  hook fire more broadly than its author intended.
- **Bundled hook scripts on targets other than claude-code.** A hook whose command uses
  `${ARTIFACT_DIR}` needs its files shipped and located at run time. claude-code does both
  (`hook-files/<name>/`, resolved via `${CLAUDE_PLUGIN_ROOT}`); no other target documents a
  plugin-root variable (github-copilot's hooks docs name none), so `targets.
  UnsupportedReason` makes export skip such a hook with a warning.
- **`tools:`/`model:` mapping for `github-copilot` and `cursor` agents.** Neither vendor's
  public spec confirms those fields (Cursor's `model:` takes only `inherit` or a
  fast-moving literal ID), so AgentWorks doesn't guess. Re-open once documented.
- **Eval runners for vendors other than Claude Code**, and per-run trace capture beyond
  tool calls and activations (token cost is reported, not asserted on). The reference
  runners in `examples/eval-runners/` have not been run against a live authenticated
  `claude`; they are tested against fixtures modeled on its stream-json envelope.
- **Project-defined custom templates** (built-in only for now).
- **Removing stale entries from merged loose files** (see `internal/targets` above).
- **`--json` for `add`, `update`, and `export`**, which the CI recipe doesn't need yet.

Design notes worth keeping:
- **Coverage floors are enforced in CI** (`scripts/check-coverage.sh`, floors in
  `scripts/coverage-floors.txt`). They are ratchets: raise a floor when coverage rises, and
  never lower one to make a change pass. Command behavior is tested in process through a
  harness (`cmd/harness_test.go`: `runCLI` resets every flag between runs and captures
  output) in addition to the black-box tests in `tests/`, which don't count toward `cmd`'s
  coverage. `scripts/scaffold-e2e.sh` (its own CI job, with network) scaffolds every template
  and builds, tests, and exports it.
- **`--json` documents have published schemas** (`docs/schemas/`), generated by reflection
  from the Go doc types in `cmd/jsonschema_test.go` (`UPDATE_GOLDEN=1 go test ./cmd`) and
  validated against real output by `tests/schema_test.go`. A new `--json` command needs an
  entry in `jsonDocTypes`. Slices must be initialized (`[]T{}`), because a nil slice marshals
  as `null` and fails the schema.
- **Targets are project-level.** `agentworks.yaml`'s `targets:` is the only place they
  live; artifacts don't declare their own.
- **`build:` is a generic frontmatter field** (`agentworks build`), a mirror of `test:`,
  run automatically before `export` (`--no-build` skips). It exists because Node/
  TypeScript artifacts need `npm install`/a compile step between "scaffolded" and
  "runnable". Artifacts stay self-contained, independently exportable leaf directories
  (no shared workspace or "build all" orchestration); sharing code between Node artifacts
  is a documented convention (README, "Sharing code between Node artifacts"), not CLI
  surface.
- **`validate` has three severities**: errors (structural), warnings (heuristic
  description quality, risky command shapes — fail only under `--strict`), and notices
  (informational — never fail). Conflating the last two once made `--strict` fail every
  project with an MCP server.
- **Description linting** (`LintDescription`/`LintOverlap`): over the Agent Skills spec's
  1024-character limit, under 20 characters, redundant with the name, or ≥70% Jaccard
  overlap with another same-kind artifact. A description is how an agent *finds* an
  artifact, and nothing else tested it.
- **The embedded project docs** (`internal/project/embedded/` — the `AGENTS.md` and
  `.agents/skills/` every new project gets) must be kept in step with the CLI and
  frontmatter rules; `TestInitWritesSkillPerKind` checks each skill's frontmatter.
- **Import namespaces**: bundle exporters file members via `targets.FlatNames` (bare name,
  `<namespace>-<name>` only on a collision) because plugin formats only discover
  `skills/<name>/` one level deep.

### ChatGPT: why skills only

Researched (Sept 2026) whether `chatgpt` could export agents/MCP servers the same
way `claude-code`/`github-copilot` do. Conclusion: there's currently no stable,
file-based target to export *to*, for reasons specific to each kind rather than "not
researched yet":

- **Agent.** Custom GPTs — the direct analog to a Claude Code subagent — are being actively retired as of this research (OpenAI announced
  retirement on 2026-09-11; new GPT creation ends 2026-09-25; full retirement
  2026-12-11). Their replacement, Workspace Agents, is a cloud-hosted, always-on,
  enterprise-workspace product with no local file format at all — there's nothing an
  `Exporter` could produce a droppable artifact *for*, even in principle.
- **MCP server.** ChatGPT's real external-tool mechanism, the Apps SDK, genuinely is
  MCP-based (same protocol `internal/targets/mcpconfig` already targets) — but
  registration is a hosted, publicly-reachable server URL configured through ChatGPT's
  UI/Developer Mode, not a local stdio process launched from a generated config file the
  way `.mcp.json` works. AgentWorks' `Exporter` interface
  (`Export(a, outDir, opts) (string, error)`, producing a local file/directory) doesn't
  fit "you need an already-deployed, running server, then register its URL by hand."

If this changes — a stable, documented, file-based ChatGPT format emerges — re-open the
investigation then; don't assume today's reasoning still holds without checking, this
area was unusually volatile even within the week it was researched.

### Vendor conformance (M6)

Exports are checked against the vendors' real formats, not only our own fixtures:

- `internal/targets/testdata/agentplugins/` vendors the Agent Plugins 1.0.0 `plugin` and
  `mcp` JSON Schemas (there is no marketplace schema). `githubcopilot/schema_conformance_test.go`
  validates every exporter output against them. The schemas are closed
  (`additionalProperties: false`), so a stray field fails: a remote MCP entry allows only
  `type`, `url`, `headers`; remote types are `streamable-http` and `sse`, not `http`.
  The plugin-name pattern uses a lookahead RE2 can't compile, so the test rewrites it.
- `tests/conformance_test.go` (gated by `AGENTWORKS_CONFORMANCE=1`, run nightly by
  `.github/workflows/conformance.yml`) hands output to the real CLIs: `claude plugin validate
  --strict`, `copilot plugin marketplace add` (with `COPILOT_HOME`), and `gemini extensions
  install --consent`. Copilot's install summary counts only skills and accepts an invalid
  `mcp.json`, and nothing headless verifies agents or hooks there, so the schema test is the
  real check for those.
- Each target carries `Format` and `Verified` in `internal/targets/registry.go`, shown by
  `agentworks targets`. Update the date when a target is re-checked.
- A local MCP server's files ship with Cursor (`.cursor/mcp-servers/<name>`, addressed via
  `${workspaceFolder}`) and Gemini (`${extensionPath}` + `cwd`), so the entry can run.
- A whole-project export to Cursor or Gemini clears their merged files first (only if the
  lockfile shows a prior export for that target), so removed servers/hooks don't linger.

### Cursor and Gemini CLI: what's real vs. deferred

Researched (Sept 2026, against cursor.com/docs and geminicli.com/
github.com/google-gemini/gemini-cli) whether Cursor and Gemini CLI could be added as
export targets the same way `chatgpt` was. Unlike ChatGPT, both have a
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
- **MCP server** → `.cursor/mcp.json`, confirmed identical in shape to Claude Desktop's
  `claude_desktop_config.json` — reuses `mcpconfig.ServerFor` directly, merging into any
  existing file so several servers land in the one file.
- **Hook** → `.cursor/hooks.json`. AgentWorks' hook artifact events are already
  vendor-shaped, not translated (see `claudecode/hook.go` above) — a hook targeting
  Cursor is expected to use Cursor's own event names (`beforeShellExecution`,
  `afterFileEdit`, `preToolUse`, ...), unrelated to Claude Code's or Gemini CLI's.

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
- **MCP server** → an extension's `gemini-extension.json` with only its `mcpServers` field
  populated (no `GEMINI.md` needed for a bare server) — same server shape as
  Cursor/Claude Code, just nested under the manifest instead of a bare top-level file.
- **Hook** → a `.gemini/settings.json` *fragment* (just the `hooks` key), not a complete
  file. Gemini CLI hooks live only in `settings.json`, shared with unrelated user/project
  settings — there's no extension-scoped hook format to write a self-contained artifact
  to, so the output is meant to be merged in by hand, the same "AgentWorks produces an
  artifact, never edits your live project" posture a bare MCP server's `.mcp.json`-shaped
  output already has elsewhere. Event names are raw-passthrough, same reasoning as
  Cursor's hooks above.

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
- Commands that report results take `--json`: build one result struct embedding
  `envelope` (`cmd/output.go`: `schema_version`, `command`, `ok`) and call `emitJSON` once.
  Under `--json` stdout must be exactly that document — human messages already go to
  stderr (`color.SetOutput`), and a spawned test/eval process's stdout goes through
  `stdoutForChildren()`. Return errors from `RunE` and let `Execute` report them (it emits
  an error document under `--json` if the command didn't). Only *add* fields to a shipped
  document; removing or retyping one needs a `jsonSchemaVersion` bump. Cover each document
  in `tests/json_test.go`.
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
2. `task test` (or `go test ./...`) passes. When chaining it before a commit, gate on the
   test's own exit status — filtering its output through `grep` hides a failure.
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
