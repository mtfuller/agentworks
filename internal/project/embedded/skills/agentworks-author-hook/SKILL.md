---
name: agentworks-author-hook
description: >
  How to write or edit a lifecycle hook in this AgentWorks project: hook.md frontmatter,
  choosing vendor-specific event names, matchers and timeouts, the command it runs, and
  safety review. Use when creating or changing anything under hooks/.
---

# Authoring a hook

A hook runs a shell command automatically on a harness lifecycle event (before a tool runs,
at session start, after a file edit, ...).

## Create it

```bash
agentworks new hook <name> --description "What it enforces or reports, and on which event"
```

Or `--from-template <id>` (`pre-commit-lint`, `notify-webhook`).

```
hooks/<name>/hook.md
```

## hook.md frontmatter

```yaml
---
kind: hook
name: pre-commit-lint         # required; matches directory
description: Runs the linter before a tool use proceeds, blocking on failure.
version: 0.1.0
events: [PreToolUse]          # required together with command
command: ./scripts/lint.sh
---
```

That `events` + `command` form is shorthand for one handler per event. For a matcher, a
timeout, or different commands per event, list `handlers` instead:

```yaml
handlers:
  - event: PreToolUse
    matcher: Bash               # optional filter; a regex for tool events on most vendors
    command: ./scripts/check-bash.sh
    timeout: 30                 # optional, seconds
  - event: SessionStart
    command: ./scripts/hello.sh
```

Rules:

- Use **either** `handlers` **or** the `events` + `command` shorthand, never both. The
  shorthand's two fields must be **set together**; validate fails if only one is set, and
  export fails if the hook declares no handlers at all. Every handler needs an `event` and
  a `command`; `timeout` is seconds and never negative.
- `events` values are passed through to the target **unchanged**, so use that vendor's own
  names:
  - Claude Code and GitHub Copilot: `PreToolUse`, `PostToolUse`, `SessionStart`, ... (Copilot
    accepts PascalCase or camelCase).
  - Cursor: `beforeShellExecution`, `afterFileEdit`, `preToolUse`, ...
  - Gemini CLI: its own set. Check the vendor docs.
  A hook meant for several vendors may need one hook artifact per vendor.
- A `matcher` filters when a handler fires; without one it fires for every occurrence of the
  event. All four hook-capable targets support `matcher` and `timeout` (AgentWorks converts
  seconds to Gemini CLI's milliseconds). Cursor honors `matcher` only on some events; see its
  docs.
- If several hooks share an event, all run; none replaces another.

## Safety

`command` runs on every trigger with the user's permissions. `agentworks validate` flags
patterns like `curl | sh`, `base64 -d`, `eval $(...)`, `/dev/tcp`, `nc -e`, `chmod +s`, and
`sudo`; treat each warning as a real review item. Keep commands short, call a script kept
in the repo, and never inline secrets — reference env vars.

## Body

Explain what the hook does, why, and how to disable it. Nothing in the body is executed.

## Finish

`agentworks doctor hooks/<name>` (is the command's binary on PATH?), `agentworks validate`,
`agentworks export`.
