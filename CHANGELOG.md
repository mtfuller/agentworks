# Changelog

All notable changes. See [COMPATIBILITY.md](COMPATIBILITY.md) for what counts as breaking.
Before 1.0, a minor version may include breaking changes; each is marked **Breaking**.

## v0.21.0

Milestone 6 of [PLAN-1.0.md](PLAN-1.0.md): vendor conformance and platform.

### Added
- **Publishing metadata** in `agentworks.yaml`: `version`, `author`, `license`, `homepage`,
  `repository`. Carried into plugin manifests and `marketplace.json`.
- **Schema conformance tests**: every Copilot export is validated against the vendored Agent
  Plugins 1.0.0 `plugin` and `mcp` schemas.
- **Vendor conformance job** (`AGENTWORKS_CONFORMANCE=1`, nightly workflow): `claude plugin
  validate --strict`, Copilot `plugin marketplace add`, Gemini `extensions install`.
- `agentworks targets` shows each target's format and when it was last verified.
- `--json` for `add`, `update`, `export`, and `build` (schemas in `docs/schemas/`).
- `doctor` reports a missing `sh`. macOS and Windows CI legs, `golangci-lint` and
  `govulncheck` in CI, and an experimental `windows/amd64` release binary.

### Fixed
- **Marketplaces Claude Code and Copilot rejected**: `marketplace.json` had no `owner`, and
  carried a `$schema` that doesn't exist. Both are corrected; both real CLIs now accept it.
- Copilot `mcp.json` used `http` for remote servers (the schema says `streamable-http`) and
  wrote `env` on remote entries (not allowed).
- Plugin names are slugged to the vendors' allowed pattern.
- Local MCP servers exported to Cursor and Gemini CLI now ship their files, so the entry can
  actually run.
- A whole-project export to Cursor or Gemini no longer leaves removed servers or hooks in the
  merged files.

## v0.20.0

Milestone 5 of [PLAN-1.0.md](PLAN-1.0.md): evals v2.

### Added
- **A json runner protocol.** `eval_protocol: json` (frontmatter, or `eval.protocol` in
  `agentworks.yaml`): the runner prints one object, `{text, tool_calls, activated, usage}`.
  The text protocol remains the default, so existing runners are unaffected.
- **Trace assertions:** `tool_called`, `tool_not_called`, and `tool_args` (exact `equals`, or
  regex `matches`, on a call's arguments), and `activated` / `not_activated`.
- **Trigger cases:** `should_trigger: true|false` tests whether an artifact is chosen for a
  prompt, i.e. whether its description works, and says which way a failure is wrong.
- **Rubrics** graded by a judge command (`judge_runner`, or `eval.judge_runner`) that reads a
  JSON request and prints `{pass, reason}`. AgentWorks still never calls a model.
- **Reliability:** `runs` and `pass_threshold` for nondeterministic cases, a per-run `timeout`
  (default 120s), and `--junit <file>` for CI test reporting. `eval --json` gains `runs`,
  `passes`, `duration_ms`, `usage`, and a `skipped` count (additive).
- The runner and judge see `AGENTWORKS_ARTIFACT_NAME`, `_KIND`, `_DIR`, and
  `AGENTWORKS_EVAL_CASE`, `_PROTOCOL`, `_ROLE`.
- `examples/eval-runners/`: a Claude Code json-protocol runner and a rubric judge, tested
  against fixtures. They fail loudly on an error from `claude`, including an authentication
  failure, which `claude -p` reports as an ordinary-looking result.

### Changed
- **Breaking:** eval case files are parsed strictly. An unknown key (for example a typo like
  `contians:`), a case that asserts nothing, or a duplicate case name is now an error instead
  of a case that silently passes for any response.
- A timed-out runner is killed along with every process it started, instead of leaving them
  running.
- The starter example's `researcher` agent demonstrates the json protocol.

## v0.19.0

Milestone 4 of [PLAN-1.0.md](PLAN-1.0.md): MCP transports and test coverage.

### Added
- **Remote MCP servers in `run` and `test`.** `agentworks run` connects to an http/sse mcp
  artifact and `agentworks test --remote` smoke-tests one, in addition to local stdio
  servers. `internal/mcpclient` now sits on a `Transport` interface with three
  implementations: stdio, streamable HTTP (JSON or SSE replies, session id and protocol
  version echoed, DELETE on close), and legacy SSE.
- **Credential safety for remote servers.** Headers are never written to the traffic log or
  an error message; a URL's query is stripped from errors; a header referencing an unset
  `${VAR}` is left out rather than sent half-expanded; cross-host redirects are refused, and
  an SSE `endpoint` on another host is rejected, because either would forward your headers.
- **Resources and prompts in the inspector.** A server that offers them gets a section for
  each (keys `1`/`2`/`3`): read a resource, render a prompt (its arguments in a form). The
  smoke test also reports how many resources and prompts a server lists.
- Coverage floors enforced in CI (`scripts/check-coverage.sh`) and a scaffold end-to-end job
  (`scripts/scaffold-e2e.sh`, its own workflow, also nightly).

### Changed
- Coverage: `internal/inspector` 16% → 83%, `internal/mcpclient` 62% → 90%,
  `internal/tui` 67% → 80%, `cmd` 30% → 75%. Command behavior is now tested in process.
- The TUI's plugin import and preview show an import's warnings (an ignored field, a file a
  command references that wasn't in the plugin), not only what was skipped.

## v0.18.0

Milestones 1 and 2 of [PLAN-1.0.md](PLAN-1.0.md): freeze the formats, then import completeness.

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

**Import completeness (M2):**

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
