# AGENTS.md

Guidance for coding agents working in this project.

## What this project is

**{{project}}** is an [AgentWorks](https://github.com/mtfuller/agentworks) project: agents,
skills, MCP servers, and hooks are authored here as plain files, then exported to
whatever vendor format a given AI harness needs (Claude Code, ChatGPT, GitHub Copilot,
Cursor, Gemini CLI, and others) with the `agentworks` CLI.

## Layout

```
agentworks.yaml   project manifest: name, description, default targets, eval
agents/           agent definitions (agent.md + resources/)
skills/           skills (skill.md + scripts/tests/samples)
mcp/              MCP servers (mcp.md + src/tests)
hooks/            lifecycle hooks (hook.md)
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
| [agentworks-author-mcp](.agents/skills/agentworks-author-mcp/SKILL.md) | write or edit an MCP server (local or remote) |
| [agentworks-author-hook](.agents/skills/agentworks-author-hook/SKILL.md) | write or edit a lifecycle hook |
| [agentworks-evals](.agents/skills/agentworks-evals/SKILL.md) | write behavior-eval cases for a skill or agent |

Prefer the CLI over hand-writing `<kind>.md` files from scratch (`agentworks new`) and over
hand-editing exported vendor output (`agentworks export` regenerates it).
`agentworks validate` also warns on weak descriptions (too vague, too long, or
indistinguishable from another artifact's) -- worth heeding even though it won't fail
the command unless `--strict` is passed, since a bad description is how an agent picks
the wrong artifact or misses this one entirely.
