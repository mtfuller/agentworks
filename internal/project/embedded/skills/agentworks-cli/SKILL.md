---
name: agentworks-cli
description: >
  How to scaffold, validate, build, test, run, export, import, and publish this project's
  agents, skills, MCP servers, and hooks with the agentworks CLI. Use whenever asked
  to add, change, check, ship, or import an artifact in this project.
---

# Using the agentworks CLI

This project is managed by [AgentWorks](https://github.com/mtfuller/agentworks). Prefer
these commands over hand-writing or hand-editing files under `agents/`, `skills/`,
`mcp/`, or `hooks/` directly. For what to put *inside* each kind, load the
matching `agentworks-author-<kind>` skill.

## Authoring

- `agentworks new <kind> [name] --description "..."` — scaffold an artifact (`kind` is
  `agent`/`skill`/`mcp`/`hook`). Omit `name`/`description` in an interactive
  terminal to get a wizard. Flags: `--from-template <id>` (start from a built-in template),
  `--version <v>` (default `0.1.0`), `--target <id>` (repeatable). A namespaced name like
  `team-a/csv-analyzer` lands at `skills/team-a/csv-analyzer/`.
- `agentworks templates [kind]` — list built-in starter templates.
- `agentworks list [kind]` — table of discovered artifacts.
- `agentworks validate [path]` — structural checks (required fields, kind-specific rules,
  eval file syntax, field types) plus description-quality warnings, and a warning for any
  unrecognized frontmatter key (ignored on export, so usually a typo; prefix your own keys
  `x-`). `--strict`
  makes warnings fail. Run after every hand edit.

## Verifying

- `agentworks build [path]` — run the `build:` command an artifact declares.
- `agentworks test [path]` — run the `test:` command an artifact declares. An mcp server
  with no `test:` gets a smoke check (start it, MCP handshake, list tools).
- `agentworks doctor [path]` — static preflight: are the declared `command:`/`test:`/
  `build:`/`eval_runner:` binaries on PATH, does the `entrypoint:` exist, are an mcp
  server's `auth:` env vars set. Never executes anything. `--strict` fails on missing env vars.
- `agentworks run <mcp-path>` — start a local mcp server and inspect it interactively
  (interactive terminal only; actually executes the command).
- `agentworks eval [path] [--case <name>]` — run behavior-eval cases under `evals/`. See
  the `agentworks-evals` skill.

With no path, `build`, `test`, `doctor`, and `eval` cover every artifact that declares
something to run.

## Shipping

- `agentworks targets` — matrix of which vendor targets support which kinds, and whether a
  real exporter exists for the combination.
- `agentworks export [path...]` — bundle to each target in `agentworks.yaml`'s `targets:`
  (override with `--target <id>`). `--namespace <ns>` bundles one plugin per namespace
  (`.` = your own un-namespaced artifacts). `--format skills.zip` / `--format skill` export
  every skill as one zip or as individual `.skill` files, no target needed. Export first runs
  every exported artifact's `build:` command and aborts if any fails (`--no-build` skips
  this), so bundled output is always fresh. Output goes to
  `dist/`, which is gitignored. Targets are project-level; artifacts don't declare their own.
- `agentworks status [path]` — offline drift check of `dist/` against `agentworks.lock`:
  `in sync`, `modified` (exported output was hand-edited; re-export discards it), `stale`
  (source changed since export), or `missing`.
- `agentworks marketplace` — publish the whole project as a plugin marketplace repo:
  committed `plugins/` directories plus `.claude-plugin/marketplace.json` and
  `.github/plugin/marketplace.json`. `--single` for one plugin total.

## Importing

- `agentworks add <url>` — import a published Agent Skill or a Claude Code or GitHub Copilot
  plugin. Accepts `owner/repo`, a repo/tree/blob URL, an archive URL, `npm:@scope/name`, or
  any git remote over https/ssh (`https://gitlab.com/owner/repo`, optionally `#ref:path`). Filed under a namespace (GitHub
  owner by default, `--namespace` to override). Also imports a plugin's MCP servers and
  hooks (script files it references come along; a literal credential in a server's env or
  headers is never imported). Pins the source and exact commit in `agentworks.lock`
  (commit that file). Any declared shell command asks for confirmation; read it first.
  `--force` replaces an artifact that already exists.
  With no argument in an interactive terminal, opens the plugin browser.
- `agentworks update [path...]` — compare imported artifacts against upstream. Reports
  drift only; `--diff` shows what would change; `--apply` overwrites local copies with the
  fresh upstream content, but not one you have edited since importing it unless `--force`.

## Other

- `agentworks tui` — full-screen browser for all of the above.
- `agentworks version`.
- Global flags: `-p, --project <path>` (any path inside the project; resolved upward like
  `git`), `-v, --verbose`, `-l, --log-level`.

## Workflow for a typical change

1. `agentworks new <kind> <name> --description "..."`, or edit an existing artifact.
2. Fill in the kind-specific fields (see the `agentworks-author-<kind>` skill).
3. `agentworks validate`, then fix errors and heed description warnings.
4. `agentworks build <path>` (export also runs it) / `agentworks test <path>` if declared; `agentworks doctor <path>`
   if either can't find a binary.
5. `agentworks export` (rebuilds first; a failed build stops it). Never hand-edit the output in `dist/` — re-export instead.
