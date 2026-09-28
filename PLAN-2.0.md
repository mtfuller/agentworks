# Plan: agent teams and local runtime

This plan implements the architecture in
[`docs/architecture/runtime-rearchitecture.md`](docs/architecture/runtime-rearchitecture.md).
It replaces the artifact-first product center with agent-team composition and a local,
owned runtime while retaining the working import, MCP, eval, security, and vendor-rendering
foundations.

The plan is organized around demonstrable vertical increments. A milestone is not complete
because its types exist; it is complete when its user-visible exit criteria pass.

## Status

| Milestone | State |
|---|---|
| M0 Architecture fixtures and guardrails | In progress |
| M1 Format 2 and resolved agent teams | In progress |
| M2 Studio and durable local state | Complete |
| M3 Manual runs and process supervision | Complete |
| M4 Harness adapters and bounded approvals | Complete |
| M5 Context, memory, outcomes, and monitors | Complete |
| M6 Events, schedules, and deterministic routing | Complete |
| M7 Jira and GitHub sources | Complete |
| M8 Packaging, providers, and platform hardening | Complete |
| M9 Cutover, dogfooding, and release | In progress |

Initial implementation (2026-09-24): format 2 is isolated from the existing format-1
command paths. `internal/spec` now strictly loads projects, local workspace bindings,
teams, Markdown agents, and host/container/remote tool variants. `internal/resolver`
validates team membership and delegation cycles, resolves local or installed native
skills/tools without silent shadowing, deduplicates shared capabilities, selects an
available tool provider, and produces a content-addressed deterministic plan.
`agentworks plan <team> [--provider ...] [--json]` exposes that read-only slice. Sources,
routes, monitors, dependency fetching/materialization, and format-2 save/migration paths
remain M1 work; none of the current format-1 commands have been switched over.

M0 implementation (2026-09-24): `internal/process` now provides structured argv,
exact-environment construction, serialized stdout/stderr events, cancellation, an 8-second
default grace period, Unix process-group termination, and Windows Job Object termination.
Native tests prove parent/grandchild cleanup and forced termination after ignored graceful
shutdown; Windows and Linux variants cross-compile. The existing eval runner is the first
consumer migrated onto this spine. Initial Claude Code and GitHub Copilot probe adapters
distinguish missing, unauthenticated, ready, and incompatible installations without making
a model request; observed local readiness is recorded in the harness conformance ledger.
Authenticated approval/output fixtures remain an M0 exit gate.

M2 implementation (2026-09-25): `agentworks studio --experimental` now validates a format-2
project, selects an available loopback port, serves an embedded dependency-free UI, emits
an SSE readiness event, and shuts down on Ctrl-C/SIGTERM. Host-header validation and strict
browser security headers are enabled from the first endpoint. A pinned pure-Go SQLite driver
opens a per-project database outside the checkout with WAL, busy handling, foreign keys, and
transactional checksum-protected embedded migrations. The initial schema covers every minimum
runtime record, and repositories provide deduplicated event ingestion, atomic one-run routing,
idempotent manual runs, optimistic run transitions, restart persistence, and a Studio run-list
endpoint. Its first state-changing endpoint uses a per-process session cookie, separate CSRF
token, and same-origin enforcement. The manual-run form resolves live project definitions,
requires a valid local workspace binding, caps authority at the agent's permission ceiling,
and atomically stores one event and one pending run. Studio now lists and edits the canonical
tracked project manifest, team, agent, skill, tool, and memory files. Saves validate the
individual definition, use an atomic same-directory replacement, require the SHA-256 version
that the browser loaded, and exclude machine-local configuration and dependency state. A
lightweight watcher automatically reloads clean external changes and warns without overwriting
when the browser also has unsaved edits. Storage health reports total per-project runtime bytes
against the configured limit without exposing host paths. This completes M2; leases, harness
execution, and approvals form the M3/M4 slice.

M3 execution spine (2026-09-25): the SQLite repository now claims pending runs with durable
attempts and expiring heartbeated leases. Readonly runs may share a workspace; every
write-capable run is exclusive, and a blocked workspace does not starve runnable work in
another. Recovery returns never-started claims to `pending` and marks started work
`interrupted`. `internal/worker` drives the full leased → preparing → running → terminal
state path through the shared process-tree supervisor, writes ordered JSONL output chunks
outside SQLite with a SHA-256 digest, and is verified with a fake harness for success,
failure, long-running heartbeats, workspace writes, and cancellation. Studio can now start
that runtime behind the explicit `--fake-harness` development flag. A committed manual run
wakes the worker, unsupported harnesses remain pending, startup drains recoverable work, and
the run-detail UI reads durable state and logs and can cancel pending or active work. Log API
responses expose opaque URLs and metadata rather than host paths. Run state transitions and
output pointers now enter a monotonic SQLite event journal; SSE uses those sequence IDs for
`Last-Event-ID` replay after a disconnect or Studio restart, while a slow UI poll remains a
safety net. Raw output stays outside SQLite. Studio now supports explicitly confirmed,
session-only ad hoc folders without persisting their absolute paths. Every attempt stores a
bounded before/after summary of added, modified, and deleted relative paths; incomplete scans
say so. Adapter-requested retries now close the failed attempt, release its lease, and create
a durable timer atomically. Due timers survive restart and create a fresh attempt; Studio
sleeps until the next due retry instead of relying on polling. Retries require readonly
authority unless the adapter explicitly marks them safe for a write-capable run, and a
scheduled retry remains cancellable. The fake harness covers fail-once/succeed-on-retry and
descendant-spawning scenarios, completing M3.

M4 approval spine (completed 2026-09-26): adapters may request a typed `filesystem.write` directory
scope or `command.execute` executable/subcommand prefix. The worker durably pauses the run,
heartbeats its lease while awaiting a decision, and resumes only after a CSRF-protected
Studio approval. Denial and shutdown cancellation conclude before process launch. Grants
are audit-recorded, exact-scope, and reusable only within the same run; readonly authority
cannot be elevated. The fake harness exercises both scope shapes. Studio now starts real
Claude Code and GitHub Copilot workers for readonly runs. Each fresh invocation receives the
selected agent instructions, referenced skill text, shared team memory, private agent memory,
and user request even when the chosen workspace is a different folder. Copilot CLI `1.0.88`
has a recorded, redacted authenticated JSONL fixture and a live-verified allowlist containing
only `view`, `rg`, and `glob`; its provider request material is removed before persistence.
Claude Code `2.1.282` is authenticated and live-verified in readonly and readwrite modes.
Write-capable runs use a run-local MCP permission server; Copilot CLI `1.0.88` uses a
run-local `permissionRequest` plugin. Both pause durably for bounded Studio decisions,
reuse only covered grants within the same run, remove their temporary configuration on
every exit path, and keep bearer credentials out of argv and the harness environment.
Authenticated conformance created one approved file through each harness. Vendor output is
redacted before persistence and mapped to a portable started/text/tool-requested/
tool-completed/completed/diagnostic event contract. The remaining vertical-slice steps
(structured conclusions and shared-memory proposals) belong to M5.

M5 context and effectiveness slice (completed 2026-09-26): every harness prompt now requires
a strict portable conclusion envelope, and the worker extracts the last complete envelope
without making an otherwise successful run fail when it is missing. Structured conclusions
and private run memory are durable and immutable. Correlated fresh runs receive bounded,
deterministically selected prior conclusions, private memory, file/action summaries, and
generic outcomes; older detail is replaced by an explicit compacted-history marker. Generic
work items and outcomes drive chronological event/run/outcome timelines without GitHub or
Jira branches in the runtime. Suggested durable facts become pending agent-memory Markdown
patches with a rendered diff and base SHA-256; Studio is the only approval path, applies an
atomic same-directory replacement, audits the decision, and reports external edits as a
conflict. Studio exposes conclusions, memory proposals, outcome ingestion, correlated
timelines, and monitor state. Built-in software-delivery monitors cover PR opened, checks
passed, PR merged, Jira transitioned, and human correction rate; operational monitors cover
queue delay, retries, run duration, and source freshness. Their windows and latest-outcome
semantics are deterministic and persisted for inspection.

## Success definition

The first runtime release is successful when one developer can define an agent team with
shared skills/tools, start Studio, manually run an agent through Claude Code or Copilot in
an arbitrary folder, approve bounded actions, watch live progress, inspect durable history,
approve a memory proposal, and then route scheduled, Jira, and GitHub events to exactly one
agent with useful outcome monitoring.

## Decisions

| # | Decision |
|---|---|
| D1 | One Go executable owns the complete local runtime and embeds the web UI. |
| D2 | `agentworks studio` runs all services in the foreground and binds only to loopback. |
| D3 | SQLite/WAL is authoritative local state; channels are wakeups only. |
| D4 | One agent invocation is the durable work unit; no general workflow engine. |
| D5 | Project YAML/Markdown is canonical; operational state stays outside the project. |
| D6 | Locally authored and installed dependencies appear in one catalog but have separate storage/ownership. |
| D7 | Absolute workspace paths live in gitignored local bindings; events name aliases only. |
| D8 | Permission levels are `readonly`, `readwrite`, `collaborate`, and `autonomous`. |
| D9 | Agents set a permission ceiling; harness and route policy can only reduce it. |
| D10 | Initial grants approve an executable/subcommand prefix or directory for one run only. |
| D11 | One write-capable run per workspace; locks are advisory outside AgentWorks. |
| D12 | Claude Code and Copilot CLI are the initial headless harness adapters. |
| D13 | Host and Docker execution are supported first; managed runtimes are deferred. |
| D14 | Every run is fresh and receives bounded, summarized prior context. |
| D15 | Shared Markdown memory changes require diff approval. |
| D16 | Structured routes select one highest-priority match; ties remain unrouted. |
| D17 | Runtime storage is capped at 1 GB with summary-preserving pruning. |

## Dependency map

```text
M0 fixtures/guardrails
  |
  +--> M1 format/resolver ----+
  |                           |
  +--> M2 Studio/storage -----+--> M3 manual runtime --> M4 harness/approval
                                                          |
                                      +-------------------+------------------+
                                      |                                      |
                                      v                                      v
                              M5 context/monitoring                  M6 events/routing
                                      |                                      |
                                      +-------------------+------------------+
                                                          v
                                                M7 Jira/GitHub sources
                                                          |
                                                          v
                                                M8 packaging/platform
                                                          |
                                                          v
                                                M9 cutover/release
```

M1 and M2 can proceed in parallel after M0. M5 and M6 can proceed in parallel after the
manual harness vertical slice works.

## M0: Architecture fixtures and guardrails

Purpose: remove vendor and platform uncertainty before building core abstractions.

### Work

- Check in this specification and maintain a short ADR index for decisions that change it.
- Capture authenticated Claude Code headless output for text, JSON/streaming, success,
  failure, cancellation, and permission requests delegated through an MCP permission tool.
- Capture authenticated Copilot CLI programmatic output and `permissionRequest` hook inputs
  and decisions for reads, writes, commands, URLs, and MCP calls.
- Record fixtures with CLI version and date; redact credentials and machine paths.
- Prototype whole-process-tree termination on Unix process groups and Windows Job Objects.
- Benchmark candidate pure-Go SQLite drivers for migrations, WAL, concurrent UI readers,
  worker claims, busy handling, and abrupt restart.
- Prototype an embedded frontend and a synthetic live-log SSE stream.
- Decide runtime data/config/cache directories through platform-native conventions.
- Add feature gates so incomplete format-2/Studio code cannot alter format-1 behavior.

### Tests

- Fixture parsers run offline in normal CI.
- Process-tree tests launch a child that launches a grandchild and prove both terminate.
- SQLite crash/reopen test proves committed work survives and incomplete transactions do not.
- Browser/API smoke test loads embedded assets and receives ordered SSE messages.

### Exit criteria

- Both harness approval paths are demonstrated with recorded payloads.
- SQLite and frontend choices are documented.
- Process cancellation is proven on every supported OS in CI.
- No production runtime API is designed around guessed vendor output.

## M1: Format 2 and resolved agent teams

Purpose: make the agent team and its selected dependency graph the product's canonical model.

### Work

- Add typed format-2 definitions for projects, teams, agents, tools, sources, routes,
  monitors, workspaces, and runtime settings under `internal/spec`.
- Use strict YAML decoding with useful file/line/field errors.
- Load native `SKILL.md` directories directly instead of converting authored skills to a
  parallel private shape.
- Implement the combined catalog of local and installed skills/tools.
- Move imported dependencies toward manifest declaration, lock resolution, and
  `.agentworks/deps` materialization; reuse current source fetch, staging, security, and
  content hashing.
- Implement typed agent edges: `skills`, `tools`, and `delegates`.
- Resolve one selected team into an immutable plan with content digests and selected
  execution variants.
- Validate missing references, cycles, duplicate identities, permission values, and team
  membership.
- Add machine-local workspace bindings and canonical path resolution.
- Add `agentworks plan <team|agent> --json` to expose the resolved plan for tests and UI.
- Write a one-way development migration for the starter project and dogfood fixtures.

### Tests

- Golden parse/render tests for every new definition.
- Dependency graph, cycle, collision, and closure tests.
- Identical plans produce identical digests independent of filesystem enumeration order.
- Installed content cannot shadow a local package silently.
- Absolute paths cannot enter committed definitions through Studio/spec save paths.
- Existing importer security fixtures remain green.

### Exit criteria

- A fixture project defines two agents sharing at least five skills and one tool.
- The resolved plan stores each shared dependency once and records per-agent visibility.
- A missing or ambiguous dependency fails with an actionable error.
- The plan can be produced without any vendor renderer or runtime.

## M2: Studio and durable local state

Purpose: establish the one-command local application and operational source of truth.

### Work

- Add `agentworks studio` with loopback binding, automatic port selection, and optional
  browser opening.
- Embed the compiled UI in the Go binary.
- Implement SQLite migrations and repository/transaction interfaces.
- Add tables for events, source cursors, runs, attempts, leases, timers, approvals,
  outcomes, monitor states, and work items.
- Implement the definition service that reads/writes project YAML and Markdown atomically.
- Add filesystem watching with auto-reload and unsaved-edit conflict handling.
- Implement REST endpoints for catalog/team/agent/workspace reads and edits.
- Implement SSE infrastructure with sequence IDs and reconnect support.
- Build initial screens: project readiness, team catalog, manual-run form shell, run list,
  and runtime storage usage.
- Bind state-changing requests to a random local session and protect against CSRF.

### Tests

- Repository contract suite against a temporary SQLite database.
- Migration forward-only and interrupted-migration tests.
- HTTP tests for loopback defaults and state-changing request protection.
- Filesystem conflict tests: clean reload versus unsaved conflict.
- SSE reconnect test does not miss persisted state.
- Cross-platform data-directory tests.

### Exit criteria

- `agentworks studio` opens a functioning UI from one binary.
- File edits made in Studio appear as ordinary project diffs.
- Restarting Studio preserves synthetic event/run records.
- No harness or model call is needed for the test suite.

## M3: Manual runs and process supervision

Purpose: deliver a complete runtime using a fake harness before adding vendor behavior.

### Work

- Implement the run state machine and transition validation.
- Implement durable pending work, claims, leases, heartbeats, interruption recovery, retry
  timers, cancellation, and idempotency keys.
- Implement per-workspace leases: one writer or multiple readonly runs.
- Add registered workspace selection and confirmed ad hoc folders.
- Build the cross-platform process supervisor and migrate reusable current process-group
  logic into it.
- Prefer structured command/args; make shell execution explicit and platform-specific.
- Persist chunked raw output outside SQLite and stream it through SSE.
- Add a fake harness executable/fixture that requests permissions, emits structured events,
  writes a file, succeeds, fails, hangs, and spawns descendants.
- Add run detail UI: state, live output, effective permissions, workspace, attempts,
  cancellation, and changed-file summary.
- Implement graceful Studio shutdown and forced process-tree termination after eight seconds.

### Tests

- State-machine property/table tests reject illegal transitions.
- Concurrent workers cannot both claim one run.
- Expired leases recover after simulated crashes.
- Duplicate manual requests with one idempotency key create one run.
- Two readonly runs share a workspace; a writer excludes every other writer.
- Cancellation kills descendants and preserves partial changes.
- Ad hoc folders cannot write before confirmation.

### Exit criteria

- The complete manual-run UX works with the fake harness.
- Live logs survive UI reconnects and Studio restart.
- A crash cannot lose a committed queued run.
- macOS, Linux, and Windows process tests pass.

## M4: Harness adapters and bounded approvals

Purpose: complete the agreed first vertical slice with real headless harnesses.

### Work

- Define the harness adapter and normalized harness-event contracts.
- Implement readiness states: missing, incompatible, unauthenticated, and ready.
- Implement Claude Code headless invocation, stream parsing, and ephemeral MCP permission
  broker integration.
- Implement Copilot CLI programmatic invocation, stream parsing, and ephemeral
  `permissionRequest` hook/config integration.
- Never write grants into the user's ordinary Claude or Copilot configuration.
- Implement portable permission ceilings and mapping to each harness's real controls.
- Add approval requests and decisions to the run UI.
- Implement run-only command grants matching canonical executable plus subcommand/argument
  prefix.
- Implement directory write grants with canonical path, symlink/junction, traversal, and
  case-normalization defenses.
- Treat compound shell commands as exact-command approvals.
- Cancel pending approvals and all active harnesses on Studio shutdown.
- Record a complete approval audit trail with secret redaction.

### Tests

- Offline fixture suites for both adapters and every normalized event type.
- Grant matcher tests across Unix and Windows path/command behavior.
- Deny always wins; a route can never raise an agent permission ceiling.
- Malformed/unknown harness permission requests fail closed.
- Temporary harness config is removed after success, failure, and cancellation.
- Opt-in authenticated conformance tests exercise one readonly and one readwrite run per
  harness without entering normal PR CI.

### Exit criteria

- The 14-step vertical-slice acceptance test in the architecture spec passes for Claude
  Code and Copilot CLI.
- A `git status` run grant approves later `git status ...` requests but not `git push`.
- A harness version change that breaks parsing fails a fixture/conformance check clearly.

## M5: Context, memory, outcomes, and monitors

Purpose: make fresh runs coherent and measure whether agents are effective.

### Work

- Implement work-item correlation and event/run/outcome timelines.
- Define and extract the structured run conclusion.
- Add context budgets and assembly from the current event, recent conclusions, outcomes,
  action/file summaries, selected transcript excerpts, and older compacted summaries.
- Store private run memory in runtime state.
- Load team and per-agent Markdown memory.
- Implement proposed shared-memory patches, rendered Markdown diffs, approval, atomic file
  writes, and audit records.
- Define generic outcome events and ingestion APIs.
- Implement monitor evaluation over runs, outcomes, source freshness, queue delay, retries,
  duration, and human correction.
- Ship software-delivery monitor templates for PR opened, checks passed, PR merged, Jira
  transitioned, and correction rate.
- Add timeline, memory proposal, outcome, and monitor screens.

### Tests

- Context selection is deterministic and never exceeds its configured budget.
- Old raw transcripts are replaced by summaries in assembled context.
- Memory changes cannot apply without approval.
- Concurrent external memory edits produce a conflict, not lost content.
- Generic outcome correlation works without Git or Jira-specific runtime branches.
- Monitor windows and state changes are deterministic against fixtures.

### Exit criteria

- A second fresh run receives the first run's conclusion and correlated outcomes.
- A proposed memory edit is visible as a diff and only lands after approval.
- A software-delivery monitor can become healthy/unhealthy from generic fixture outcomes.

## M6: Events, schedules, and deterministic routing

Purpose: move from manual invocation to durable, inspectable automation.

### Work

- Implement the CloudEvents-compatible internal envelope.
- Add durable ingestion, `(source, external ID)` deduplication, and raw/normalized payload
  storage with redaction.
- Implement structured route matching, priority selection, and one-winner semantics.
- Store no-match and tied-highest-priority events as `unrouted`.
- Add manual `route now`, replay, disable, and inspect actions.
- Implement manual HTTP/UI sources and scheduled events.
- Persist timers and catch up safely after Studio restarts or laptop sleep.
- Resolve route workspace aliases only through local bindings.
- Add event inbox and route editor/preview UI.
- Add route test fixtures so users can validate rules without executing an agent.

### Tests

- Duplicate deliveries create one event and at most one run.
- Tied routes never dispatch implicitly.
- Event payload fields cannot influence a filesystem path.
- Schedule catch-up honors idempotency and configured catch-up policy.
- Route previews and real dispatch use the same evaluator.
- Malformed route files fail closed while unrelated valid definitions remain visible.

### Exit criteria

- A manual event and a schedule each route to exactly one fake/real agent.
- An ambiguous event remains visible and can be routed manually.
- Restarting Studio neither loses nor duplicates a due scheduled run.

Implementation checkpoint (2026-09-26): format-2 `source.yaml` and `route.yaml`
definitions now feed a CloudEvents-compatible, recursively redacted event envelope. SQLite
deduplicates `(source, external ID)`, persists normalized and redacted source payloads, records
routing decisions in the replayable runtime journal, and creates at most one idempotent run.
The pure evaluator selects only a unique highest-priority match; ties and misses remain in the
Studio inbox for manual routing, replay, or re-evaluation. Operational route disablement is
durable without rewriting the checked-in definition. Routes resolve only a workspace alias,
which is bound to a host path by `agentworks.local.yaml`; payload data never participates in
path selection. Checked-in route fixtures use the same evaluator without persisting work.
Durable schedule timers support `all`, `latest`, and `skip` catch-up and retain their advanced
deadline across Studio restarts. Studio exposes ingestion, preview, inbox/detail, route state,
fixture results, and manual resolution through its loopback/CSRF-protected API and embedded UI.

## M7: Jira and GitHub sources

Purpose: deliver the motivating software-factory automation without making the runtime
software-engineering-specific.

### Work

- Define the connector interface, health model, cursor persistence, backoff, and rate-limit
  handling.
- Implement Jira polling for assignment/change events using environment-supplied credentials.
- Implement GitHub polling for PR comments, checks, PR state, and merge outcomes using
  environment-supplied credentials.
- Prefer polling locally; add verified webhook decoders only where they do not complicate
  the default setup.
- Normalize source-specific payloads into generic envelopes and outcomes.
- Correlate Jira issues and GitHub PRs into work-item timelines through configured keys.
- Add connector setup, test, pause, health, last-poll, cursor, and error UI.
- Add recorded API fixtures and simulated rate limits; normal tests never contact real
  services.

### Tests

- Cursor advancement and event insertion commit atomically.
- A failed page does not skip events or advance the cursor.
- Retries and repeated API pages deduplicate correctly.
- Credentials never enter errors, database records, logs, or browser payloads.
- External ticket/comment text remains untrusted context and cannot modify routing or paths.
- Opt-in live connector smoke tests are separate from normal CI.

### Exit criteria

- A simulated Jira assignment routes to the software-factory agent.
- A later simulated PR comment creates a fresh correlated run with relevant history.
- Checks, merge, and Jira transition outcomes update the same work-item timeline and monitors.

Implementation checkpoint (completed 2026-09-26): `internal/connectors` defines one
foreground polling contract for external sources, synchronizes checked-in definitions into
durable operational state, and commits each cursor, work-item update, outcome, and deduplicated
event in one optimistic SQLite transaction. Failed pages retain the prior cursor; exponential
backoff and provider rate-limit reset times survive restart. Jira enhanced-search and changelog
polling emits assignment events and transition outcomes. GitHub PR, issue-comment, and check-run
polling emits comment events plus opened, checks-passed/failed, and merged outcomes. A configured
work-item key expression correlates GitHub activity onto the Jira issue timeline without allowing
external text to select a route or workspace. Credentials are read from named environment
variables only, request errors omit bodies and URL queries, and cross-host redirects are refused.
Studio exposes connector setup through the canonical definition editor and adds health, cursor,
last/next poll, last error, test, poll-now, pause, and resume controls. Recorded Jira/GitHub
fixtures, simulated rate limits, atomicity, deduplication, recovery, end-to-end correlation, and
an opt-in live readiness smoke test cover the slice without network or model usage in normal CI.

## M8: Packaging, providers, and platform hardening

Purpose: make teams portable and runtime behavior predictable across machines.

### Work

- Add `agentworks pack` for a platform-independent team archive containing the resolved
  definition/dependency closure.
- Refactor vendor exporters to consume resolved plans rather than raw generic artifacts.
- Preserve static `export` as a delivery mode distinct from Studio runtime deployment.
- Implement the host provider with executable/version readiness checks.
- Implement the Docker provider with workspace-only mounts, read-only/read-write policy,
  explicit environment allowlists, no Docker socket, and visible network capability.
- Record selected provider/runtime/image digest in run plans and lock data.
- Implement storage accounting, 1 GB pruning, pinned runs, and manual deletion.
- Build signed/notarized release artifacts for supported OS/architecture pairs.
- Add native installer/uninstaller behavior and optional service installation only after
  foreground Studio is stable.

Implementation checkpoint (completed 2026-09-26): deterministic team packs capture and
verify the exact shared closure; pack installation probes the destination machine without
making the archive machine-specific. Resolved-plan exporters produce native Claude Code and
GitHub Copilot layouts while format-1 export remains unchanged. Host readiness distinguishes
missing and incompatible executables; Docker requires digest-pinned images and constructs a
workspace-only, permission-sensitive, environment-allowlisted boundary with explicit network
state and no socket mount. Actual ready provider/runtime/version/image choices are stored in
immutable run plans. Studio enforces the 1 GB default by pruning oldest unpinned raw detail
while preserving conclusions, outcomes, approvals, audits, monitors, and the run record.
Native user-level installers preserve project/runtime data on uninstall. Tagged release jobs
fail closed unless macOS and Windows signing credentials are available, notarize macOS, and
publish checksums plus provenance. Optional service registration remains deliberately gated
on the M9 multi-day foreground soak.

### Tests

- Pack content and digest are deterministic.
- Shared dependencies occur once in a team pack.
- Docker policy is verified on supported CI platforms where Docker is available.
- Host-provider readiness distinguishes missing and incompatible versions.
- Pruning removes raw detail in order but preserves summaries, outcomes, audits, monitors,
  and pinned runs.
- Static exporters retain their existing conformance coverage after plan conversion.

### Exit criteria

- The same logical team pack installs on two supported host platforms and resolves an
  appropriate provider or reports an actionable incompatibility.
- A readonly Docker run cannot write the workspace or access unrelated host directories.
- Crossing 1 GB prunes safely without losing the retained record.

## M9: Cutover, dogfooding, and release

Purpose: replace the old product center only after the new one proves itself.

Implementation checkpoint (2026-09-28): fresh installations can create a complete format-2
project with `init --runtime`, then resolve, pack, and open it in Studio. A checked-in research
team exercises a non-software, non-Git workspace model. The runtime threat model now covers the
required injection, approval, path, command, supply-chain, secret, container, and localhost
boundaries. Authenticated harness and Jira/GitHub connector jobs fail closed when credentials are
missing instead of silently passing. The compatibility cutover is recorded in ADR 0002, and the
multi-day foreground soak has an evidence checklist. The local security review closed its four
findings, including removal of unrelated host environment variables from harness processes;
the machine-readable release gate remains pending. RC diagnostics now retain redacted, durable
connector-poll attempts with cursor/count/routing evidence and provide a Studio inspection path;
recorded mock HTTP integration tests remain the ordinary-CI simulation surface. Remaining release gates require external
evidence: real Jira/GitHub software-factory dogfood, the completed multi-day soak, green configured
authenticated jobs, security finding review, and a tagged release candidate. Until those pass,
format-1 remains frozen compatibility tooling and service installation stays unavailable.

### Work

- Keep `examples/starter-project` as the format-1 compatibility fixture until ADR 0002's final
  switch. Use `examples/agent-team` for the format-2 shared-skill, Claude/Copilot manual-run,
  Jira-assignment, PR-comment, memory, and monitor scenario; migrate the starter name only when
  the external cutover gates pass.
- Update `README.md`, `AGENTS.md`, embedded authoring guidance, docs, schemas, and examples.
- Dogfood on at least one non-software workspace to prove that Git is optional.
- Dogfood the software-factory flow on real Jira/GitHub work with explicit safety review.
- Run a multi-day Studio soak including laptop sleep/wake, harness failures, source rate
  limits, full storage, and abrupt termination.
- Decide which format-1 commands remain as compatibility aliases and remove obsolete
  artifact-first/TUI paths.
- Publish security and threat-model documentation for event injection, approval spoofing,
  path escape, command matching, dependency supply chain, and secret redaction.
- Tag a release candidate, collect dogfood findings, then cut the runtime release.

### Exit criteria

- All first-release scenarios work from a fresh install using documented steps.
- The normal test suite requires no paid account or network access.
- Authenticated conformance jobs are green for both harnesses and both external connectors.
- No open critical/high security finding remains.
- The old artifact-first architecture is no longer needed for core Studio behavior.

## Cross-cutting engineering rules

- Business logic stays below `cmd/` and below HTTP handlers.
- Every persisted transition is transactional and independently testable.
- Database state, not an in-memory channel, is authoritative.
- Every external delivery and side effect has an idempotency strategy.
- Permission and path failures are fail-closed.
- Every vendor payload parser has recorded fixtures and version metadata.
- No normal unit/integration test calls a live model or SaaS API.
- Test the runtime with fake harnesses/connectors before real adapters.
- Preserve user-authored and user-modified files; never overwrite silently.
- Raise coverage floors as new packages mature; never lower them to land a phase.
- Keep this plan, the architecture specification, schemas, examples, and embedded guidance
  synchronized with implementation changes.

## Major risks

| Risk | Mitigation |
|---|---|
| Harness permission protocols change | Version-labelled fixtures, readiness constraints, scheduled authenticated conformance |
| Headless output is incomplete or unstable | Adapter-specific parsers behind normalized events; preserve redacted raw output for diagnosis |
| Approval matching grants more than intended | Structured argv, canonical executable/path resolution, prefix tests, deny precedence, no broad shell grants |
| External events inject instructions or paths | Treat data as untrusted context; routes own agent/workspace/policy; paths never come from payloads |
| SQLite lock contention harms UI/runtime | WAL, short transactions, busy handling, benchmark in M0, logs outside BLOBs |
| Crash leaves orphaned processes or runs | OS process-tree control, leases/heartbeats, startup reconciliation |
| Current checkout receives conflicting edits | One writer lease, advisory-lock warning, changed-file detection, preserve partial work |
| Runtime grows into a workflow engine | Keep agent invocation as the sole durable unit; child runs only for delegation |
| Package model duplicates existing standards | Native SKILL.md and MCP; own only dependency/composition metadata |
| Cross-platform promise is undermined by scripts | Explicit providers/variants and readiness resolution; no implicit shell assumption |
| Studio shutdown surprises users | Visible shutdown state, graceful cancellation, retained partial changes and conclusions |
| Scope overwhelms the existing CLI | Vertical milestones and feature gates; cut over only after real end-to-end value exists |

## Explicitly deferred

- Direct calls to model APIs.
- Multi-user authentication and RBAC.
- Remote browser access.
- Remote worker fleets and PostgreSQL implementation.
- Cloud event relay/tunnel service.
- Managed Node/Python runtimes.
- Automatic shared-memory mutation.
- Persistent cross-run permission grants.
- Arbitrary expression routing language.
- Arbitrary workflow/DAG authoring.
- Full workspace rollback.
- Exactly-once external actions.
- Container engines other than a Docker-compatible first implementation.
- Automatic agent instruction/skill/dependency evolution.

## First build sequence

The shortest path to validated value is:

1. M0 fixture spikes.
2. Minimal M1 team/agent loader and resolver.
3. Minimal M2 Studio shell and SQLite run list.
4. M3 fake-harness manual execution and live logs.
5. M4 Claude bounded approvals.
6. M4 Copilot bounded approvals.
7. M5 structured conclusions and memory proposal.

Do not begin Jira/GitHub connector work until this sequence works end to end. A connector
cannot rescue an unreliable local execution and approval loop.
