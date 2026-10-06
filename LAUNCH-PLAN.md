# Plan: public launch of the AgentWorks runtime

This plan takes AgentWorks from its current state (format-2 runtime behind `--experimental`,
release gate pending) to a credible public technical preview. It comes from a release-readiness
review on 2026-10-05 against commit `03ffb23`. It builds on
[`PLAN-2.0.md`](PLAN-2.0.md) and [ADR 0002](docs/architecture/decisions/0002-runtime-cutover.md)
and doesn't replace them: M9's release gate still decides when a release can be tagged.

Effort figures are rough planning estimates for one developer working with a coding agent.

## Decisions

| # | Decision |
|---|---|
| L1 | The goal is a **credible technical preview**: fix the defects, tell one story, ship a real console, pass the M9 gate, then tag. |
| L2 | **The Studio runtime leads.** The artifact toolchain stays supported but moves out of the README into `docs/`. This matches ADR 0002. |
| L3 | **Positioning: a control layer for agent CLIs, for any team's work.** AgentWorks governs the Claude Code and Copilot CLIs people already pay for, and it isn't only for software teams. |
| L4 | **Studio becomes a modern single-page console app.** The current single page is replaced, not restyled. |
| L5 | The runtime ships as **2.0**. The `v1.0.0-rc` compatibility promise covers format 1 only. |

## Pitch

Draft README opening:

> AgentWorks is a local control layer for the agent CLIs your team already pays for. Give a team
> of agents shared skills, route work to them, and approve every write or command before it
> happens, whether the work is code, research or operations.

Supporting points, each already partly true in the code:

- **Your rules, your subscriptions.** Agents run through Claude Code or Copilot CLI with no API
  keys. Permission ceilings, run-only approvals and an audit trail bound what they can do.
- **Beyond code.** Workspaces don't need Git, and routes, memory and approvals don't assume
  software. The `examples/research-team` project already works this way.
- **Reviewed memory.** Agents propose memory changes as diffs, and nothing lands without approval.
- **Private by default.** Studio binds to loopback, and code, documents and credentials never
  pass through a relay.

## Current state

What was verified by running the code (no live model calls were made):

- `go build`, `gofmt -l .` and `go vet ./...` are clean. The suite passes except one test (B3).
- `init --runtime` → `plan` → `studio --experimental` works on a fresh project.
- In Chromium, a fake-harness `readwrite` run paused for approval, wrote its file once approved,
  and recorded the change summary.
- Studio rejects a foreign `Host`, a missing CSRF token and a cross-origin POST, and sends a
  strict CSP.
- A request above the agent's ceiling is lowered to the ceiling. A readonly run that tries to
  write fails instead of writing. A repeated `request_id` creates one run.
- `SIGINT` and `SIGTERM` both shut Studio down with exit code 0.
- The format-1 flow works on a fresh scaffold: `new` → `validate --strict` → `test` →
  `export` to Claude Code, Cursor and Gemini CLI.

The engineering core is strong. What needs work is listed in the next two sections.

## Bugs to fix

### B1. Prompts are passed as one command-line argument (high)

Both harness adapters pass the assembled run context as `-p <prompt>`, but the context budget
is 2 MiB.

- **Where:** `internal/harness/claudecode/adapter.go:122` and `:185`,
  `internal/harness/githubcopilot/adapter.go:94` and `:204`, budget in
  `internal/harness/runcontext/context.go:19`.
- **Impact:** Linux rejects any single argument over 128 KiB (`E2BIG`, reproduced), and Windows
  caps the whole command line near 32K characters. Context grows with skills, memory and prior
  conclusions, so runs start failing at launch only after a team has been in real use, which is
  likely during the soak. The prompt, including ticket text and memory, is also visible in `ps`.
- **Fix:** send the prompt on stdin where each CLI supports it, otherwise in a `0600` temp file
  removed on every exit path. Add a test with a 300 KiB context.

### B2. Studio hands a session to any local client (high on shared machines)

`GET /api/v1/session` returns the session cookie and CSRF token with no check, and the read
endpoints (runs, logs, definitions, event payloads) need no session.

- **Where:** `internal/studio/session.go:40`. The threat model covers only processes under the
  same OS account (`docs/security/m9-local-review.md:33`).
- **Impact:** another OS user on a shared host can read run history, then queue and approve runs
  that execute under the Studio owner's Claude or Copilot login.
- **Fix:** print a one-time token in the startup URL (`http://127.0.0.1:PORT/?token=…`), exchange
  it once for the session cookie, and require the session on every `/api/v1` route, including
  GETs and the event stream. Add the case to `docs/security/runtime-threat-model.md`.

### B3. One test calls the real GitHub API (medium)

`TestFetchGitHubFallsBackToMaster` uses a real `GITHUB_TOKEN`, `GH_TOKEN` or `gh auth token` and
calls the live `api.github.com`.

- **Where:** `internal/importer/fetch_test.go:189`, token lookup in
  `internal/importer/fetch.go:198`.
- **Impact:** `go test ./...` fails for any contributor who is logged into `gh`, against the
  project's rule that normal tests never touch the network.
- **Fix:** in the test helper, `t.Setenv` both variables to empty and stub `ghAuthToken`. The
  test passes with the tokens unset.

### B4. Windows ships but its process tests never run (medium)

Windows CI only builds and vets (`.github/workflows/ci.yml:109`), so the Job Object process-tree
code is never exercised. `PLAN-2.0.md:331` lists Windows process tests passing as an M3 exit
criterion, and releases still ship a Windows archive and `install.ps1`.

- **Fix:** run `go test ./internal/process ./internal/workspace` on `windows-latest`. Those
  packages don't need `sh`. If they can't pass soon, stop shipping Windows.

### B5. Papercuts (low)

- `agentworks version` prints `Format: 1` while `init --runtime` writes format 2
  (`cmd/version.go`).
- A readonly run that tries to write fails with only `prepare approval request`. Say what
  happened and what to do instead.
- Studio prints nothing when it shuts down.
- The README's project structure (`README.md:484`) leaves out every runtime package.
- The GitHub repository description still describes the format-1 tool.

## Gaps

### G1. The release gate is still pending (blocker)

`docs/operations/m9-release-evidence.yaml` has every field empty, so the release workflow blocks
tags. PLAN-2.0 still lists M0 and M1 as in progress. Nobody has run the runtime for longer than a
session yet. Closing this needs the authenticated conformance jobs, real Jira and GitHub dogfood,
and the multi-day soak.

### G2. Two products, one blurred story (high)

The README is 545 lines and mostly about format 1. The `v1.0.0-rc` changelog promises format-1
stability, while ADR 0002 plans to remove the TUI and change `init` before stable.
`COMPATIBILITY.md` doesn't cover format 2, the Studio API or the database migrations.

### G3. Studio looks like a developer console (high)

Today Studio is one long page with no navigation, conclusions shown as raw JSON, and a textarea
for editing definitions. A control-layer product is judged by its approval and audit screens,
and non-engineering users need forms instead of YAML. See [Proposed Studio console](#proposed-studio-console).

### G4. Foreground only (medium)

Jira, GitHub and schedule routing work only while Studio is open. Service installation is
correctly deferred until the soak passes (`PLAN-2.0.md:521`).

### G5. Two harnesses, and no Codex anywhere (medium)

The runtime drives Claude Code and Copilot CLI. Codex CLI is missing from the runtime and from
the export targets.

### G6. Depends on fast-moving CLI flags (medium)

The adapters rely on flags such as `--permission-prompts host` and `--restricted`, and detect
support by searching `--help` output (`internal/harness/claudecode/adapter.go:70`). The nightly
authenticated conformance jobs are the only early warning, so they must run with real secrets.

### G7. Install instructions are buried (low)

The README quick start is "clone and build with Go 1.24". Release archives and Homebrew are
documented only in `docs/release.md`.

### G8. Cursor and Gemini skill exports may be out of date (low, needs checking)

The docs say neither vendor reads `SKILL.md` natively, so skills export as a Cursor rule or a
Gemini extension. Both may have added native Agent Skills support in early 2026. Re-check their
current docs.

## Proposed Studio console

### Goals

1. **Approvals come first.** For a control layer, the approvals queue is the home screen, and a
   pending request is never easy to miss.
2. **Usable without reading YAML.** Every definition can be edited through a form, and the
   underlying file is always one click away.
3. **Plain language.** Requests, failures and states say what happened and what to do next.
4. **Live.** Runs, approvals and health update from the existing event stream without reloading.
5. **Same architecture.** Studio stays one Go binary with an embedded UI and no Node dependency
   at runtime (ADR 0001).

### Layout

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ AgentWorks  [acme-teams ▾]  [ Search runs, agents…  ⌘K ]  ● Claude ● Copilot │
├────────────────────┬─────────────────────────────────────────────────────────┤
│ WORK               │ Approvals                                               │
│ ▸ Approvals    (2) │ 2 requests waiting. A grant lasts for one run and never │
│   Runs             │ raises its permission.                                  │
│   Inbox        (1) │ ┃ WAITING · research / synthesizer · Claude Code        │
│ TEAM               │ ┃ Write files in notes/synthesis/                       │
│   Agents and skills│ ┃ This folder only, for this run                        │
│   Memory       (1) │ ┃ Run: Summarize six customer interviews                │
│ HEALTH             │ ┃ [Approve for this run]  [Deny]  Open run              │
│   Monitors         │                                                         │
│   Sources          │ ┃ WAITING · engineering / implementer · Copilot CLI     │
│   Settings         │ ┃ Run git push origin abc-123-rounding                  │
│                    │ ┃ Commands starting with git push, for this run         │
│                    │ ┃ [Approve for this run]  [Deny]  Open run              │
└────────────────────┴─────────────────────────────────────────────────────────┘
```

The top bar shows the project, a command palette, harness readiness and storage use. The sidebar
groups views into Work, Team and Health, with badge counts for anything waiting on you. At phone
width the sidebar becomes a horizontal tab strip.

### Views

| View | What it shows | What you can do |
|---|---|---|
| **Approvals** | Every pending request across all runs, written in plain language with its scope ("This folder only, for this run"), agent, harness and permission. Past decisions below. | Approve for this run, deny, open the run. Browser notifications for new requests. |
| **Runs** | A filterable table: state, agent, request, harness, start time, duration. | Open a run, cancel, pin, delete raw detail. |
| **Run detail** | A live timeline (started, files read, approval requested and decided, files written, finished), the rendered conclusion (completed and still open), changed files, and a plain failure reason when it stops. | Approve inline, cancel, re-run with different permission, open the raw log. |
| **Inbox** | Unrouted and tied events, with the routes that matched and their priorities. | Route to a chosen route, re-evaluate, replay. |
| **Agents and skills** | Agents grouped by team, with description, permission ceiling, shared skills, default harness and memory file. Skills, routes and sources likewise. | Edit through forms, or toggle **View as file** to see and edit the YAML or Markdown. Saves keep today's SHA-256 version check. |
| **Memory** | Pending memory proposals as rendered diffs, with the run and agent that proposed them, plus history. | Apply or reject. External edits show as a conflict. |
| **Monitors** | Approval wait time, human correction rate, run success, source freshness, with status and a trend. | Open the runs and outcomes behind a monitor. |
| **Sources** | Each source's kind, state, last event and next check, including rate-limit backoff. | Test, poll now, pause or resume, view diagnostics. |
| **Settings** | Workspace bindings, harness readiness detail, storage limit and usage. | Confirm ad hoc folders, change limits. |

Example of the plain-language rule. Today a readonly run that tries to write ends with
`prepare approval request`. The console says:

> **Why it stopped:** This run needed to write a file, but it is read only. Re-run it with Read &
> write, or raise the route's permission.

### API changes

Most views map onto existing `/api/v1` endpoints. New or changed:

- `GET /api/v1/approvals?state=pending` lists approvals across all runs. Today approvals are
  returned only inside `GET /api/v1/runs/{runID}` (`internal/studio/runtime_api.go:45`).
- `GET /api/v1/memory-proposals?state=pending` likewise, for the Memory view and badge.
- Runtime events for approval requests and decisions, so badges and notifications update from
  the existing event stream.
- A plain-language failure reason on failed runs, alongside the current machine reason.
- Session bootstrap from the startup token (B2) instead of an open `GET /api/v1/session`.

### Implementation

- Use a small component framework, such as Preact or Svelte, built with esbuild into
  `internal/studio/static`. Commit the compiled output so `go build` and `go install` still need
  no Node, as ADR 0001 requires. CI checks that the committed build matches the source.
- Keep the current CSP (`script-src 'self'`, no inline scripts) and the CSRF header on every
  mutation.
- Use hash or history routing, so each view and run has a shareable local URL.
- One set of color tokens for light and dark, visible keyboard focus, and respect for
  `prefers-reduced-motion`.
- Playwright smoke tests drive the fake harness through queue → approve → succeed, deny → fail,
  memory apply, and inbox routing, and run in CI.

## Roadmap

### Phase 1: Fix the bugs (~15.5 h)

| Task | Fixes | Effort |
|---|---|---|
| Pass prompts on stdin or a private temp file | B1 | ~4 h |
| Require a startup token for Studio | B2 | ~6 h |
| Make the GitHub fetch test hermetic | B3 | ~30 min |
| Fix the papercuts | B5 | ~2 h |
| Test Windows in CI, or stop shipping it | B4 | ~3 h |

### Phase 2: Tell one story (~16 h)

| Task | Closes | Effort |
|---|---|---|
| Rewrite the README around the runtime: the pitch above, a console screenshot, install from Homebrew or a release archive, a five-minute `--fake-harness` demo with the research team first, and a short GIF of an approval | G2, G7 | ~8 h |
| Call the runtime 2.0, say what the `v1.0.0-rc` promise still covers, and extend `COMPATIBILITY.md` to format 2, the Studio API and database migrations | G2 | ~3 h |
| Add `CONTRIBUTING.md`, issue and PR templates, repository topics, and a roadmap issue | G2 | ~2 h |
| Move format-1 documentation into `docs/` | G2 | ~3 h |

### Phase 3: Build the Studio console (~72 h)

| Task | Effort |
|---|---|
| Console shell and design system: sidebar navigation, routing, ⌘K command palette, light and dark tokens, top-bar readiness and storage | ~16 h |
| Approvals and runs views: cross-run approvals queue with badges and browser notifications, run table, live run timeline, rendered conclusions, plain failure reasons | ~18 h |
| Form-based team editor for agents, skills, routes and sources, with "View as file" | ~18 h |
| Inbox, memory, monitors and sources views | ~12 h |
| Frontend build pipeline, committed build output, and Playwright smoke tests in CI | ~8 h |

### Phase 4: Make the positioning real (~40 h)

| Task | Supports | Effort |
|---|---|---|
| Run limits and budgets per route and agent: runs per day, maximum duration, concurrent runs, token usage. A run that exceeds its budget pauses for you instead of continuing. | Control layer | ~10 h |
| Exportable approval audit (JSON or CSV per run) for a team lead or compliance review | Control layer | ~6 h |
| Starter teams in `agentworks init --runtime` for software, research, writing or operations work. Only the software team needs Jira or GitHub. | Beyond code | ~8 h |
| Folder-watch source: a file landing in a watched folder emits an event. The path stays in `agentworks.local.yaml`, never in the event. | Beyond code | ~10 h |
| A second non-code example, such as a writing or operations team, with a guide for people who don't code | Beyond code | ~6 h |

The folder-watch source and the starter teams are new proposals. Neither exists in the code yet.

### Phase 5: Earn the release gate (~25 h, plus calendar time)

| Task | Closes | Effort |
|---|---|---|
| Configure the Claude, Copilot, Jira and GitHub secrets from `docs/release.md`, get the nightly authenticated conformance jobs green, and alert on failure | G1, G6 | ~4 h |
| Dogfood the software-factory flow on real Jira assignments and PR comments for one to two weeks, and record findings | G1 | ~15 h |
| Run the multi-day Studio soak: sleep and wake, harness failures, rate limits, full storage, abrupt termination | G1 | ~6 h |

### Phase 6: Ship the preview (~2 h)

Fill in `m9-release-evidence.yaml` for the tested commit, commit only that file, tag the
release, and check the Homebrew formula and release archives on each supported platform.

### Phase 7: After launch (~68 h)

| Task | Closes | Effort |
|---|---|---|
| Codex CLI harness adapter | G5 | ~20 h |
| Gemini CLI harness adapter | G5 | ~16 h |
| Git worktree isolation so several write-capable runs can share one repository | Competitive gap | ~16 h |
| Opt-in background service mode (launchd and systemd user services) | G4 | ~16 h |

Also re-check the Cursor and Gemini skill mappings (G8, ~6 h) when convenient.

### Total

About 239 hours: roughly 16 weeks at 15 hours a week. The dogfood and soak also need at least a
week of calendar time each.

### Shorter alternative

To get users sooner, tag a clearly labelled `v2.0.0-preview.N` right after Phases 1 and 2. That
needs a small change to `release.yml` so a preview tag can bypass the soak requirement while still
running build and tests. Then build only the console shell, the approvals and runs views, and the
frontend build before collecting feedback. Finish the rest of the console, the positioning work
and the release gate afterwards.

## Positioning against other tools

These are my assessments as of mid-2026. This market changes monthly, so re-check names and
features before publishing comparisons.

| Category | Examples | Where they're stronger | Where AgentWorks can win |
|---|---|---|---|
| Hosted issue-to-PR agents | Copilot coding agent, Codex cloud, Cursor background agents, Devin | No setup, always on | Any harness, local code and credentials, per-run permission ceilings, existing subscriptions |
| Local CLI orchestrators | Vibe Kanban, Conductor, Claude Squad | Polished UI, parallel worktrees | Event routing, durable state, bounded approvals and audit, reviewed memory, monitors |
| Agents in CI | Claude Code GitHub Actions | Runs in existing CI | Jira and GitHub correlation, non-Git workspaces, mid-run human approval |
| Agent frameworks | LangGraph, CrewAI, vendor agent SDKs | Full programmatic control | No code and no API keys; the vendor harness does the reasoning |
| Automation platforms | n8n, Zapier agents | Connector catalogs, mature scheduling | Coding-grade agents with file and command permissions and workspace locking |

The console (Phase 3) and worktree isolation (Phase 7) close the main gaps against local
orchestrators. Budgets, the audit export and reviewed memory are what hosted agents don't offer.

## Out of scope for this plan

Everything in PLAN-2.0's "Explicitly deferred" list stays deferred, including multi-user
authentication, remote browser access, persistent cross-run grants and a general workflow engine.
