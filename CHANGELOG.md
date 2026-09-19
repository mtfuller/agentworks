# Changelog

All notable changes. See [COMPATIBILITY.md](COMPATIBILITY.md) for what counts as breaking.
Before 1.0, a minor version may include breaking changes; each is marked **Breaking**.

## Unreleased (heading to v0.18)

Milestone 1 of [PLAN-1.0.md](PLAN-1.0.md): freeze the formats.

### Added
- **Field registry.** `internal/artifact/fields.go` lists every supported frontmatter field
  per kind, and [docs/reference/frontmatter.md](docs/reference/frontmatter.md) is generated
  from it. `agentworks validate` now warns about an unknown key (previously ignored
  silently on export, so a typo like `comand:` did nothing) and rejects a supported field
  with the wrong type (for example `auth: FOO` instead of `auth: [FOO]`). Keys prefixed
  `x-` are exempt.
- **Hook handlers.** A hook can declare `handlers: [{event, matcher, command, timeout}]`
  for a per-handler matcher and timeout (seconds). The `events` + `command` form remains as
  shorthand. All four hook-capable targets support both; Gemini CLI's timeout is converted
  to milliseconds.
- **Project format.** `agentworks.yaml` has a `format:` field (`agentworks init` writes 1).
  A binary refuses a project with a newer format.
- `agentworks version --json`, reporting the supported `project_format`.
- **JSON Schemas** for every `--json` document in `docs/schemas/`, generated from the Go
  types and validated against real output in the tests.
- `COMPATIBILITY.md` and this changelog.

Milestone 2 (import completeness), first part:

- **Import MCP servers and hooks from a plugin.** `agentworks add` now turns a Claude Code
  plugin's `.mcp.json` / `plugin.json` `mcpServers` into `mcp` artifacts, and its
  `hooks/hooks.json` / `plugin.json` `hooks` into `hook` artifacts (handlers that run the
  same script are grouped). Files referenced through `${CLAUDE_PLUGIN_ROOT}` are copied into
  the artifact and the reference rewritten; a literal credential in a server's env or
  headers is never imported; a prompt-type hook, or one guarded by an `if`, is reported and
  skipped. Importing your own claude-code bundle round-trips.
- **Bundled hook scripts.** A hook command may use `${ARTIFACT_DIR}`; claude-code export
  ships the hook's files under `hook-files/<name>/` and resolves the path. Other targets
  skip such a hook with a warning.
- **Safer updates.** `update --apply` refuses to overwrite an artifact you edited since
  importing it unless `--force`; `update --diff` shows the change; `add --force` replaces an
  existing artifact. The lockfile records a `local_sha256` and the resolved GitHub `commit`.
- **More sources** for `agentworks add` and marketplace entries: `npm:@scope/name[@version]`
  (checksum-verified), and any git remote over https or ssh (`https://gitlab.com/owner/repo`,
  `git@host:owner/repo.git`, with `#<ref>[:<path>]`). GitHub Copilot (Agent Plugins) plugins
  are imported as well as Claude Code ones, including `mcp.json`, `com.github.copilot/`
  agents and hooks.
- `SECURITY.md` states the import trust model.

### Changed
- **Claude Code MCP export** locates a plugin's server files through `${CLAUDE_PLUGIN_ROOT}`
  (as Claude Code's docs require) instead of assuming the server starts in the plugin
  directory. This was likely broken for installed plugins.
- Extracted and copied files keep their execute bit (and only that): imported scripts used
  to lose it, so a hook or server exec'd directly would not run.
- Hooks that share an event and matcher are merged into one matcher block in Claude Code's
  `hooks.json`, instead of one block per hook artifact.
- The security lint reads every hook handler's command, not just `command`.

### Fixed
- `update` failed for any artifact added with `--namespace`, because it re-planned under the
  source's default namespace.
- The tests wrote `dist/` into `tests/` and it was committed by mistake; removed and ignored.

### Removed
- **Breaking:** the per-artifact `targets:` key is deprecated (warned about). Targets live in
  `agentworks.yaml`. The example project no longer sets it.

## v0.17.0

### Added
- `--json` output on `list`, `validate`, `doctor`, `test`, `eval`, `status`, `targets`, and
  `marketplace --check`; a `schema_version`/`command`/`ok` envelope on each.
- `agentworks marketplace --check` and `agentworks status --fail-on-drift`.
- `action.yml` (a composite GitHub Action) and `agentworks init --ci`.
- Linux release binaries.
- MCP servers: `transport` (`stdio`, `http`, `sse`), `args`, `env`, `url`, `headers`; working
  dependency-free scaffolds in Python, Node, and TypeScript, plus `npx-wrapper` and
  `remote-http` templates; a built-in smoke test in `agentworks test`.

### Changed
- **Breaking:** the `tool` artifact kind is now `mcp` (directory `mcp/`, manifest `mcp.md`).
- `validate`: a hook or mcp server's shell command is now an informational notice, not a
  warning, so `--strict` passes on a working project. Risky command shapes still count.

### Removed
- **Breaking:** the `workflow` kind and the `m365-copilot` target.

### Fixed
- Cursor's `.cursor/mcp.json` and `.cursor/hooks.json`, and Gemini CLI's hooks fragment, were
  overwritten per artifact so only the last survived; they now merge.

## v0.16.0 and earlier

See the git history and tags.
