# Studio release-candidate soak

M9 requires a multi-day foreground soak before background service installation or a runtime release.
Use a disposable format-2 project and workspace, start Studio in the foreground, and retain this
checklist with timestamps and the tested commit.

## Setup

```sh
agentworks init --runtime soak-project
cd soak-project
cp agentworks.local.yaml.example agentworks.local.yaml
agentworks studio --experimental --fake-harness
```

Use the real Claude and Copilot harnesses for at least one readonly and one approved write-capable run
each. Use the fake harness for repeatable failure and restart cases that must not consume model usage.

## Required observations

- Leave Studio running across at least two laptop sleep/wake cycles and confirm schedules, leases,
  connector next-poll times, and the UI recover without duplicate runs.
- Exercise successful, failed, cancelled, timed-out, retried, and descendant-process runs.
- Stop Studio during pending, approval-waiting, and active runs; restart and verify durable recovery and
  that no child process survived shutdown.
- Exercise Jira and GitHub success, authentication failure, rate limiting, stale-source monitoring, and
  recovery without resetting the durable cursor.
- Generate storage above the configured limit. Confirm oldest unpinned raw detail is removed in order,
  pinned detail remains, and conclusions/outcomes/approvals/audits/monitors stay readable.
- Edit a memory file outside Studio while a proposal is pending and confirm optimistic conflict rather
  than overwrite.
- Attempt ambiguous and unmatched events, path traversal, malformed approvals, duplicate external event
  IDs, and concurrent write-capable runs. Each must fail closed or remain inspectable.
- Review logs and API responses for prompts, credentials, authorization headers, bearer tokens, and
  opaque vendor request material.

## Evidence record

Record start/end UTC, commit, OS/architecture, AgentWorks version, harness versions, Docker version,
sleep/wake count, run/event counts, peak storage, every failure, and its disposition. A crash, leaked
secret, orphaned process, duplicated external action, lost durable record, or approval/path escape is a
release blocker. Service installation remains unavailable until this checklist completes without an open
critical/high finding.
