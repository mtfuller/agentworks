# Harness conformance ledger

This ledger records evidence for the headless harness surfaces AgentWorks depends on. A
version string alone never establishes runtime compatibility. Authenticated payload
fixtures must be captured, redacted, and replayed offline before an adapter can execute a
write-capable run.

## Local probe: 2026-09-24

| Harness | Local result | Evidence | Remaining gate |
|---|---|---|---|
| Claude Code | Installed, unauthenticated | Native CLI `2.1.220`; `--print`, JSON/stream-JSON output, permission modes, and MCP configuration are present. `claude auth status --json` returned `loggedIn: false`. | Capture authenticated text/stream output, tool use, delegated permission requests, denial, cancellation, and completion. |
| GitHub Copilot CLI | Missing | No `copilot` executable was found on `PATH`. | Install, authenticate, and capture the same conformance set, including `permissionRequest` hook payloads and decisions. |

No model request or paid API operation was made by this probe.

One discrepancy is intentionally retained as a readiness diagnostic: Anthropic's official
CLI reference documents `--permission-prompt-tool` for non-interactive permission handling,
but the installed `2.1.220` help output does not advertise it. Some native builds accept
hidden flags, so AgentWorks will not infer compatibility from help text; the authenticated
fixture must demonstrate the behavior.

## Readonly execution capture: 2026-09-25

| Harness | Local result | Evidence | Remaining gate |
|---|---|---|---|
| Claude Code | Authenticated readonly path verified | Native CLI `2.1.282`; status reported `loggedIn: true` with `claude.ai` authentication. A live stream-json invocation initialized with exactly `Glob`, `Grep`, and `Read`, `dontAsk` permission mode, and no MCP servers, then completed successfully in one turn. | Capture tool-bearing streams, failure, cancellation, and MCP permission-tool behavior. |
| GitHub Copilot CLI | Authenticated readonly path verified | Native CLI `1.0.88`; a live JSONL prompt completed successfully with `view`, `rg`, and `glob` as the only available tools. The CLI reported shell and edit tools disabled, returned no tool requests, modified no files, and emitted a terminal `result`. A redacted replay fixture is checked in beside the adapter tests. | Capture tool-bearing streams plus every `permissionRequest` hook shape and allow/deny behavior before enabling write-capable runs. |

The live Copilot capture also proved that wildcard `--excluded-tools='*'` is invalid in this
release. AgentWorks therefore uses an explicit `--available-tools` allowlist and only then
supplies the prompt-mode-required automatic permission flag. Provider call/request material
is removed from JSONL before the worker persists it.

Claude's redacted, version-labelled replay fixture is checked in beside the adapter tests;
local paths, session identifiers, and user configuration inventories are removed before
persistence.

## Write-capable execution capture: 2026-09-26

| Harness | Local result | Evidence |
|---|---|---|
| Claude Code | Authenticated bounded command path verified | Native CLI `2.1.282` loaded only the ephemeral AgentWorks permission MCP server. A compound shell write paused with an exact-command scope, resumed after Studio approval, created the requested file, and completed successfully. |
| GitHub Copilot CLI | Authenticated bounded file-write path verified | Native CLI `1.0.88` loaded an ephemeral plugin whose `permissionRequest` hook received the `create` request. Studio presented a workspace-relative directory grant, approval resumed the same tool call, and Copilot created the requested file. A missing run credential was separately observed to deny the action explicitly rather than fall through. |

Neither run modified ordinary Claude or Copilot configuration. Per-run configuration was
created with owner-only permissions and removed by the worker. Gateway credentials appeared
only in those ephemeral child-helper configurations, never in harness argv or tool-process
environments. Unit fixtures cover allow, deny, malformed input, credential revocation,
scope reuse, and temporary-config cleanup; portable normalizer tests cover every M4 event
kind.

Primary references checked on 2026-09-24:

- [Claude Code CLI reference](https://docs.anthropic.com/en/docs/claude-code/cli-usage)
- [GitHub Copilot CLI programmatic usage](https://docs.github.com/en/copilot/how-tos/copilot-cli/automate-copilot-cli/run-cli-programmatically)
- [GitHub Copilot hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference)
- [GitHub Copilot CLI command reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference)

## Fixture policy

Each checked-in fixture must include harness ID, exact CLI version, capture date, platform,
invocation mode, and expected normalized events. Replace credentials, usernames, absolute
paths, repository identities, session IDs, and remote URLs with stable placeholders. Never
check in a raw capture first and redact it afterward.

Required fixture cases:

1. Text-only success.
2. Streaming success with tool calls.
3. Non-zero harness failure.
4. User cancellation and forced cancellation.
5. Read, write, command, URL, and MCP permission requests.
6. Allow-once and deny decisions.
7. Malformed/truncated output.
8. Structured conclusion present and missing.

Write-capable execution is enabled only for the bounded filesystem-write and command scopes
covered by the M4 bridge fixtures. URL, external-collaboration, and MCP-specific permissions
remain fail-closed until later milestones add and fixture those capability kinds.

## Post-hardening readiness recheck: 2026-09-28

After harness subprocess environments were reduced to an operational allowlist plus each
harness's recognized credential variables, the host-level probe still reported Claude Code
`2.1.282` ready with `claude.ai` authentication. GitHub Copilot CLI `1.0.88` remained
`auth-unverified`, which is the expected non-consuming result for secure-store authentication:
this CLI version exposes `login` but no authentication-status command. Its authenticated readonly
and bounded-write executions above remain the conformance evidence; scheduled CI uses
`COPILOT_GITHUB_TOKEN` so its probe can fail closed without submitting a model prompt.
