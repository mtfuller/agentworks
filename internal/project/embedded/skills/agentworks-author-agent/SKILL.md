---
name: agentworks-author-agent
description: >
  How to write or edit an agent (subagent) in this AgentWorks project: agent.md
  frontmatter, portable tools and model settings, writing the instructions, and resources.
  Use when creating or changing anything under agents/.
---

# Authoring an agent

An agent is a persona with its own instructions, delegated to for a class of tasks. It
exports to each vendor's native agent format (Claude Code subagents, Copilot `.agent.md`,
Gemini CLI, Cursor).

## Create it

```bash
agentworks new agent <name> --description "What it handles and when to delegate to it"
```

Or `--from-template <id>` (`code-reviewer`, `researcher`, `reference-file-qa`, ...).

```
agents/<name>/
  agent.md       manifest: frontmatter + system prompt
  resources/     reference material the agent may consult
  evals/*.yaml   behavior-eval cases
```

## agent.md frontmatter

```yaml
---
kind: agent
name: code-reviewer           # required; matches directory
description: Reviews a code change for correctness, security, and style. Use after finishing a feature or before a PR.
version: 0.1.0
tools: [read-files, run-commands]   # optional
model: balanced                     # optional
eval_runner: claude -p              # optional
---
```

### Portable `tools:` and `model:`

Both are vendor-neutral and mapped per target on export. Values outside these sets fail
`agentworks validate`.

- `tools:` — any of `read-files`, `edit-files`, `run-commands`, `web-search`,
  `code-execution`. Grant the minimum the agent needs. Leave unset to get each vendor's own
  default (usually every tool).
- `model:` — `fast`, `balanced`, or `powerful`. Leave unset to inherit.

Not every target honors both (Copilot and Cursor currently ignore them); check
`agentworks targets`.

## Body

The body is the system prompt. Cover: role, what to do step by step, what to avoid, and the
exact output format callers can rely on. Put longer reference material in `resources/` and
say when to read each file.

The description is how a parent agent chooses to delegate. Include *when* to use it, not
just what it is, and keep it distinct from other agents' descriptions.

## Finish

1. `agentworks validate` (tools/model values, description warnings).
2. Add `evals/` cases (see `agentworks-evals`).
3. `agentworks export`.
