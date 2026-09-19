# Security

## Reporting a problem

Please report a vulnerability privately through GitHub's "Report a vulnerability" on this
repository's Security tab, rather than in a public issue.

## What AgentWorks does and doesn't protect you from

AgentWorks is a local tool. It runs commands you author (`test:`, `build:`, an MCP server's
`command:`) and exports commands that *other programs* will later run with your permissions
(a hook's command, an MCP server's command). The security question is mostly about what
happens when that content comes from somewhere else, through `agentworks add`.

### Importing (`agentworks add`, `agentworks update`)

**What is checked.**
- **Shell commands are surfaced and need consent.** If imported content declares a command,
  it is printed and you must confirm (`--yes` skips the prompt for scripted use). A
  non-interactive run without `--yes` refuses, rather than proceeding silently.
- **Hostile shapes are flagged.** A command that pipes a download into a shell, decodes a
  base64 payload, opens a `/dev/tcp` socket, uses `sudo`, sets a setuid bit, and similar, is
  called out as a warning. This is a denylist scan, **not a sandbox**: it catches sloppy or
  obviously hostile commands, not a determined author who obfuscates. A clean result is not
  a safety guarantee.
- **Literal credentials are never written to your project.** An MCP server whose `env` or
  `headers` holds a literal secret is not imported; it is reported by field name only (the
  value is never echoed). Secrets belong in environment variables, referenced as `${VAR}`.
- **Content is pinned.** Every import records the source, the exact GitHub commit it
  resolved to, and a hash of what was fetched in `agentworks.lock`. `agentworks update` shows
  when upstream has moved and never overwrites your local edits without `--force`.
- **Archives are extracted defensively.** Path traversal ("Zip Slip"), absolute paths,
  symlinks, and oversized archives are rejected or skipped, and only the execute bit of a
  file's permissions is kept: no setuid, setgid, sticky, or group/world-write bits.
- **Nothing runs at import time.** Importing copies files and writes config. It does not
  execute anything from the source.

**What is not checked.**
- **The content itself is trusted once you confirm.** AgentWorks does not verify the author,
  sign-off, or reputation of a source, and it does not verify signatures. Pinning a commit
  tells you *what* you got, not that it is safe. Read what you import, especially any command
  it declares and any script it bundles, before you export and install it.
- **An imported script can do anything you can.** Confirming a command means confirming its
  effect, including whatever bundled scripts it calls.
- **Instructions are not commands, but they steer an agent.** An imported skill or agent is
  text that a model will follow. AgentWorks does not check it for prompt-injection or for
  instructions you would not want your agent to follow.

**Recommendations.** Prefer sources you trust and can read; import at a specific commit
(`github.com/owner/repo/tree/<sha>`) rather than a branch you don't control; review
`agentworks update --diff` before `--apply`; commit `agentworks.lock`.

### Exporting

Exported configuration never contains a secret you declared under `auth`: those are passed as
`${VAR}` references that the target expands from the real environment. `validate` rejects a
literal credential in a sensitive header of an MCP server. Exported hooks and MCP servers
still run whatever command you wrote, with the permissions of the harness that runs them.

### `agentworks run` and `agentworks test`

These start your MCP server as a real process with your real environment (including any
`auth` variables you have set). `agentworks doctor` is the side-effect-free alternative: it
only checks that commands resolve and variables are set.
