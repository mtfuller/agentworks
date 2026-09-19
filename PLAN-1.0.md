# Plan: v0.17 to v1.0

Written from the post-v0.17 review. Where an item depends on a vendor detail that
couldn't be confirmed, it is marked as a **spike**: a short investigation whose outcome
sets the design.

## Status

| Milestone | State |
|---|---|
| M1 Freeze the formats | Done (v0.18 content) |
| M2 Import completeness | Done: MCP and hook import, path rewriting, safer updates, commit pinning, npm and generic git sources, Copilot layout, `SECURITY.md`. Not done: `command`-sourced marketplace entries (deliberately), Copilot `plugin.json` inline `mcpServers`. |
| M4 MCP transports and coverage | Done: streamable HTTP and SSE transports, `run`/`test --remote`, resources and prompts in the inspector, coverage floors in CI, scaffold e2e job. |
| M5 Evals v2 | Done: json runner protocol, trace/tool-argument/activation assertions, trigger cases, judge-graded rubrics, `runs`/`pass_threshold`, timeouts, JUnit, reference Claude Code runner and judge (the runner is not verified against a live authenticated `claude`). |
| M6 Conformance and platform | Done: vendored Agent Plugins schemas and conformance tests, nightly real-CLI checks (Claude, Copilot, Gemini), format metadata, stale merged entries, `doctor` sh check, macOS/Windows CI, `--json` for add/update/export/build, lint and govulncheck. Real-tool spikes found and fixed marketplace `owner`, remote transport names, Cursor/Gemini missing files. |
| M3, M7 | Not started |

## What 1.0 promises

1. **Stable formats.** A project that works on 1.0 keeps working on 1.x: artifact
   frontmatter, `agentworks.yaml`, the lockfile, and the `--json` documents.
2. **Exports that install.** Output is checked against the vendors' real schemas or tools,
   not only against our own fixtures.
3. **Symmetry.** Anything that can be exported can be imported, and dependencies between
   artifacts are declared and checked.
4. **Trustworthy testing.** MCP servers can be tested locally and remotely, and evals can
   check behavior, not just final text.
5. **Supported platforms are stated and covered.**

## Decisions

| # | Decision | Recommendation |
|---|---|---|
| D1 | Where does the format version live? | One `format: 1` in `agentworks.yaml`, not per artifact. Missing means the current format. |
| D2 | Hook model for 1.0 | `handlers:` with event, matcher, timeout, and `type: command` only. Prompt/agent handlers wait until vendors converge. |
| D3 | Eval runner protocol | Opt-in JSON on stdout, so existing text runners keep working. |
| D4 | Windows | Document "POSIX shell required (WSL or Git Bash)", add a `doctor` check, run CI on Windows for the non-shell code. No native `cmd` support in 1.0. |
| D5 | Release cadence | One minor version per milestone (v0.18 to v0.24), then `v1.0.0-rc.1`, then 1.0. |
| D6 | Trust model for `add` | Pin the resolved commit SHA and require confirmation for shell commands. State the model in `SECURITY.md`. No signature verification. |

## Milestones and dependencies

```
M1 Freeze formats ──┬─► M2 Import completeness ──┐
                    ├─► M3 Dependencies ─────────┤
                    └─► M4 MCP transports + coverage ──► M5 Evals v2 ──► M6 Conformance + platform ──► M7 Docs + RC
```

M2, M3 and M4 can run in parallel once M1 lands. M6 waits for the formats to be frozen.

## M1: Freeze the formats (v0.18)

**Field registry.** `internal/artifact/fields.go`: a table of every public frontmatter field
per kind (name, type, required, meaning). Single source of truth for `validate` (which gains
a warning for unknown keys, catching typos like `comand:`), generated reference docs, and the
embedded authoring skills. This forces the decision on which `Extra` keys are public:
`entrypoint`, `command`, `args`, `env`, `auth`, `transport`, `url`, `headers`, `events`,
`test`, `build`, `eval_runner`, `resources`, `tools`, `model`. Anything not in the registry
is unsupported.

**Hook model v2 (D2).** `handlers: [{event, matcher, command, timeout}]`; `events` plus
`command` stays as shorthand that expands to one handler per event. Update the four hook
exporters (claude-code, github-copilot, cursor, gemini-cli) and `buildHooksDoc`. Where a
target can't express a field (e.g. a Cursor timeout), export warns rather than dropping it
silently. Golden tests per target.

**Version markers.** `format:` in `agentworks.yaml`; `project.Load` rejects a value newer
than the binary understands, with a clear message. `agentworks version --json`.

**Machine-readable contract.** JSON Schemas for each `--json` document under
`docs/schemas/`, validated against real output by golden tests. Rule: fields may be added;
removing or retyping one needs a `jsonSchemaVersion` bump.

**Process documents.** `CHANGELOG.md` and `COMPATIBILITY.md` (deprecation policy: a
deprecated field warns for one minor version before removal; after 1.0, removals only in a
major version).

**Exit criteria:** every public field is in the registry and documented; hook v2
round-trips through all four exporters; schemas are published and enforced by tests; the
example project uses only registry fields.

## M2: Import completeness (v0.19)

Depends on M1's hook model.

**MCP import.** Read `.mcp.json` (and Copilot's `mcp.json`); each server entry becomes one
`mcp` artifact. Secret-looking values go to `auth:` as `${VAR}` references; literal
token-looking values warn. Rewrite `${CLAUDE_PLUGIN_ROOT}`-style paths and copy referenced
scripts into the artifact directory (the hardest part; fixture tests from real plugins). All
imported commands go through the existing security scan and confirmation gate.

**Hook import.** Read `hooks/hooks.json`; group handlers by script/command; one artifact per
distinct group, named `<event>-<matcher>` when there is no script. Golden tests that
export → import → export is stable.

**Safer updates.** Record a hash of the local directory at import time in the lockfile so
`update` can tell locally edited artifacts from untouched ones. `update --apply` refuses to
overwrite local edits without `--force`; add `update --diff`; add `add --force`.

**More sources.** Generic git fallback (local `git` binary) for GitLab/Bitbucket/other
hosts; npm-sourced marketplace entries; GitHub Copilot's `com.github.copilot/` layout.

**Trust model (D6).** Resolve every ref to a commit SHA at import and pin it; `update`
reports when the upstream ref has moved. `SECURITY.md` states exactly what is and isn't
verified.

**Exit criteria:** importing a real multi-component plugin yields skills, agents, MCP servers
and hooks; `Plan.Unsupported` is empty for supported layouts.

## M3: Dependencies and runtime requirements (v0.20)

Depends on M1's registry.

**`requires:`.** Optional list on any artifact with kind-prefixed references, e.g.
`requires: [skill:csv-analyzer, mcp:jira-fetch, agent:researcher]`. Validated for existence
(including namespaced names), cycles, and kind correctness.

**Bundle closure.** `export` of a subset errors if a required artifact isn't in the bundle
(warns if it exists elsewhere in the project). `list --json` includes dependency edges;
optionally `agentworks graph` for a text/DOT view.

**Runtime requirements.** `bins: [python3, node]` on mcp/hook/skill artifacts; `doctor`
checks each is on `PATH`, with an optional minimum version (`node>=20`).

**Spike:** whether Claude Code and Copilot subagent formats have a native field for
referencing skills. If so, map `requires:` onto it; if not, export the dependency
information into the agent's generated instructions.

**Exit criteria:** a dangling reference fails `validate`; a partial export that would produce
a broken bundle fails loudly.

## M4: MCP transports and test coverage (v0.21)

Independent of M2/M3.

**Transport abstraction.** Refactor `internal/mcpclient` so `Client` sits on a `Transport`
interface instead of an `io.Writer`/`io.Reader` pair. Keep stdio; add streamable HTTP
(including servers answering with an SSE stream) and the legacy SSE transport. Headers come
from `auth` environment variables and are never logged.

**Consumers.** `agentworks run` and the inspector work for remote servers; `agentworks test`
smoke-checks remote servers only with an explicit `--remote` flag (network calls); the
inspector gains resources and prompts panes.

**Coverage floors**, enforced in CI by a small script:

| Package | Was | Target |
|---|---|---|
| `internal/mcpclient` | ~62% | 85% |
| `internal/inspector` | ~16% | 60% |
| `internal/tui` | ~67% | 75% |

Test Bubble Tea models by feeding messages to `Update` directly. Add command-level tests for
`build`, `add`, `update`, `status`, and `marketplace`.

**Scaffold end-to-end test.** A CI job with network access that scaffolds every template and
installs, builds, and tests it. This keeps the TypeScript template honest.

**Exit criteria:** coverage floors hold in CI; `run` works against a hosted MCP server.

## M5: Evals v2 (v0.22)

**Runner protocol (D3).** `eval_protocol: text | json` (artifact or `agentworks.yaml`);
default `text`. With `json` the runner prints
`{"text": "...", "tool_calls": [{"name": "...", "arguments": {}}], "activated": ["skill-name"], "usage": {}}`.

**New assertions.** `tool_called`, `tool_not_called`, `tool_args_match` (tool-call trace);
`rubric` (a judge command receives `{prompt, response, rubric}` and returns
`{pass, reason}`; project-level `eval.judge_runner`; AgentWorks still never calls a model
itself); trigger cases (`type: trigger`, `should_trigger: true | false`, checked against
`activated`) to test whether a skill's description actually selects it.

**Reliability features.** `runs: N` with a pass threshold; `--junit` output; keep `--json`.

**Reference runners** under `examples/eval-runners/`. **Spike:** confirm the exact headless
invocation and JSON output flags of the vendor CLIs before writing any.

**Exit criteria:** an eval can assert a skill triggers on one prompt and not another, and can
fail on a wrong tool call.

## M6: Vendor conformance and platform (v0.23)

**Schema validation (offline).** Vendor the Agent Plugins schemas (`plugin`, `mcp`,
`marketplace`) into `internal/targets/testdata/` and validate every exporter's output in
unit tests. **Spike:** which other vendors publish schemas.

**Real-tool install checks (scheduled).** Nightly workflow, outside PR CI, installing an
exported plugin into each vendor CLI where a headless mode exists; opens an issue on failure.
**Spike:** the exact commands for Claude Code, Copilot CLI, and Gemini CLI. Where none exists,
fall back to schema validation and say so.

**Format metadata.** "Last verified" and a format note per target, shown in
`agentworks targets`.

**Stale merged entries.** On a whole-project export to a loose target (Cursor, Gemini), clear
the merged file first and rebuild it from the full artifact set; partial exports still merge.

**Platforms (D4).** `doctor` checks `sh` is on `PATH`; Windows and macOS legs in CI for the
pure-Go code; an experimental `windows/amd64` release build; document the POSIX-shell
requirement.

**Also:** `--json` for `add`, `update`, `export`, `build`; `golangci-lint` and `govulncheck`
in CI.

**Exit criteria:** every exporter's output is schema-validated; nightly job green;
stale-entry cleanup tested.

## M7: Docs, onboarding, release candidate (v0.24 → 1.0)

- Generated frontmatter reference from the M1 registry; task-oriented guides (author → test
  → export → publish, CI, importing, evals); a trust and security page plus `SECURITY.md`;
  shell completion documented.
- Rewrite the starter example's `jira-fetch` to the dependency-free server pattern so
  `agentworks test` passes on a fresh clone without `pip install mcp`.
- Dogfood on two or three real projects, including a real marketplace repo consumed by
  Claude Code and Copilot; fix what breaks.
- Tag `v1.0.0-rc.1`, soak, tag `v1.0.0`. Pin `action.yml` and the `init --ci` template to a
  release tag instead of `@main`.

## Cross-cutting rules

- Gate every commit/merge on the test exit status directly, never on filtered output.
- A test alongside every behavior change.
- Update `README.md`, `AGENTS.md`, and the embedded project docs in the same change as the code.
- Keep `CHANGELOG.md` current from M1 on.

## Risks

| Risk | Mitigation |
|---|---|
| Plugin path rewriting (M2) is the likeliest place for subtle breakage | Fixture tests from real plugins; export → import → export stability tests |
| The hook model change touches four exporters | Golden tests first, then change the model |
| Vendor install commands (M6) may not exist headlessly | Spikes early; schema validation is the fallback |
| Vendor formats change during the plan | Nightly job and "last verified" metadata make drift visible |
| Eval protocol design (M5) is the least certain | Opt-in JSON keeps the current protocol working; start small |

## Explicitly not in 1.0

Native Windows shell support, a workflow engine, project-defined templates, shared snippets,
watch mode, skip-unchanged export, new vendor targets.

## Effort

M1 medium, M2 large, M3 medium, M4 large, M5 large, M6 medium, M7 small to medium. M2, M3,
and M4 parallelize, so the critical path is M1 → M4 → M5 → M6 → M7.
