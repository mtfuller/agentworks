# Runtime threat model

This document defines the security boundary for the first local AgentWorks runtime release.
AgentWorks is single-user, localhost-only software. It coordinates powerful vendor harnesses and
therefore assumes the operating-system account and the checked-in project definitions are trusted.
Event payloads, imported dependencies, remote service responses, model output, and files in a
selected workspace are not trusted.

## Protected assets and boundaries

The runtime protects workspace files, environment credentials, harness authentication, approval
decisions, durable run history, and the integrity of project definitions. Studio binds only to
loopback. Browser mutation endpoints require the Studio session cookie, a separate CSRF token,
same-origin requests, and an accepted local Host header. SQLite commits are authoritative; SSE and
in-memory channels are notification mechanisms only.

AgentWorks does not make an untrusted project safe to run. A project owner can deliberately name
host commands, Docker images, remote tools, hooks, and imported instructions. Review the project
and its lock data before granting it authority.

## Threats and controls

### Event and prompt injection

Jira fields, GitHub comments, manual-event data, tool results, and workspace text may contain
instructions intended to redirect an agent. Routes—not payloads—select the team, agent, workspace,
harness, and maximum permission. Routing uses typed equality/presence rules and chooses exactly one
winner; ambiguity remains in the inbox. Context labels external event data as untrusted and never
interprets payload fields as paths, commands, policy, or configuration. Injection can still
influence reasoning inside the authority already granted, so write-capable work remains approval
bounded and external actions must be reviewed through their tool boundary.

### Approval spoofing and replay

Permission bridges are created per run with a random bearer token and loopback-only endpoint. They
are never written into ordinary Claude or Copilot configuration. Approval requests are normalized
into either a canonical workspace directory or an executable plus argv prefix. Grants are durable,
audited, scoped to one run, and reusable only when the later request is covered by the approved
scope. A readonly run cannot be elevated. Denial, cancellation, token mismatch, unsupported tool
shapes, and malformed input fail closed. The remaining local risk is another process running as the
same OS user and able to inspect that user's process memory or runtime files; OS-account compromise
is outside this release boundary.

### Path escape and workspace confusion

Browser requests contain workspace aliases, never authoritative absolute paths. Aliases resolve
through the machine-local, gitignored registry and are canonicalized before use. Approval paths are
relative to the selected workspace and traversal or absolute-path scopes are rejected. Pack and
dependency extraction reject absolute paths, traversal, symlinks, and non-regular files. One
write-capable run may hold a workspace lease; readonly runs may overlap. An advisory lock warns
about non-AgentWorks writers, but cannot stop another local program from changing the folder.

### Command matching and process escape

Harnesses and providers use structured executable/argv specifications; invoking a shell is an
explicit choice. Command grants match a canonical executable and bounded subcommand prefix, not a
substring. Compound shell commands normalize to a shell-level request rather than accidentally
granting one inner command. The process supervisor owns the complete process tree and terminates it
on cancellation or timeout on Unix and Windows. Host tools still execute with the user's OS
authority, so use the Docker provider for a stronger filesystem boundary when available.

### Container boundary

Docker tools require a digest-pinned image. The selected workspace is the only host bind mount and
is readonly for readonly runs. AgentWorks never mounts the Docker socket, drops all capabilities,
sets no-new-privileges and a PID limit, uses a readonly container root for readonly work, and passes
only explicitly named environment variables. Network mode is visible in the execution plan and can
be disabled. Docker daemon administrators and a malicious container runtime are outside this
boundary.

### Dependency and release supply chain

Imports resolve before writing, constrain transports, extract defensively, verify registry
integrity when available, and pin source plus local content digest. Local and installed components
cannot silently shadow one another. Team packs include a deterministic manifest and verify every
file before installation. Container variants require image digests. Tagged release jobs require
macOS Developer ID signing and notarization or Windows Authenticode signing, publish SHA-256 sums,
and attach build provenance. These controls establish identity and integrity, not that third-party
instructions or binaries are benign.

### Secret exposure and logs

First-release secrets come only from environment variable names declared by a source or tool.
Configurations preserve references rather than values. Docker receives only its explicit
allowlist, and credential values never enter its argv. Vendor harnesses receive only an operational
environment allowlist and the credential variables recognized by that harness; unrelated host
variables are not inherited. Recognized credential values are removed from normalized output as a
defense-in-depth measure. HTTP errors strip URL queries and never
include authentication headers; redirects cannot carry headers to another host. Runtime events are
a small redacted index and never store prompts, output, credentials, or opaque provider request
material. Raw JSONL remains in bounded external logs and is subject to redaction and retention
pruning. Arbitrary values copied from prompts, tool results, or workspace files cannot be identified
as secrets reliably, so users should still avoid placing secrets in those inputs.

### Local web and denial of service

Studio is localhost-only and rejects unexpected Host and Origin values. Request bodies, stored
context, snapshots, archives, and SSE replay are bounded. Runtime storage defaults to 1 GB; oldest
unpinned raw detail is pruned while conclusions, outcomes, approvals, audits, monitors, and run
identity remain. A pinned set can consume the limit and require manual intervention. Remote access,
multi-user authentication, and RBAC are explicitly outside the first release.

## Release security gate

Before a release candidate:

1. Run the normal, race, provider, vendor-schema, and authenticated conformance suites.
2. Review new commands, mounts, environment propagation, persisted payloads, and HTTP endpoints.
3. Confirm no credential or raw prompt appears in `runtime_events`, API responses, or test fixtures.
4. Exercise cancellation, restart recovery, full storage, and malformed event/approval inputs.
5. Record every critical/high finding. A release cannot proceed while any such finding is open.

Report vulnerabilities privately as described in the repository's `SECURITY.md`.

The current M9 implementation review is recorded in
[`m9-local-review.md`](m9-local-review.md). Its local disposition does not replace the external
dogfood, authenticated-conformance, soak, or reviewer evidence required by the release gate.
