# M9 local runtime security review

Date: 2026-09-28  
Scope: the local format-2 runtime implementation in the current working tree  
Disposition: local review complete; this is not the release approval or external soak evidence

## Review scope

The review traced the security boundaries described in the runtime threat model through Studio's
HTTP middleware and definition editor, event normalization and routing, run-scoped permission
bridges, workspace path resolution, process supervision, harness invocation and output storage,
pack extraction and verification, provider construction, connector persistence, and the release
gate. It also exercised focused regression tests for every finding below.

## Findings

| ID | Initial severity | Finding | Resolution | Status |
|---|---|---|---|---|
| AW-M9-001 | High | Claude and Copilot processes inherited the complete host environment, exposing unrelated credentials to the selected vendor process. | Harness invocation and probes now receive a deterministic operational allowlist plus only credential variables explicitly recognized by that harness. Recognized credential values are also removed from normalized output as defense in depth. | Closed |
| AW-M9-002 | Medium | `pack.Verify` could follow a symlink when called directly on a previously extracted directory. Installation extraction was already defensive, but the public verification boundary was weaker. | Verification now rejects every non-regular entry before reading content; a regression test replaces a declared file with an external symlink. | Closed |
| AW-M9-003 | Low | The loopback approval gateway accepted a valid JSON request followed by another JSON value. | The gateway now requires EOF after exactly one strictly decoded request and returns HTTP 400 for trailing data. | Closed |
| AW-M9-004 | Medium | Event redaction covered common token/password names but omitted common API-key, private-key, and credential field names. | Recursive redaction now recognizes those key forms, with nested regression coverage. | Closed |

No critical or high finding from this local review remains open.

## Residual risks and release blockers

- Arbitrary secrets copied into prompts, workspace files, tool results, or free-form model text
  cannot be identified reliably. The runtime minimizes inherited credentials and filters known
  values, but users must still keep secrets out of agent-visible content where possible.
- A malicious checked-in project can intentionally select powerful host commands, images, tools,
  and instructions. Project trust remains an explicit boundary.
- Another process running as the same OS account can interfere with local files or process memory.
  Multi-user isolation is outside v1.
- The Jira/GitHub software-factory dogfood, authenticated Claude/Copilot and connector workflows,
  multi-day foreground Studio soak, and independent release approval still require external
  evidence. `docs/operations/m9-release-evidence.yaml` therefore remains `pending`.

## Release recommendation

The local implementation is suitable to proceed to authenticated conformance and soak testing.
Do not tag the runtime release until the pending evidence file is completed, independently
reviewed, and accepted by `agentworks release-check`.
