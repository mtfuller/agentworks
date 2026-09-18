---
name: agentworks-author-tool
description: >
  How to write or edit a tool in this AgentWorks project: tool.md frontmatter, running it as
  an MCP server via command and auth, source and tests layout, and the build, test, doctor,
  and run loop. Use when creating or changing anything under tools/.
---

# Authoring a tool

A tool is a callable capability an agent uses, exposed as an **MCP server**. Claude Code
and GitHub Copilot register it from the `command` field on export.

## Create it

```bash
agentworks new tool <name> --description "What it does and what it operates on"
```

Or `--from-template <id>` (`api-wrapper`, `cli-wrapper`, `node-tool`).

```
tools/<name>/
  tool.md     manifest: frontmatter + interface docs
  src/        implementation
  tests/      tests
```

## tool.md frontmatter

```yaml
---
kind: tool
name: jira-fetch              # required; matches directory
description: Fetches Jira issues by key or JQL. Use when an agent needs ticket details.
version: 0.1.0
entrypoint: src/main          # file must exist
command: node src/server.js   # shell command that starts the MCP server
auth: [JIRA_TOKEN]            # env vars the command needs
build: npm install && npm run build
test: npm test
---
```

Rules `agentworks validate` enforces: `auth` requires `command`. Auth entries are
environment variable **names**, never values; exports generate `${VAR}` placeholders, so
never put a secret in any file here.

`command` runs with the user's permissions. Validate warns on risky shapes (`curl | sh`,
`base64 -d`, `sudo`, `/dev/tcp`, ...). Keep it a plain launch command.

## Body

Document the interface: each MCP tool the server exposes, its arguments, result shape, and
error behavior. Agents read this to know how to call it.

## Development loop

1. `agentworks build tools/<name>`, then `agentworks test tools/<name>`.
2. `agentworks doctor tools/<name>` — checks binaries on PATH, entrypoint exists, and that
   `auth` variables are set (a warning unless `--strict`).
3. `agentworks run tools/<name>` — interactive MCP inspector against the real server (needs
   a terminal; inherits your shell's env, so export `auth` vars first).
4. `agentworks validate`, then `agentworks export`.
