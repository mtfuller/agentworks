# AgentWorks runtime rearchitecture

Status: accepted direction; implementation in progress.

This document defines the intended product and technical architecture for turning
AgentWorks from an artifact-format and export tool into a local-first system for
composing, running, observing, and evolving teams of agents. `PLAN-2.0.md` describes
how to reach this architecture incrementally.

## 1. Product statement

AgentWorks is a local agent-team studio.

Users define a team of agents, give each agent selected skills and tools from a shared
catalog, route events to exactly one agent, and run that agent through an installed AI
harness. AgentWorks owns composition, packaging, event delivery, permissions, durable
run state, memory, outcomes, and monitoring. The harness owns the model's reasoning and
tool-use loop.

The primary v2 experience is:

```sh
agentworks studio
```

This starts one foreground, localhost-only application containing the web UI, HTTP API,
scheduler, event sources, durable queue, local worker, approval broker, and monitor
engine. Agents operate only while Studio is running. An optional service installation
may come later, but is not required for the first runtime release.

## 2. Confirmed product decisions

| Area | Decision |
|---|---|
| Initial user | One developer on one machine |
| Availability | Runs and event sources operate while Studio is open |
| Interface | Localhost-only web application |
| Harnesses | Claude Code and GitHub Copilot CLI first |
| Harness mode | Headless CLI subprocesses; no direct model API calls |
| Deployable unit | An agent team with a shared skill/tool catalog |
| Working directory | A configured or confirmed ad hoc local folder; Git is optional |
| Concurrency | At most one write-capable run per workspace; concurrent readonly runs are allowed |
| Permissions | `readonly`, `readwrite`, `collaborate`, `autonomous` |
| Initial approval posture | Consequential actions require approval; grants are bounded and run-only |
| Configuration source | YAML and Markdown files edited directly by Studio |
| Runtime state | SQLite locally |
| Execution providers | Host commands and Docker in the first release |
| Secrets | Environment variables only in the first release |
| Run continuation | New harness run with selected prior context, never vendor-session resume |
| Memory | Private run memory plus approved shared Markdown memory |
| Event routing | Structured rules select exactly one agent; ambiguity remains unrouted |
| Outcomes | Generic outcome events; software-engineering templates initially |
| Shutdown | Gracefully cancel all runs, wait approximately eight seconds, then kill process trees |
| Retention | Maximum 1 GB of runtime data, pruning oldest unpinned detail first |

## 3. Scope boundaries

### AgentWorks owns

- Agent, team, skill, tool, event-source, route, monitor, and workspace definitions.
- Resolution and locking of third-party packages.
- Materializing an agent's selected dependency closure.
- Invoking and supervising headless harness processes.
- Permission policy and human approval.
- Durable event, run, attempt, timer, and outcome state.
- Context assembly, run summaries, and long-term memory proposals.
- Local observability and effectiveness monitoring.
- Static vendor export as a separate delivery mode.

### AgentWorks does not own

- The model's internal reasoning or tool-use loop.
- Direct model API calls in the first runtime release.
- Arbitrary workflow DAGs or a general workflow language.
- Deterministic replay of model execution.
- Exactly-once external side effects.
- Container orchestration or a container runtime.
- Distributed consensus or multi-region scheduling.

The durable unit is one agent invocation. Delegation is either handled inside the
harness or represented as a child AgentWorks run with a parent run ID.

## 4. Standards strategy

AgentWorks defines composition and operations, not replacements for existing standards.

- Skills use the native Agent Skills `SKILL.md` directory shape.
- Agent-callable tools use MCP where possible.
- Repository guidance exported for broad harness compatibility uses `AGENTS.md`.
- External events use a CloudEvents-compatible envelope.
- Docker-backed execution uses pinned OCI image references.
- Vendor-specific files remain renderer output, never the canonical model.

`AGENTS.md` is generated guidance, not the typed source of truth for an agent team.

## 5. Source project layout

A format-2 project uses plain files:

```text
project/
├── agentworks.yaml
├── agentworks.local.yaml       # machine bindings; gitignored
├── agentworks.lock             # resolved third-party dependencies
├── teams/
│   └── engineering/team.yaml
├── agents/
│   ├── software-factory/AGENT.md
│   └── researcher/AGENT.md
├── skills/                     # locally authored skills
│   └── code-review/SKILL.md
├── tools/
│   └── jira/tool.yaml
├── sources/
│   ├── jira-main/source.yaml
│   └── github-main/source.yaml
├── routes/
│   ├── jira-assigned/route.yaml
│   └── pr-comment/route.yaml
├── monitors/
│   └── software-delivery/monitor.yaml
├── memory/
│   ├── team.md
│   └── agents/software-factory.md
└── .agentworks/
    └── deps/                   # materialized, gitignored third-party packages
```

The final names are part of the format-2 implementation spike, but these ownership
boundaries are normative:

- Authored source is readable and editable in the project.
- Imported dependencies are declared, locked, and materialized separately.
- Machine-local absolute paths and other host bindings are not committed.
- Runtime state and logs do not live beside authored definitions.

Project initialization adds `agentworks.local.yaml` and `.agentworks/deps/` to
`.gitignore`. Durable runtime state, logs, workspaces, and artifacts live in the
platform-native AgentWorks application-data directory, not under this project-local
`.agentworks/deps/` package materialization directory.

### 5.1 Project manifest

Illustrative shape:

```yaml
format: 2
name: engineering-agents

teams:
  - engineering

workspaces:
  - agentworks
  - customer-portal

dependencies:
  skills:
    obra/code-review:
      source: github:obra/agent-skills
      path: skills/code-review
      ref: v1.4.0
  tools: {}

runtime:
  storage_limit: 1GB
  shutdown_grace: 8s
```

Third-party source declarations are human intent. `agentworks.lock` records the resolved
commit/version, content digest, provenance, and selected runtime variants.

### 5.2 Machine-local workspace bindings

Committed definitions contain portable workspace names. Absolute paths live in the
gitignored `agentworks.local.yaml`:

```yaml
workspaces:
  agentworks:
    path: /Users/example/Development/agentworks
  customer-portal:
    path: /Users/example/Development/customer-portal
```

Routes refer only to workspace aliases. An external event can never provide a raw path.
Studio may accept an ad hoc folder for a manual run, but it must confirm the folder before
the first write-capable run in that Studio session.

Implementation checkpoint (2026-09-25): Studio's confirmation endpoint canonicalizes an ad
hoc directory into a random process-lifetime alias. Only that alias enters events and runs;
the absolute path remains in memory and disappears when Studio exits. The worker captures
bounded pre/post snapshots and reports only sorted relative paths. Generated directories are
skipped, symlinks are not followed, and a scan that exceeds its bounds is marked incomplete.

### 5.3 Team definition

```yaml
name: engineering
description: Agents that take engineering work from request to delivery.
agents:
  - software-factory
  - researcher
default_agent: software-factory
memory:
  team: memory/team.md
```

A team is the main packaging, browsing, and deployment boundary. Skills and tools are
stored once in the catalog. Each agent receives only its declared capabilities.

### 5.4 Agent definition

`AGENT.md` contains typed YAML frontmatter followed by its instructions:

```yaml
---
name: software-factory
description: Implements assigned work and takes it through review.
skills:
  - ticket-planning
  - code-review
  - obra/code-search
tools:
  - jira
  - github
delegates:
  - researcher
max_permission: collaborate
memory: memory/agents/software-factory.md
---

# Software factory

Follow the acceptance criteria and leave the workspace in a tested state.
```

`skills`, `tools`, and `delegates` replace the ambiguous use of a generic `requires:`
field for agent composition. Internally they are edges in one typed dependency graph.

### 5.5 Tool definition

MCP remains the first tool protocol. A tool definition describes how to reach it and may
offer more than one execution variant:

```yaml
name: jira
description: Read and update Jira work items.
transport: stdio
runtime:
  variants:
    - provider: host
      command: node
      args: [dist/server.js]
      requires: [node>=22]
    - provider: container
      image: ghcr.io/example/jira-mcp@sha256:...
      command: [/app/server]
auth: [JIRA_TOKEN]
```

### 5.6 Route definition

Routes use structured matching in the first runtime release:

```yaml
name: platform-jira
priority: 100
when:
  source: jira-main
  type: issue.assigned
  fields:
    project: PLATFORM
invoke:
  team: engineering
  agent: software-factory
  workspace: agentworks
  permission: readwrite
```

Expressions may be added later without replacing the structured form.

### 5.7 Monitor definition

Monitors evaluate AgentWorks domain outcomes rather than generic process health alone:

```yaml
name: software-delivery
for:
  agent: software-factory
window:
  runs: 20
assert:
  success_rate: ">= 0.8"
  pr_created_rate: ">= 0.7"
  human_correction_rate: "< 0.2"
on_failure:
  severity: warning
```

Initial software-engineering outcome templates cover PR opened, checks passed, PR merged,
Jira transitioned, and human correction. The runtime itself stores generic outcome events.

## 6. Package catalog and dependency resolution

Studio presents locally authored and installed components in one catalog, but preserves
their different ownership:

```text
Catalog
├── Local skills       editable under skills/
├── Installed skills   read-only under .agentworks/deps/
├── Local tools
└── Installed tools
```

Resolution rules:

1. Parse the selected team and its agents.
2. Resolve every agent's skills, tools, and delegates.
3. Validate missing references and cycles.
4. Resolve third-party sources from the manifest and lockfile.
5. Select a compatible execution variant for the current worker.
6. Produce an immutable resolved team plan with content digests.
7. Materialize only that closure into a run workspace or exported pack.

The existing importer, content hashing, security lint, and lockfile provenance are reused,
but import output moves from editable source directories to the dependency store.

## 7. Runtime topology

The single binary contains separable components:

```text
Browser
   │ HTTP + SSE
   ▼
Studio API
├── definition service ───── project files
├── event service ────────── SQLite
├── router
├── scheduler
├── approval broker
├── monitor engine
├── context assembler
└── worker gateway
        │
        ▼
Local worker
├── workspace manager
├── execution provider
├── harness adapter
└── process supervisor
```

`agentworks studio` composes every component in one process. Internal package boundaries
must not assume this forever. A later team deployment can run `agentworks server` and
`agentworks worker` separately against PostgreSQL and an authenticated worker gateway.

Implementation checkpoint (2026-09-24): the feature-gated Studio shell binds to loopback,
serves embedded assets, publishes a synthetic SSE readiness event, and handles graceful
shutdown. It opens a migrated per-project SQLite/WAL database in the platform-native user
data directory, reports storage health, and exposes its durable run list. Manual runs and
definition edits are protected by an in-memory Studio session, SameSite cookie, separate
CSRF token, and same-origin check. A manual request atomically persists its event and pending
run after resolving the live project definitions. The definition editor exposes only
canonical tracked YAML/Markdown files, validates changes, performs atomic replacements, and
uses content hashes to prevent an external edit from being overwritten. Machine-local
bindings and materialized dependencies are deliberately excluded.
With the explicit `--fake-harness` development flag, Studio also starts the local worker,
wakes it after a committed manual request, shows durable attempt output from an opaque log
URL, and cancels pending or active fake work. A monotonic SQLite event journal drives the UI
through SSE and replays from `Last-Event-ID`; reconnects and restarts therefore recover every
committed state/output notification. Claude Code and Copilot remain visible as planned
harness choices but are not claimed by this worker.

The web UI is compiled at release time and embedded in the Go executable. End users do not
need Node to run Studio.

## 8. Durable storage

SQLite in WAL mode is the local source of truth. Use a pure-Go driver so release builds do
not require CGO. Embedded SQL migrations are applied transactionally.

The accepted driver, version policy, connection settings, migration checksums, data-directory
layout, and embedded-frontend choice are recorded in
[ADR 0001](decisions/0001-local-storage-and-embedded-ui.md).

Minimum logical tables:

| Table | Purpose |
|---|---|
| `sources` | Configured source identity and health |
| `source_cursors` | Poll position and last successful poll |
| `events` | Normalized, deduplicated external and internal events |
| `subscriptions` | Loaded route identity and revision |
| `runs` | Durable agent invocation and current state |
| `run_attempts` | Retry-specific timing, worker, process, and result |
| `run_leases` | Write ownership and worker heartbeats |
| `runtime_events` | Monotonic replay journal for Studio state/output notifications |
| `timers` | Schedules, retries, and delayed work |
| `approvals` | Requests, decisions, grants, and audit data |
| `outcomes` | Generic results correlated to events/work items/runs |
| `monitor_states` | Current health, window, and last evaluation |
| `work_items` | Correlation timeline across Jira, PR, and later events |

Large raw logs and artifacts live in the runtime data directory, not unbounded SQLite BLOBs.
SQLite stores their metadata and content digests.

Database commits are authoritative. In-memory channels may wake workers or UI streams but
must never be the only record that work exists.

Small UI notifications live in `runtime_events` with a monotonic sequence. State-change
notifications are inserted in the same transaction as the authoritative run transition.
Output notifications contain only the run, attempt, stream, and chunk sequence; raw bytes
remain in the per-attempt JSONL log. SSE clients replay after `Last-Event-ID` and then fetch
the authoritative REST resource named by the notification.

## 9. Event ingestion and routing

Every source emits a CloudEvents-compatible envelope:

```yaml
id: jira-main:ABC-123:17
source: jira-main
type: issue.assigned
subject: ABC-123
time: 2026-09-24T14:30:00Z
data: {}
```

Ingestion and dispatch occur durably:

1. Verify and normalize the source payload.
2. Insert the event with a unique `(source, id)` constraint.
3. Evaluate enabled routes.
4. Select the sole highest-priority route.
5. If no route matches or the highest priority is tied, mark the event `unrouted`.
6. Otherwise create one idempotent run and correlate it to a work item.
7. Commit before waking a worker.

The Studio event inbox shows raw source data, normalized data, matches, routing decisions,
and correlated runs. Unrouted events have a manual `route now` action.

Initial sources are manual UI/API, schedules, Jira assignment, and GitHub PR comments.
Polling is preferred locally because it works behind NAT. Webhooks are supported when the
machine is reachable or a future relay exists.

Format-2 schedule and route definitions are standalone checked-in files. A route may carry
preview fixtures; `expect: unrouted` asserts that no unique winner exists. Fixtures call the
same evaluator as ingestion but never persist an event or create a run:

```yaml
# sources/weekly-review/source.yaml
name: weekly-review
kind: schedule
schedule:
  every: 168h
  start_at: 2035-01-01T09:00:00Z
  catch_up: latest # all, latest, or skip
event:
  type: review.requested
  data: {scope: weekly}
```

```yaml
# routes/weekly-review/route.yaml
name: weekly-review
priority: 50
when:
  source: weekly-review
  type: review.requested
invoke:
  team: engineering
  agent: reviewer
  workspace: product # alias only; resolved through agentworks.local.yaml
  harness: github-copilot
  permission: readonly
tests:
  - name: scheduled review
    event:
      source: weekly-review
      type: review.requested
      data: {scope: weekly}
    expect: weekly-review
```

Implementation checkpoint (2026-09-26): `internal/router` owns normalization, recursive
secret-key redaction, fixture preview, subscription state, one-winner evaluation, replay, and
manual resolution. `internal/scheduler` syncs scheduled sources into durable timers and emits
stable occurrence IDs, so catch-up and repeated processing deduplicate at ingestion. Both raw
and normalized payload columns contain redacted JSON; source credentials are never retained as
an observability convenience. Studio runs both components in-process and in the foreground.

External connectors use the same durable ingestion boundary. A Jira or GitHub poll returns a
candidate cursor plus normalized events, work-item updates, and generic outcomes; SQLite accepts
all of them atomically only when the source's previously observed cursor still matches. A failed
request or stale poll cannot advance the cursor. Source definitions name credential environment
variables but never contain their values. Operational pause, health, backoff, rate-limit reset,
and last/next-poll state remain local runtime records so editing or exporting the project cannot
leak machine state. Connector payload text is untrusted data: only checked-in route definitions
may select an agent, harness, permission level, or workspace alias.

Implementation checkpoint (2026-09-26): `internal/connectors` implements restart-safe Jira and
GitHub polling with recorded offline fixtures. Jira assignment changes become routable events and
Jira transitions become outcomes. GitHub PR comments become routable events; PR-opened,
checks-passed/failed, and merged states become outcomes. A checked-in work-item-key expression
correlates a PR with its Jira issue so subsequent runs receive the same bounded timeline. Studio's
source panel can test, poll, pause, and resume each connector and shows health, cursor, poll times,
rate-limit reset, and sanitized errors. Optional live smoke tests verify credentials and endpoint
readiness without joining the normal test suite.

## 10. Scheduler, runs, and recovery

Run states:

```text
pending -> leased -> preparing -> running -> succeeded
                         |          |  |
                         |          |  +-> waiting-for-approval -> running
                         |          +----> retry-scheduled -> pending
                         +---------------> failed / cancelled / interrupted
```

Workers claim runs with time-limited leases and heartbeat while active. On restart:

- Expired leased work returns to `pending`.
- Expired running work becomes `interrupted`.
- Retry policy determines whether a new attempt is safe.
- Externally visible actions require idempotency evidence or human review before retry.

Execution is at least once. Connectors, routes, runs, and external-action adapters must all
use stable idempotency keys.

Only one write-capable run may hold a workspace lease. Readonly runs may overlap. The lock
is advisory to processes outside AgentWorks; Studio warns prominently that external edits
can still conflict.

Implementation checkpoint (2026-09-25): durable claims create an attempt and expiring
workspace lease transactionally. The worker heartbeats while its supervised process runs,
stores ordered output as per-attempt JSONL outside SQLite, and commits the terminal attempt,
run, and lease release together. Startup recovery returns a claimed-but-unstarted run to
`pending`; a process that had started becomes `interrupted`. Studio starts this worker only
under `--fake-harness`; the worker declares the harnesses it can execute so it cannot claim
and fail queued work intended for a future Claude Code or Copilot adapter. In-memory wakeups
reduce latency, while the committed SQLite queue remains authoritative and is drained again
at startup and on a safety interval. Adapter-requested retries atomically finish the current
attempt and persist a timer; the runtime sleeps until the earliest due timer, then returns the
run to pending for a fresh attempt. Write-capable retry requires an adapter's explicit safety
assertion, and scheduled retries remain cancellable.

## 11. Permission model and approval broker

Portable permission levels:

| Level | Intended capability |
|---|---|
| `readonly` | Read the workspace and inspect configured context |
| `readwrite` | Edit the selected workspace and execute locally approved commands |
| `collaborate` | Also perform approved external collaboration actions such as PRs or ticket updates |
| `autonomous` | Execute actions covered by policy without individual prompts |

An agent declares its maximum. Harness defaults and routes may reduce but never increase it:

```text
effective permission = minimum(agent maximum, harness default, route permission)
```

The adapter maps the portable level to real harness controls and fails closed when the
harness cannot enforce a requested restriction.

### 11.1 Harness integration

- Claude Code headless mode delegates permission requests to an ephemeral AgentWorks MCP
  permission tool.
- Copilot CLI uses an ephemeral per-run `permissionRequest` hook/configuration.
- Neither integration persists grants into the user's normal harness configuration.

The adapter boundary hides the vendor protocol:

```go
type ApprovalRequest struct {
    Kind      Capability
    Command   []string
    Path      string
    Tool      string
    URL       string
    Detail    json.RawMessage
}
```

### 11.2 Bounded grants

Initial grants are run-only. For commands, the user approves an executable plus subcommand/
argument prefix for the run:

```yaml
kind: command
executable: /usr/bin/git
arguments_prefix: [status]
workspace: agentworks
expires: end-of-run
```

For file edits, the user may approve writes recursively within one selected folder for the
run. Paths are canonicalized before matching. Directory grants reject `..`, symlink,
junction, and case-normalization escapes. Structured command/args execution is the default.
Compound shell expressions receive exact-command approval and never a broad executable
grant.

Implementation checkpoint (2026-09-25): the durable approval broker supports run-only
`filesystem.write` scopes constrained to portable workspace-relative directories and
`command.execute` scopes constrained to an executable name plus non-empty argument prefix.
Requests pause the run transactionally, decisions are audited and streamed through the
runtime event journal, and the worker heartbeats while waiting. An identical approved scope
is reused only by the same run. The fake adapter exercises this path.

Implementation checkpoint (2026-09-25): Studio now owns real readonly harness execution.
The invocation context is assembled from the selected format-2 agent, its referenced skills,
team memory, private agent memory, and the event prompt, then run in the event-selected folder.
Copilot CLI `1.0.88` has been authenticated and live-verified with an explicit read-tool
allowlist; its JSONL is scrubbed of opaque provider request material before persistence.
Claude Code `2.1.282` is also authenticated and live-verified with only `Glob`, `Grep`, and
`Read`, `dontAsk` mode, and no MCP servers; its stream is scrubbed of host paths, session
identifiers, and user-configuration inventories.

Implementation checkpoint (2026-09-26): write-capable Claude Code runs delegate permission
prompts to an ephemeral AgentWorks MCP server, while Copilot CLI runs load an ephemeral
`permissionRequest` plugin. A loopback gateway authenticates each run with a random bearer
credential and maps vendor requests to durable directory-write or executable/subcommand
grants. Credentials expire with the run; temporary files are removed after success, failure,
and cancellation. Authenticated live runs for both vendors paused in Studio, resumed after a
bounded approval, created only the requested file, and recorded the workspace change.
Redacted vendor records are also mapped to the portable harness-event contract.

Shared-memory writes are normal Markdown diffs and require approval.

## 12. Process supervision

All subprocess consumers use one cross-platform supervisor: harnesses, builds, tests,
evals, connectors, MCP servers, and host execution providers.

Required behavior:

- Structured executable and argument arrays by default.
- Explicit environment construction and secret redaction.
- Working-directory confinement.
- Incremental stdout/stderr capture and structured events where available.
- Context cancellation and deadlines.
- Whole-process-tree termination.
- Unix process groups/signals.
- Windows Job Objects.
- Graceful termination followed by forced termination after the configured grace period.

POSIX `sh` is no longer a platform-wide assumption. Shell execution is explicit and may
provide separate Unix and Windows forms.

## 13. Execution providers

The runtime spine is portable; an artifact's executable dependencies may not be. Providers
make the difference explicit.

First runtime release:

- `host`: resolve an executable on the host and validate requirements.
- `container`: run a pinned image through Docker.
- `remote`: use a remote MCP endpoint where already supported.

Later:

- `managed`: download a verified, private Node or Python runtime without modifying the
  user's global environment.
- `builtin`: perform a capability directly inside AgentWorks.

For Docker:

- Mount only the selected workspace.
- Mount it read-only for `readonly`, read-write otherwise.
- Never mount the host Docker socket.
- Pass only declared environment variables.
- Do not mount unrelated host directories.
- Treat network access as an explicit visible capability; it is enabled by default in the
  first runtime release.

Variant selection is recorded in the resolved deployment/run plan for reproducibility.

## 14. Harness adapter contract

Harness adapters discover installations, validate authentication, construct invocations,
stream normalized events, broker permissions, and parse completion metadata.

Conceptual interface:

```go
type Harness interface {
    ID() string
    Probe(context.Context) ProbeResult
    Prepare(context.Context, RunPlan) (Invocation, error)
    ParseOutput(context.Context, io.Reader, chan<- HarnessEvent) error
    Summarize(Result) RunConclusion
}
```

`Probe` must distinguish missing, installed-but-unauthenticated, ready, and incompatible
versions. Adapter conformance fixtures pin the vendor JSON/event shapes that AgentWorks
parses.

The first adapters are Claude Code and GitHub Copilot CLI.

## 15. Context assembly and memory

Every run is new. Context is built from:

- The current event and its work-item chain.
- The selected agent and team instructions.
- The resolved skill/tool catalog.
- Team and agent memory.
- Prior structured conclusions and outcomes.
- File-change and action summaries.
- Selected recent transcript excerpts.
- Compacted summaries of older history.

Every harness run is instructed to finish with a structured conclusion containing:

```yaml
completed: []
files_changed: []
commands_run: []
checks: []
external_actions: []
outstanding_work: []
suggested_memory: []
```

The adapter extracts this conclusion. If extraction fails, the run still finishes but is
marked as missing structured context. Older context is summarized before the configured
context budget is exceeded; raw history is not injected without a bound.

Private run memory stays in runtime state. Shared memory is Markdown at team and agent scope.
Agents may propose shared-memory patches, but Studio applies them only after showing an
approved diff.

## 16. Outcomes and work items

An outcome is a generic correlated fact:

```yaml
type: github.pr.opened
subject: github:org/repo:pull/123
work_item: jira:ABC-123
run: run-123
data: {}
```

Work items connect events and runs into a user-visible timeline:

```text
Jira assigned -> implementation run -> PR opened
    -> review comment -> response run -> checks passed -> PR merged
```

Software-engineering support is delivered as source, route, outcome, and monitor templates,
not hard-coded assumptions in the runtime.

## 17. Studio API and UI

The Go HTTP server binds to loopback by default and serves embedded frontend assets.

- REST/JSON handles definitions and commands.
- Server-Sent Events streams events, runs, logs, approvals, and monitor changes.
- WebSocket is reserved for future interactive terminal/session needs.
- A random local session secret and CSRF protection protect state-changing routes.
- Binding to a non-loopback interface is outside the first runtime release and must later
  require authentication.

Required first-release screens:

1. Team and agent catalog.
2. Manual run form.
3. Active and historical runs.
4. Run detail with live logs, actions, approvals, changes, and conclusion.
5. Event inbox and unrouted-event resolution.
6. Sources and routes.
7. Monitors and outcome timelines.
8. Runtime/harness readiness.
9. Memory proposal diffs.
10. Storage usage and retention.

Studio watches project files. It reloads an external change when the UI has no unsaved edit;
otherwise it presents a conflict instead of overwriting either side.

## 18. Shutdown, retention, and local security

Closing Studio stops new scheduling, cancels all pending approvals, gracefully cancels active
runs, waits the configured grace period (eight seconds by default), and kills remaining
process trees. Partial workspace changes are deliberately preserved and summarized.

At the 1 GB runtime-data limit, prune the oldest unpinned completed-run data in this order:

1. Raw streamed output.
2. Raw transcript details.
3. Temporary run artifacts.
4. Old event payload bodies.

Retain structured conclusions, outcomes, approval audit records, monitor results, memory
changes, pinned runs, and basic event/run metadata. Studio shows usage and supports pinning
and manual deletion.

Secrets come only from explicitly named environment variables in the first runtime release.
They are never written to project files, the database, logs, approval details, errors, or
generated bundles.

## 19. Static packaging remains supported

The runtime and static export are separate delivery modes:

- `agentworks pack` produces a portable agent-team archive with selected dependencies.
- `agentworks export` renders a team/agent plan into a vendor's static native format.
- `agentworks studio` runs and observes teams through local harnesses.

A static plugin cannot poll events, retain runtime state, or evolve memory on its own. Those
features require Studio or a later AgentWorks server.

Pack contents are platform-independent when possible. A later `--standalone --platform`
mode may include AgentWorks and selected runtime dependencies, but embedding a runtime in
every pack is not the default.

M8 implementation checkpoint (2026-09-26): `pack` writes a deterministic, integrity-checked
closure and installs it into an absent destination; `providers` reports host/Docker/remote
readiness; run resolution records the actual ready tool runtime in an immutable SQLite plan;
and `export --team` renders that plan for Claude Code or GitHub Copilot without replacing the
format-1 export path. Docker invocation construction enforces the workspace-only boundary,
permission-sensitive mount mode, digest-pinned image, explicit environment and network policy,
and no socket mount. Studio enforces the 1 GB default with pinned-run-aware raw-detail pruning.
Release installers remain foreground-only until the M9 soak justifies an optional service.

## 20. Evolution to teams

The first release is intentionally single-user, but boundaries must permit:

```text
agentworks server + PostgreSQL
        |
authenticated worker gateway
    +---+---+
 macOS   Linux/container workers
```

Workers advertise OS, architecture, execution providers, harnesses, runtime versions, and
labels. The scheduler matches requirements to a compatible worker. Remote workers never
connect directly to the database.

Team authentication, RBAC, centralized secrets, high availability, and remote web access
are not first-release features.

## 21. Internal package boundaries

Target shape:

```text
internal/spec            typed format-2 definitions
internal/catalog         local and installed package catalog
internal/resolver        typed dependency graph and immutable run/team plans
internal/packages        content-addressed dependency storage
internal/studio          HTTP server and embedded UI
internal/store           SQLite migrations, repositories, run state, and leases
internal/worker          execution worker
internal/process         cross-platform subprocess supervision
internal/workspace       bindings, canonicalization, and leases
internal/events          envelope, ingestion, deduplication, correlation
internal/connectors      manual, schedule, Jira, and GitHub sources
internal/router          structured rule evaluation
internal/approval        policies, requests, grants, and broker
internal/harness         adapter interfaces and normalized events
internal/harness/claude
internal/harness/copilot
internal/providers       host, container, and remote execution
internal/context         context budgets and run conclusions
internal/memory          Markdown memory and proposal diffs
internal/outcomes        outcome and work-item timelines
internal/monitors        effectiveness evaluation
internal/runlog          log persistence, streaming, and pruning
```

Existing packages are reused behind these boundaries where appropriate. Target exporters
should ultimately consume an immutable resolved plan rather than a raw generic artifact.

## 22. Compatibility and migration

This is a format-2 rearchitecture. There are no external users requiring a long compatibility
window, but the transition should remain testable and reviewable:

1. Implement typed v2 definitions alongside the current artifact loader.
2. Build Studio behind an experimental command while existing commands remain green.
3. Add a one-way migration command for the repository's examples and useful dogfood projects.
4. Switch examples and embedded authoring guidance only after the manual-run vertical slice
   works end to end.
5. Remove v1-only architecture after exporter/importer behavior has equivalent v2 coverage.

The migration helper is a development convenience, not a permanent v1 compatibility promise.

## 23. First vertical-slice acceptance test

The architecture is validated when a developer can:

1. Run `agentworks studio`.
2. Open the localhost UI.
3. Select an agent from a team.
4. Choose Claude Code or Copilot CLI.
5. Select a registered or confirmed ad hoc folder.
6. Start a headless run.
7. Watch stdout, stderr, and normalized harness events live.
8. Receive a permission request.
9. Approve an executable/subcommand for the remainder of that run.
10. See a subsequent matching request approved automatically.
11. Cancel or complete the run.
12. Inspect partial or completed workspace changes.
13. Restart Studio and read the durable structured conclusion.
14. Review and approve a proposed shared-memory Markdown diff.

## 24. Required spikes

Spikes produce fixtures and decisions, not production abstractions:

1. Capture current Claude Code headless stream output and permission-MCP requests.
2. Capture current Copilot CLI programmatic output and `permissionRequest` hook payloads.
3. Prove whole-process-tree cancellation on macOS, Linux, and Windows.
4. Evaluate a pure-Go SQLite driver under WAL, concurrent readers, migrations, and abrupt exit.
5. Prove Docker mount and environment policy on macOS, Linux, and Windows.
6. Prototype a compiled-and-embedded web UI with live SSE logs.
7. Measure context conclusion reliability from both harnesses.

Vendor-facing fixtures must be version-labelled because these interfaces will change.

## 25. Non-functional requirements

- A Studio restart must not lose accepted events, queued runs, decisions, or conclusions.
- A malformed route, definition, or local binding must fail closed with a file/field error.
- Event ingestion and run creation must be idempotent.
- No secret may appear in persistent logs or API responses.
- The browser must remain responsive while harnesses and connectors are busy.
- Runtime operations must work natively on supported macOS, Linux, and Windows builds.
- Every state transition and permission decision must be testable without a real model call.
- Vendor adapters require recorded-fixture tests plus opt-in authenticated conformance tests.
- Project files remain understandable and editable without Studio.
