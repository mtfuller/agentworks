# AGENTS.md

Guidance for coding agents working in this project.

## What this project is

**{{project}}** is an [AgentWorks](https://github.com/mtfuller/agentworks) project: agents,
skills, tools, hooks, and workflows are authored here as plain files, then exported to
whatever vendor format a given AI harness needs (Claude Code, ChatGPT, GitHub Copilot,
Microsoft 365 Copilot, and others) with the `agentworks` CLI.

## Layout

```
agentworks.yaml   project manifest: name, description, default targets, publisher, eval
agents/           agent definitions (agent.md + resources/)
skills/           skills (skill.md + scripts/tests/samples)
tools/            MCP-server-backed tools (tool.md + src/tests)
hooks/            lifecycle hooks (hook.md)
workflows/        multi-artifact pipelines (workflow.md)
```

Each artifact is a directory containing one `<kind>.md` file (YAML frontmatter plus a
Markdown body) alongside whatever supporting files it needs. It's plain text — read and
edit it directly rather than going through the CLI for inspection.

## Skills for working in this project

Start with `agentworks-cli`, then load the authoring skill for the kind you're touching.

| Skill | Use it to |
| --- | --- |
| [agentworks-cli](.agents/skills/agentworks-cli/SKILL.md) | run any `agentworks` command: scaffold, validate, build, test, export, import |
| [agentworks-author-skill](.agents/skills/agentworks-author-skill/SKILL.md) | write or edit a skill |
| [agentworks-author-agent](.agents/skills/agentworks-author-agent/SKILL.md) | write or edit an agent |
| [agentworks-author-tool](.agents/skills/agentworks-author-tool/SKILL.md) | write or edit an MCP-server-backed tool |
| [agentworks-author-hook](.agents/skills/agentworks-author-hook/SKILL.md) | write or edit a lifecycle hook |
| [agentworks-author-workflow](.agents/skills/agentworks-author-workflow/SKILL.md) | compose agents and tools into a workflow |
| [agentworks-evals](.agents/skills/agentworks-evals/SKILL.md) | write behavior-eval cases for a skill or agent |

Prefer the CLI over hand-writing `<kind>.md` files from scratch (`agentworks new`) and over
hand-editing exported vendor output (`agentworks export` regenerates it).
`agentworks validate` also warns on weak descriptions (too vague, too long, or
indistinguishable from another artifact's) -- worth heeding even though it won't fail
the command unless `--strict` is passed, since a bad description is how an agent picks
the wrong artifact or misses this one entirely.
