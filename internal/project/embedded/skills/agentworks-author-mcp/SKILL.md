---
name: agentworks-author-mcp
description: >
  How to write or edit an MCP server in this AgentWorks project: mcp.md frontmatter, local
  (stdio) versus remote (http/sse) servers, command, args, env and auth, source and tests
  layout, and the build, test, doctor, and run loop. Use when creating or changing anything
  under mcp/.
---

# Authoring an MCP server

An `mcp` artifact registers an **MCP server** that gives an agent callable tools. Every
export target that supports MCP (Claude Code, GitHub Copilot, Cursor, Gemini CLI) generates
its own server registration from this artifact's frontmatter.

## Create it

```bash
agentworks new mcp <name> --description "What it does and what it operates on"
```

The default scaffold is a working, dependency-free Python server with one example tool, so
`agentworks test` and `agentworks run` pass immediately. Or `--from-template <id>`:

| Template | Use for |
| --- | --- |
| `api-wrapper` | expose a REST API (needs `API_BASE_URL`, `API_TOKEN`) |
| `cli-wrapper` | expose a local command-line tool |
| `node-mcp` / `node-ts-mcp` | a Node.js or TypeScript server |
| `npx-wrapper` | register a published server package, no code |
| `remote-http` | register a hosted server by URL, no code |

```
mcp/<name>/
  mcp.md      manifest: frontmatter + interface docs
  src/        implementation (absent for npx-wrapper and remote-http)
  tests/      tests
```

## mcp.md frontmatter

A local server (the default, `transport: stdio`):

```yaml
---
kind: mcp
name: jira-fetch              # required; matches directory
description: Fetches Jira issues by key or JQL. Use when an agent needs ticket details.
version: 0.1.0
entrypoint: src/server.py     # file must exist
command: python3 src/server.py
auth: [JIRA_TOKEN]            # secret env vars the server needs
env: {JIRA_SITE: example}     # non-secret env vars
build: npm install && npm run build
test: python3 -m unittest discover -s tests
---
```

- `command` alone runs through `sh -c`, so it can be a full command line.
- With `args: [...]`, `command` is run directly with those arguments (each quoted for you),
  the natural shape for `command: npx` with `args: [-y, some-server@1.2.3]`.

A remote server:

```yaml
---
kind: mcp
name: docs-search
description: Searches the company docs. Use when an agent needs internal documentation.
transport: http               # or sse
url: https://example.com/mcp
headers: {Authorization: "Bearer ${DOCS_TOKEN}"}
auth: [DOCS_TOKEN]
---
```

Rules `agentworks validate` enforces: `transport` is `stdio`, `http`, or `sse`; stdio needs a
`command`; http/sse needs a `url` and must not set `command`; a sensitive header
(Authorization, anything with token/key/secret/password) must reference `${VAR}` rather than
carry a literal value.

`auth` entries are environment variable **names**, never values; exports generate `${VAR}`
placeholders, so never put a secret in any file here. Use `env` only for non-secret settings.

`command` runs with the user's permissions. Validate warns on risky shapes (`curl | sh`,
`base64 -d`, `sudo`, `/dev/tcp`, ...). Keep it a plain launch command.

## Body

Document the interface: each MCP tool the server exposes, its arguments, result shape, and
error behavior. Agents read this to know how to call it.

## Development loop

1. `agentworks build mcp/<name>` if it has a `build:`, then `agentworks test mcp/<name>`.
   With no `test:`, `test` runs a smoke check: start the server, run the MCP handshake, list
   its tools (skipped with a warning if an `auth` variable is unset).
2. `agentworks doctor mcp/<name>` — checks binaries on PATH, entrypoint exists, and that
   `auth` variables are set (a warning unless `--strict`).
3. `agentworks run mcp/<name>` — interactive MCP inspector against the real server (needs a
   terminal; inherits your shell's env, so export `auth` vars first). Local servers only.
4. `agentworks validate`, then `agentworks export`.
