# Release-candidate diagnostics

AgentWorks records a redacted diagnostic attempt for every Jira or GitHub poll. This is intended
to answer operational questions without retaining credentials, HTTP headers, prompts, or external
payloads.

## Studio workflow

1. Open **External sources** in Studio.
2. Use **Test connection** for a credential/readiness check or **Poll now** to run one real poll.
3. Select **Diagnostics** for the source.

An attempt records its start/finish time, cursor before and after, emitted and deduplicated event
counts, outcome counts, routed run count, opaque event/run IDs, rate-limit reset, and a bounded
safe error code/message. A successful poll first becomes `committed` with its durable cursor and
event IDs, then `succeeded` after routing completes. A `running` attempt after an unexpected stop
is useful evidence that a process was interrupted before a connector result was finalized.

The health endpoint also exposes aggregate queue, run, and stale-source metrics. Studio presents a
compact pending-run/stale-source summary beside storage readiness.

## CI simulation coverage

Connector tests use recorded Jira and GitHub HTTP responses, not live accounts. They exercise the
production parsers and polling paths for assignment, comment, checks, merge, cursor advancement,
deduplication, rate limiting, malformed/error handling, and credential-safe failures. These tests
run in ordinary CI with no model or SaaS account.

Live connector checks remain separately opt-in because they prove deployment credentials and vendor
API behavior rather than the deterministic connector contract.

## Privacy boundary

Diagnostic attempts never contain request or response bodies, authorization headers, credential
values, workspace paths, or agent prompts. Event and run IDs are retained so Studio can join an
attempt to the existing inbox, route decision, run, approval, outcome, and work-item timeline.
