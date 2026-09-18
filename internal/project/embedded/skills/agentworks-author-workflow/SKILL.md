---
name: agentworks-author-workflow
description: >
  How to write or edit a workflow in this AgentWorks project: workflow.md frontmatter, the
  ordered steps list referencing agents and tools by name, and resolving them. Use when
  creating or changing anything under workflows/.
---

# Authoring a workflow

A workflow is an ordered pipeline that composes existing **agents** and **tools**. It owns
no logic of its own; author the pieces first, then compose them.

## Create it

```bash
agentworks new workflow <name> --description "What the pipeline accomplishes end to end"
```

Or `--from-template <id>` (`research-then-act`, `fetch-then-review`).

```
workflows/<name>/workflow.md
```

## workflow.md frontmatter

```yaml
---
kind: workflow
name: fetch-then-review       # required; matches directory
description: Fetches a ticket with a tool, then has a reviewer agent assess it.
version: 0.1.0
steps:
  - tool: jira-fetch
  - agent: code-reviewer
---
```

Rules:

- Each step is a mapping with exactly one of `agent: <name>` or `tool: <name>`. Nothing
  else (skills, hooks, workflows) can be a step.
- Every name must resolve to a real artifact in this project, or `agentworks validate`
  fails naming the step. Namespaced (imported) artifacts are referenced as
  `namespace/name`.
- Order is significant: steps run top to bottom.

## Body

Describe how data flows between steps: what each step receives and passes on, and what the
final output is. The step list only names the pieces; the body is the wiring documentation
readers and agents rely on.

## Finish

1. Make sure every referenced agent/tool exists (`agentworks list`).
2. `agentworks validate`.
3. `agentworks export`. Claude Code and GitHub Copilot bundle workflows; check
   `agentworks targets` for others.
