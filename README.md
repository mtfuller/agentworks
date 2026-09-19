# AgentWorks

A local-first, vendor-agnostic tool for building agent contexts, skills, MCP servers,
and hooks — author them once as plain files, then export target-specific artifacts
for the AI harnesses you actually use (Claude Code, ChatGPT, GitHub Copilot,
Cursor, Gemini CLI, and others).

## Why

The industry has started standardizing pieces of the agent-tooling stack (e.g.
`AGENTS.md`), but most of it — skills, MCP servers, hooks, agent definitions — is
still vendor-specific, and that's likely to stay true for a while. AgentWorks doesn't
bet on any one vendor winning; it lets you build against a vendor-agnostic core model
and generate whatever vendor-specific format you need from it, so switching or
supporting multiple harnesses doesn't mean maintaining N copies by hand.

See [AGENTS.md](AGENTS.md) for the guiding scenarios and how the project is organized
for agentic development.

## Quick Start

### Prerequisites

- Go 1.24 or higher
- [Task](https://taskfile.dev) (optional, for build automation)

### Installation

```bash
git clone git@github.com:mtfuller/agentworks.git
cd agentworks
task build
./agentworks --help
```

### Try it

```bash
./agentworks init my-project
cd my-project
../agentworks new skill csv-analyzer --description "Analyze a CSV and flag rows that stand out." --target claude-code
../agentworks list
../agentworks validate
../agentworks targets
../agentworks export skills/csv-analyzer --target claude-code --out dist
../agentworks tui
```

Or look at [`examples/starter-project`](examples/starter-project) — a checked-in
project with one of each artifact kind, including two real, tested implementations (a
CSV-outlier skill and a Jira-fetching MCP server) — to see a finished example
without building one yourself.

## A project, on disk

`agentworks init` creates a project manifest, one directory per artifact kind, and an
`AGENTS.md` + a set of `.agents/skills/` (CLI usage, one authoring skill per artifact
kind, and evals) so a coding agent working in the project immediately knows how to drive
the CLI and write each kind (none is overwritten if it already exists):

```
myproject/
├── agentworks.yaml       # project manifest: name, description, targets
├── AGENTS.md             # guidance for coding agents working in this project
├── .agents/
│   └── skills/
│       ├── agentworks-cli/           # how to scaffold/validate/test/export with agentworks
│       ├── agentworks-author-agent/  # (likewise -skill, -mcp, -hook)
│       └── agentworks-evals/         # writing and running behavior evals
├── agents/
├── skills/
├── mcp/
└── hooks/
```

Each artifact is a directory containing one `<kind>.md` file — YAML frontmatter plus a
Markdown body, the same shape as Claude Code's own `SKILL.md` — alongside whatever
supporting files it needs (scripts, tests, samples, resources). It's navigable in a
plain text editor; no build step is needed to read or edit it. For example:

```
skills/csv-analyzer/
├── skill.md
├── scripts/main.py
├── tests/test_main.py
└── samples/sample.csv
```

```yaml
---
kind: skill
name: csv-analyzer
description: Analyze a CSV file and flag rows that stand out.
version: 0.1.0
targets: [claude-code, chatgpt]
entrypoint: scripts/main.py
test: python3 -m unittest discover -s tests -p "test_*.py"
---

# CSV Analyzer

<instructions / prompt body the vendor sees>
```

`agents/`, `mcp/`, and `hooks/` follow the same `<kind>.md` shape, each with a few
kind-specific frontmatter fields (an MCP server's `command`/`args`/`env`/`auth`, or its
`transport`/`url`/`headers` for a remote one -- see "MCP servers" below -- and a hook's
`handlers`, or the `events`/`command` shorthand). Every supported field is listed in
[docs/reference/frontmatter.md](docs/reference/frontmatter.md); `agentworks validate` warns
about any other key (it would be ignored on export -- usually a typo) and rejects a
supported field with the wrong type. Prefix your own metadata keys with `x-`. An agent can also set `tools:` (a list) and `model:` (a tier) --
a small, closed, vendor-agnostic vocabulary (`agentworks validate` lists the
recognized values) that `agentworks export` maps to each target's real shape: Claude
Code's and Gemini CLI's `tools:`/`model:` subagent frontmatter. GitHub Copilot's and
Cursor's custom-agent specs don't confirm these fields yet, so that mapping is
deliberately deferred (see AGENTS.md).

There is no separate "workflow" kind: an agent that delegates to other agents and calls
MCP servers, described in its own instructions, is the same thing, and the harness's
own agent loop runs it. See `examples/starter-project/agents/software-factory`.

A skill or agent can also have an `evals/` directory (a starter one is scaffolded
automatically) of `<kind>.md`-adjacent YAML case files for `agentworks eval` -- see
that command below.

An artifact can optionally be namespace-scoped -- `agentworks new skill
team-a/csv-analyzer` sets `namespace: team-a` in its frontmatter and nests it at
`skills/team-a/csv-analyzer/skill.md` instead of `skills/csv-analyzer/skill.md` -- so
two teams (or two projects merged into one registry) can each have their own
`csv-analyzer` without colliding. Reference it the same qualified way everywhere else
an artifact is named: `agentworks validate`/`export`'s path argument. An unnamespaced artifact is unaffected either way.

`agentworks.yaml` also carries a `format:` number (written by `init`; absent means 1). A
binary refuses a project whose format is newer than it understands, so an old install never
misreads a new project. See [COMPATIBILITY.md](COMPATIBILITY.md) for what is kept stable.

`agentworks add`/`export` write an `agentworks.lock` at the project root -- see
"Drift and supply-chain safety" below.

## Sharing code between Node artifacts

Every artifact -- including a `node-skill`/`node-mcp` -- is a self-contained,
independently-copyable leaf directory (its own `package.json`, its own
`node_modules`), so `agentworks export`/`agentworks add` can move one in or out
without dragging along a shared workspace root. That's deliberate, not an oversight
(see AGENTS.md) -- but it still leaves the question of how to share real logic
between several small Node artifacts without duplicating it everywhere. The
recommended pattern:

1. Put shared code in its own ordinary npm package under `packages/<name>/` at the
   project root. AgentWorks never looks there -- `list`/`validate`/`test`/`build`/
   `export` only ever walk `agents/ skills/ mcp/ hooks/` -- so it's just
   a normal npm package with its own `package.json` and its own tests (plain `npm
   test`, run directly rather than through `agentworks test`, since it isn't an
   artifact).
2. From a Node artifact, depend on it like any other package during development, via
   a relative `file:` reference in that artifact's own `package.json`:
   ```json
   "dependencies": {
     "@myproject/shared-lib": "file:../../packages/shared-lib"
   }
   ```
   `npm install` resolves this to a symlink, so edits to the shared package are
   picked up immediately -- nothing to publish for local dev.
3. A `file:` dependency is a symlink into a path that only exists inside this
   project, so it won't survive the artifact directory being copied out by
   `export`/`add`. Give the artifact a `build:` command that bundles the shared code
   in first (e.g. `npm install && npx esbuild src/index.js --bundle --platform=node
   --outfile=dist/index.js`), and point its `command:`/`entrypoint:` at the bundled
   output rather than the raw source. Run `agentworks build <path>` before
   `agentworks export` -- export itself doesn't need anything special, it already
   just copies whatever's in the directory (`node_modules` is never included, whether
   or not you use this pattern).

The `node-ts-skill`/`node-ts-mcp` templates (`agentworks new mcp <name>
--from-template node-ts-mcp`) already set this up out of the box -- TypeScript
source, a `tsconfig.json`, and a `build:` command that type-checks with `tsc` and
bundles with `esbuild` into a single `dist/` output, which is exactly what step 3
above needs. See `agentworks templates` for the full list.

## MCP servers

An `mcp` artifact registers an [MCP](https://modelcontextprotocol.io) server that gives an
agent callable tools. Every export target that supports MCP (Claude Code, GitHub Copilot,
Cursor, Gemini CLI) generates its own registration from the same frontmatter.

```bash
agentworks new mcp jira-fetch --description "Fetch a Jira ticket by key."   # a working server
agentworks test mcp/jira-fetch      # its tests, or a built-in handshake + tools/list smoke check
agentworks run mcp/jira-fetch       # inspect the live server's tools interactively
```

The default scaffold is a complete, dependency-free Python server with one example tool, so
`test` and `run` work before you write anything. Templates (`agentworks templates mcp`):
`api-wrapper`, `cli-wrapper`, `node-mcp`, `node-ts-mcp` (each a working server), `npx-wrapper`
(register a published package, no code), and `remote-http` (register a hosted server by URL).

A local server runs a process:

```yaml
kind: mcp
name: jira-fetch
command: python3 src/server.py   # run via `sh -c`; or set `args:` to exec command directly
args: []                         # e.g. command: npx, args: [-y, some-server@1.2.3]
auth: [JIRA_API_TOKEN]           # secret env vars -- exported as ${VAR}, never as values
env: {JIRA_SITE: example}        # non-secret env vars
```

A remote server is just an endpoint:

```yaml
kind: mcp
name: docs-search
transport: http                  # or sse; default is stdio
url: https://example.com/mcp
headers: {Authorization: "Bearer ${DOCS_TOKEN}"}
auth: [DOCS_TOKEN]
```

`agentworks validate` checks the transport, that stdio has a `command` and http/sse a
`url`, and rejects a literal credential in a sensitive header so no secret is written into
exported config.

`agentworks run` and `agentworks test` work for both kinds. A local server is started as a
process; a remote one is reached over its transport (streamable HTTP, or the older SSE), with
its headers' `${VAR}` references expanded from your environment. The client follows the spec
closely enough to be a real check: it echoes the session id and protocol version, refuses to
follow a redirect to a different host (which would forward your headers), and keeps credentials
out of error messages and the traffic log. `agentworks test` smoke-tests a remote server only
with `--remote`, since that makes network calls. When a server also offers resources or
prompts, the inspector adds a section for each (keys `1`, `2`, `3`).

## Commands

| Command | What it does |
| --- | --- |
| `agentworks init [path]` | Scaffold a new project (`agentworks.yaml`, the 4 kind directories, and an `AGENTS.md` + `.agents/skills/` teaching a coding agent how to drive this CLI and author each artifact kind in the project). Pass `--target` (repeatable) to set the project's export targets, or leave it out in an interactive terminal for a prompt. Targets live only in `agentworks.yaml` -- artifacts don't declare their own -- and `agentworks export` reads them, so it needs no `--target` once they're set. Pass `--ci` to also write a GitHub Actions workflow that runs the project's checks. |
| `agentworks new <kind> [name]` | Scaffold a new agent/skill/mcp/hook. Give `--description` (and kind/name) for a non-interactive run; leave any out in a terminal and a short wizard fills in the rest. Pass `--from-template <id>` to start from a curated built-in template instead of the generic blank scaffold (see `agentworks templates`) — its own description covers you if you don't pass `--description`. |
| `agentworks templates [kind]` | Table of the built-in starter templates `--from-template` can scaffold from (two for hook, three for agent, four for skill, six for mcp: e.g. an MCP server's `api-wrapper`/`cli-wrapper` (Python), `node-mcp`/`node-ts-mcp` (Node.js or TypeScript -- the TypeScript variant bundles with esbuild via a `build:` command), `npx-wrapper`, or `remote-http`; or a skill's `node-skill`/`node-ts-skill`). Pass a kind to filter. |
| `agentworks list [kind]` | Table of the project's discovered artifacts. |
| `agentworks validate [path]` | Parse and validate one artifact or the whole project. Beyond the generic checks (name/description/kind), also catches export-readiness gaps per kind: a hook's `events`/`command` must be set together, and an MCP server needs a known `transport` with a `command` (stdio) or `url` (http/sse) and no literal credentials in sensitive headers. Also lints description quality — too long (over the [Agent Skills spec](https://agentskills.io/specification)'s 1024-character limit), too short/vague, redundant with the name, or overlapping heavily with another same-kind artifact's description (checked project-wide) — and flags a hook/mcp `command` shaped like something hostile (see "Drift and supply-chain safety") — printed as warnings that don't fail the command unless `--strict` is passed. Merely having a command is shown as a notice, never a warning, so a working project passes `--strict`. With `--json`, a machine-readable report (see "Continuous integration"). |
| `agentworks build [path]` | Run the `build:` command an artifact declares in its frontmatter (any language — AgentWorks just shells out to it, e.g. installing dependencies, compiling, or bundling before the artifact can run or be exported). With no path, runs every artifact that declares one. |
| `agentworks test [path]` | Run the `test:` command an artifact declares (an MCP server with none gets a built-in smoke check: connect, run the MCP handshake, list its tools, resources, and prompts; `--remote` includes remote http/sse servers, which makes network calls) in its frontmatter (any language — AgentWorks just shells out to it). |
| `agentworks eval [path]` | Behavior-test a skill/agent: for each case under its `evals/` directory, pipe the case's `prompt` to the artifact's `eval_runner` command (or the project's `eval.default_runner`) and check what comes back. AgentWorks never calls a model itself — the runner (and a rubric judge) is your own shell command, the same "orchestrate, don't execute" split `agentworks test` follows. See "Behavior evals" below for the two runner protocols, the assertions (text, tool-call trace, activation, rubric), trigger cases, repeated runs, and timeouts. `--case <name>` re-runs one case; `--junit <file>` writes a JUnit XML report for CI; `--json` gives a machine-readable report. With no path, runs every artifact with an `evals/` directory; artifacts without one, or without a runner configured, are skipped rather than failed. |
| `agentworks doctor [path]` | A static, side-effect-free preflight check: resolves every declared shell command's (`command:`/`test:`/`build:`/`eval_runner:`) interpreter/binary against `PATH`, checks a declared `entrypoint:` file actually exists, and checks an MCP server's `auth:` environment variables are set (a warning, not a failure, unless `--strict` — they're only needed to actually call the server, not to discover what it offers) and that no scaffold `REPLACE-WITH-...` placeholder is left. With no path, checks every artifact in the project. Run this before `agentworks run` if you're not sure the command/environment is even set up. |
| `agentworks run <mcp>` | Connect to an MCP artifact's server -- starting its local `command` as a real process, or reaching its remote `url` over http/sse -- and open a full-screen inspector: browse the tools it exposes, fill in and submit a call from a form generated off each tool's `inputSchema`, and see the result — the same way an agent actually would, instead of only unit-testing the server's logic with mocked calls. A server that offers resources or prompts gets a section for each (read a resource, render a prompt with its arguments). Also shows a call-history pane and the raw JSON-RPC/stderr traffic (reachable even from a connection-failure screen, so the real cause isn't hidden behind a generic protocol error; never includes headers). Only mcp artifacts qualify; unlike `export`'s `${VAR}` placeholders, this actually connects with your real environment. Needs an interactive terminal. |
| `agentworks targets` | Print the capability matrix: which artifact kinds each vendor target supports, and whether a real exporter exists yet. |
| `agentworks export [path...]` | Bundle the project into a plugin for each target in `agentworks.yaml`'s `targets:` (`--target <id>`, repeatable, overrides it; required only when none are configured). Everything lands in one plugin named after the project; `--namespace <ns>` (repeatable; `.` = your own un-namespaced artifacts) writes one plugin per listed namespace instead, and `--format skills.zip` / `--format skill` export every skill as one `.zip` or as individual `.skill` files (vendor-neutral, no target needed). Output goes to `dist/<target>/<plugin>/`. Paths restrict the export to those artifacts. Before exporting, every artifact being exported that declares a `build:` command is built first (so bundled output like `dist/main.js` is fresh); if any build fails, nothing is exported. `--no-build` skips this. Vendors with no plugin format (`chatgpt`, `cursor`, `gemini-cli`) get each artifact exported on its own. Per-vendor detail: each artifact is translated to the vendor's native format. `claude-code` and `github-copilot` have a real exporter for all four kinds: skills (the shared [Agent Skills](https://agentskills.io/specification) format, also used by `chatgpt`), MCP servers as a server registration (`.mcp.json` / Agent Plugins' `mcp.json`, `auth` env vars passed through as `${VAR}` references, never literal secrets), agents as a subagent file (`agents/<name>.md` / `com.github.copilot/agents/<name>.agent.md`), hooks as a lifecycle-event handler (`hooks/hooks.json` / `com.github.copilot/hooks/hooks.json`) (remote http/sse servers register just their URL and headers). `cursor` and `gemini-cli` have a real exporter for skills/agents/MCP servers/hooks, each writing loose, project-scoped files rather than a plugin: Cursor a project rule (`.cursor/rules/<name>.mdc`), a subagent file (`.cursor/agents/<name>.md`, name/description only -- Cursor's docs don't confirm a tools/model mapping yet), `.cursor/mcp.json`, and `.cursor/hooks.json` (the last two are merged into as each artifact is exported, so N servers or hooks all land in the one file); Gemini CLI a distributable extension directory (`gemini-extension.json` + `GEMINI.md` for a skill, or just an `mcpServers`-only manifest for a bare MCP server), a subagent file (`.gemini/agents/<name>.md`, with a real tools/model mapping), and a `.gemini/settings.json` hooks fragment meant to be merged by hand (Gemini CLI hooks live only in a shared settings file, not an extension-scoped format). See AGENTS.md, "Cursor and Gemini CLI: what's real vs. deferred," for the full reasoning. `agentworks targets` shows the full matrix. Bundled skills/agents are filed by bare name (`skills/<name>/`), or `<namespace>-<name>` when two namespaces share a name; an MCP server member's own `src/`-relative command is namespaced under `mcp/<name>/` so multiple servers' files don't collide. |
| `agentworks add <url>` | Import a published skill or Claude Code plugin into this project — the reverse of `export`. Accepts an `owner/repo` GitHub shorthand, a full `github.com` repo/tree/blob URL, a `raw.githubusercontent.com` file URL, a direct `.zip`/`.tar.gz` archive URL (including agentskills.codes's download links), `npm:@scope/name[@version]` (checksum-verified against the registry), or any git remote over https or ssh (GitLab, Bitbucket, self-hosted; `https://host/owner/repo` or `git@host:owner/repo.git`, with an optional `#<ref>[:<path>]` suffix; uses the `git` command). Both Claude Code plugins and GitHub Copilot (Agent Plugins) plugins are understood. A bare Agent Skill (`SKILL.md` at its root) becomes one skill artifact, supporting files included. A Claude Code plugin (`.claude-plugin/plugin.json` at its root) decomposes into one artifact per skill, agent, and MCP server it contains, plus hook artifacts (handlers that run the same script are grouped into one). Files an MCP server or hook references through `${CLAUDE_PLUGIN_ROOT}` are copied into the artifact and the reference rewritten, keeping scripts executable. Anything that can't be represented faithfully -- a prompt-type hook, a hook guarded by an `if`, a server with a literal credential in its env or headers -- is reported and not imported; a literal credential is never written to your project. The exact commit a GitHub import resolved to is pinned in `agentworks.lock`. A name that doesn't fit AgentWorks' slug rules is converted automatically, with the original preserved in a `source:` provenance block alongside where it came from. `--name` overrides the derived name (single-skill imports only); `--dry-run` shows what would be imported without writing anything. If any imported content declares a shell `command` (see "Drift and supply-chain safety"), it's printed and you're asked to confirm — `--yes` skips that prompt for scripted use. Every import is pinned in `agentworks.lock`. Everything imported is filed under a namespace so plugin-sourced artifacts stay distinguishable from your own: by default the GitHub owner (`obra/superpowers` lands at `skills/obra/<name>`, shown as `@obra/<name>`), or `--namespace` to choose one. With no URL and no argument, it launches the plugin browser TUI in an interactive terminal. `--force` replaces an artifact that already exists. |
| `agentworks update [path...]` | Check artifacts imported with `add` for upstream changes: re-fetches each locked source and compares its content hash against what was pinned at import time. Report-only by default; `--apply` overwrites a changed artifact with the fresh content (refusing rather than silently renaming/moving it if upstream itself renamed the artifact) and updates the pin, subject to the same shell-command confirmation gate as `add` (`--yes` to skip it). With no path, checks every import in `agentworks.lock`. Your own edits are protected: an artifact you changed since importing it is not overwritten by `--apply` unless you pass `--force`, and `--diff` shows what would change. |
| `agentworks status [path]` | Fully offline check of `dist/` output against `agentworks.lock`'s export records: `in sync`, `stale` (source artifact changed, re-export), `modified` (dist was hand-edited since the last export — re-exporting discards it), or `missing`. `--fail-on-drift` exits non-zero unless everything is in sync. |
| `agentworks marketplace` | Publish this project as a plugin marketplace repo a team can point Claude Code or GitHub Copilot at directly — see "Becoming a plugin marketplace repo" below. |
| `agentworks tui` | Full-screen Bubble Tea browser: a tab per artifact kind (`tab`/`shift+tab` to switch) showing that kind's artifacts directly, drill into one with `enter` for its rendered frontmatter and body. Press `n` to scaffold a new artifact (the same wizard `agentworks new` uses, pre-filled with the current tab's kind), `e` to export the whole project (one plugin, a plugin per namespace, or skills as `.zip`/`.skill`) to the project's configured targets, `t` to run its declared `test:` command, `p` to browse plugins from the Claude Code and GitHub Copilot marketplaces -- only permissively licensed ones (MIT, Apache-2.0, BSD, ISC, Unlicense, CC0, Zlib), with a detail pane beside the list showing license, author, and exactly which skills/agents importing it would add --, or `b` to browse/search the built-in starter templates (also tabbed by kind) and create straight from one — all run right there, no dropping back to the CLI. |
| `agentworks version` | Print version/commit/build-date info. |

Global flags: `-p, --project` (path inside the project to operate on, default `.`,
resolved upward like `git` finds a repo root), `-v, --verbose`, `-l, --log-level`.
The reporting commands (`list`, `validate`, `doctor`, `test`, `eval`, `status`, `targets`,
`marketplace --check`) also take `--json`; see "Continuous integration".

## Behavior evals

`agentworks test` checks code; `agentworks eval` checks how a skill or agent *responds*. Each
case under `<artifact>/evals/*.yaml` sends a prompt to a runner (your own command: a script that
calls a model, `claude -p`, anything that reads a prompt on stdin) and asserts on what comes back.

```yaml
cases:
  - name: cites a source
    prompt: "Is library X really 2x faster?"
    runs: 5                       # repeat a nondeterministic case...
    pass_threshold: 0.8           # ...and pass if 80% of runs do
    timeout: 60                   # seconds per run (default 120)
    assert:
      contains: ["benchmark"]     # text: contains, not_contains, matches, not_matches,
      max_length: 800             #       max_length, min_length
      tool_called: [web-search]   # trace (needs eval_protocol: json)
      tool_not_called: [edit-files]
      tool_args:
        - tool: web-search
          matches: {query: "(?i)speedup"}    # regex on the argument, or
          equals: {max_results: 5}           # an exact value
      rubric: "Says whether the claim is confirmed, and cites where."   # graded by a judge
  - name: is chosen for a research request
    prompt: "Can you research whether library X is really 2x faster?"
    should_trigger: true          # would a model pick this artifact for this prompt?
  - name: is not chosen for arithmetic
    prompt: "What is 2 + 2?"
    should_trigger: false
```

**Trigger cases** test the artifact's *description*, the text a model reads to decide whether to
use it, which is the commonest way a skill fails. `should_trigger` passes or fails on whether the
runner reports the artifact as activated, and a failure says which way the description is wrong.

**Runner protocols.** Set `eval_protocol` in the artifact's frontmatter, or `eval.protocol` in
`agentworks.yaml`. `text` (the default): stdout is the response. `json`: stdout is exactly one
object, `{"text": "...", "tool_calls": [{"name": "...", "arguments": {}}], "activated": ["skill"],
"usage": {"input_tokens": 0, "output_tokens": 0}}`, which is what the trace assertions and trigger
cases read. Only `text` is required; a case that asserts on `tool_calls` or `activated` fails if the
runner didn't report it (an empty list means "none"; a missing key means "not reported"), so an
unreported trace can never pass a "not called" assertion by accident. Under `json`, logs belong on
stderr.

**Rubrics.** A `rubric` is graded by a *judge*: a command (`judge_runner` in frontmatter, or
`eval.judge_runner`) that reads `{"subject", "prompt", "response", "rubric"}` as JSON on stdin and
prints `{"pass": true|false, "reason": "..."}`. A case with a rubric and no judge fails loudly rather
than passing unchecked, and so does a judge whose output can't be read.

**Reliability.** Cases are parsed strictly: an unknown key (a typo like `contians:`) or a case that
asserts nothing is an error, not a case that passes for any response. Names must be unique. A runner
or judge that exceeds its timeout is killed along with any processes it started. Both run with
`AGENTWORKS_ARTIFACT_NAME`, `_KIND`, `_DIR`, `AGENTWORKS_EVAL_CASE`, and `AGENTWORKS_EVAL_PROTOCOL` in
their environment. Project defaults live under `eval:` in `agentworks.yaml`
(`default_runner`, `protocol`, `judge_runner`, `runs`, `timeout`); an artifact's own frontmatter wins.

`examples/eval-runners/` has reference runners for Claude Code: `claude_code.py` (a json-protocol
runner that reports tool calls and skill activations) and `claude_judge.py` (a rubric judge). They
fail loudly on an error from `claude` — including an authentication failure, which `claude -p`
reports as an ordinary-looking result — so a broken setup is never scored as the model's answer.
Their parsing is tested against fixtures; they have not been exercised against a live authenticated
run, so check them against your installed version.

## Drift and supply-chain safety

`agentworks add` and `agentworks export` both write to a single `agentworks.lock` at
the project root:

- **Imports** (`agentworks add`) are pinned by a content hash of the raw fetched
  source, plus where it came from (repo/ref/path). `agentworks update` re-fetches
  that same source later and reports (or, with `--apply`, applies) any drift — the
  "did an imported skill change upstream without me noticing" gap.
- **Exports** (`agentworks export`) are pinned by a hash of both the source
  artifact(s) and the exported output. `agentworks status` reads this back
  fully offline to tell a hand-edited `dist/` (edits export would silently discard)
  apart from a merely stale one (the source changed since the last export). `export`
  itself also warns before overwriting a hand-edited output.

Separately, `agentworks validate`, `agentworks add`, and `agentworks export` all scan
a hook/mcp server's `command` field — arbitrary shell that runs with your own permissions
the moment it's triggered/invoked — for both the fact that it exists and a small
denylist of shapes that are almost always hostile (piping a download into a shell,
a base64-decoded payload, a raw `/dev/tcp` reverse shell, `sudo`, setting a setuid
bit). `validate` and `export` only warn; `add` (and `update --apply`) require an
explicit `y`/`--yes` before writing anything that triggers a warning. This is a scan
and a confirmation gate, not a sandbox — it catches sloppy or obviously hostile
commands, not a determined obfuscator.

## Becoming a plugin marketplace repo

```bash
agentworks marketplace
```

turns the project itself into a repo a team can point Claude Code or GitHub
Copilot at directly, instead of exporting one artifact at a time. It exports
every eligible artifact into committed plugin directories under `plugins/`
(unlike `export`'s disposable, gitignored `dist/`), then writes a
`marketplace.json` listing them — `.claude-plugin/marketplace.json` for
Claude Code, `.github/plugin/marketplace.json` for GitHub Copilot, both by
default (pass `--target claude-code` or `--target github-copilot` to
generate just one). A team adds either as a plugin marketplace source (e.g.
Claude Code's `/plugin marketplace add <repo>`) and installs whatever
plugins it lists.

Skills, agents, MCP servers, and hooks are grouped into one bundled plugin per
namespace — an unnamespaced artifact lands in one plugin named after the
project, and each `namespace:` (see "A project, on disk" above) gets its
own. Pass `--single` to collapse everything into one plugin regardless of
namespace. Re-running `agentworks marketplace` regenerates `plugins/` and both `marketplace.json`
files from scratch, so it stays in sync as artifacts are added, removed,
renamed, or re-namespaced — nothing from a previous run is left behind.
Exports are recorded in `agentworks.lock` exactly like `agentworks export`,
so `agentworks status` reports on them too.

The two `marketplace.json` locations are the ones Claude Code and GitHub Copilot CLI
document: `.claude-plugin/marketplace.json`, and `.github/plugin/marketplace.json` (Copilot
CLI reads either). The Copilot one carries the [Agent Plugins](https://agent-plugins.org)
`$schema`, and every plugin directory under `plugins/github-copilot/` is an Agent Plugin.

`agentworks marketplace --check` writes nothing: it regenerates into a temporary
directory and exits non-zero if the committed `plugins/` or either `marketplace.json`
differs, so a source artifact can't change without the published marketplace following.

## Continuous integration

Every reporting command takes `--json`. Stdout is then exactly one JSON document and all
human-readable messages go to stderr, so `agentworks validate --json | jq` is safe:

```json
{ "schema_version": 1, "command": "validate", "ok": false,
  "summary": { "artifacts": 4, "failed": 1, "warnings": 0 },
  "artifacts": [ { "kind": "skill", "name": "csv-analyzer", "path": "skills/csv-analyzer",
                   "errors": ["..."], "warnings": [], "notices": [] } ] }
```

`list`, `validate`, `doctor`, `test`, `eval`, `status`, `targets`, `version`, and
`marketplace --check` support it. Every document has `schema_version`, `command`, and `ok` (true exactly when the
command exits 0); a failure that happens before a command can build its own document still
yields `{"ok": false, "error": "..."}` on stdout. `schema_version` changes only for a
breaking change (a removed or retyped field), so ignore keys you don't recognize. Paths are
project-relative, so output is stable across machines. Each document has a JSON Schema in
[docs/schemas/](docs/schemas/), generated from the Go types and checked against real output
in the tests.

The gates a CI run wants all exit non-zero on failure: `validate --strict`, `doctor`,
`marketplace --check`, and `status --fail-on-drift` (report-only without the flag).
`agentworks init --ci` writes a workflow that runs them, or add the composite action to an
existing one:

```yaml
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: mtfuller/agentworks@main        # pin to a release tag once you depend on it
        with:
          checks: validate,doctor,marketplace # also: test, eval, status
          strict: "true"
```

`marketplace` only runs when a `marketplace.json` is committed. `test` and `eval` are opt-in
because they run your own commands (and, for `eval`, whatever model your `eval_runner` calls).
Release archives for macOS and Linux (arm64 and amd64) are attached to each GitHub release.

## Development

### Running Tests

```bash
task test              # unit + integration
task test-unit
task test-integration
task coverage           # coverage.html
```

### Building

```bash
task build              # ./agentworks
task install             # to GOPATH/bin
```

### Project Structure

```
.
├── cmd/                        # Cobra commands (one file per command)
├── internal/
│   ├── artifact/               # vendor-agnostic artifact model (Kind, Frontmatter, <kind>.md parsing)
│   ├── project/                # agentworks.yaml manifest, project discovery
│   ├── scaffold/                # `new` boilerplate generation, one starter per kind, plus built-in named templates.go
│   ├── evalspec/                # `evals/*.yaml` case format + deterministic assertion checker for `agentworks eval`
│   ├── targets/                 # vendor registry (capability matrix) + Exporter interface
│   │   ├── agentskills/         # shared Agent Skills (agentskills.io) SKILL.md writer
│   │   ├── mcpconfig/           # shared MCP server-entry builder + validation (stdio, http, sse)
│   │   ├── filecopy/            # shared copy-artifact-files / zip-a-directory helpers
│   │   ├── agentcaps/           # vendor-agnostic agent tools/model vocabulary + per-vendor mapping
│   │   ├── claudecode/          # "claude-code": all 4 kinds, each a real Claude Code plugin
│   │   ├── chatgpt/             # the "chatgpt" skill exporter (wraps agentskills)
│   │   ├── githubcopilot/       # "github-copilot": all 4 kinds, each a real Agent Plugin
│   │   ├── cursor/              # "cursor": skill/agent/mcp/hook, each a loose .cursor/ file (no plugin format)
│   │   └── geminicli/           # "gemini-cli": skill/mcp as an extension, agent/hook as loose .gemini/ files
│   ├── importer/                 # the reverse of targets/: fetch + decompose a skill/plugin into local artifacts
│   ├── marketplace/               # agentskills.codes + well-known marketplace.json search (resolving to importer.Source), plus writing this project's own marketplace.json for `agentworks marketplace`
│   ├── tui/                     # Bubble Tea browser + `n`ew/`e`xport/`t`est/`a`dd/`b`rowse-templates actions
│   ├── mcpclient/                # minimal MCP stdio JSON-RPC client (initialize/tools-list/tools-call) for `agentworks run`
│   ├── inspector/                # Bubble Tea MCP inspector UI (tool list, call form, history, raw traffic log) behind `agentworks run`
│   ├── color/ logger/ spinner/ version/   # CLI-support packages from the starter template
├── examples/starter-project/    # a finished example project, one artifact of each kind
├── tests/                       # black-box CLI integration tests
├── main.go
├── Taskfile.yml
└── README.md
```

## Adding New Commands

To add a new CLI command, create a new file in the `cmd/` directory (or use the
`add-command` skill in `.claude/skills/`):

```go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/mtfuller/agentworks/internal/color"
)

var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "Description of my command",
    Run: func(cmd *cobra.Command, args []string) {
        color.Success("My command executed!")
    },
}

func init() {
    rootCmd.AddCommand(myCmd)
}
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
