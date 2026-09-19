# Importing from others

```bash
agentworks add https://github.com/owner/repo                 # a repo with skills, or a plugin
agentworks add https://github.com/owner/repo#main:plugins/x  # a ref and subpath
agentworks add npm:@scope/package                            # an npm package
agentworks add https://example.com/plugin.tar.gz             # an archive
```

Sources: GitHub, generic git, npm, archives, and Claude Code or GitHub Copilot plugin layouts.
A plugin is split into its parts: skills, agents, MCP servers (from `.mcp.json`), and hook
handlers each become a normal artifact under a namespace (`--namespace` overrides it).
Anything AgentWorks can't represent (for example a prompt-type hook) is listed as
not imported rather than dropped silently.

`--dry-run` shows what would happen; `--json` reports it for scripts.

## What is checked, and pinned

Before anything is written you see every shell command the content will run and confirm it
(`--yes` in scripts). A literal secret in an MCP server's `env` or `headers` is refused.
Each import is recorded in `agentworks.lock` with the exact commit and a content hash.

```bash
agentworks update            # report drift from upstream
agentworks update --diff     # and show what would change
agentworks update --apply    # apply; refuses to overwrite your local edits without --force
```

The full model, and its limits, is in [SECURITY.md](../../SECURITY.md).
