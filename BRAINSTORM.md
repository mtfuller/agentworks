# Brainstorm: missing capabilities

Ideas for what AgentWorks could add beyond what's implemented today. Not a roadmap —
a working list to pull from. See [AGENTS.md](AGENTS.md) for what's actually built and
what's been deliberately deferred (with reasons).

Status markers: 🎯 picked to build next, ✅ done, otherwise unprioritized.

## Distribution

- ✅ **Multi-artifact bundles.** Every `agentworks export` call today packages exactly
  *one* artifact into its own plugin. But Claude Code/Copilot plugins are meant to
  bundle a *set* of skills/agents/tools/hooks together (that's how real plugins are
  usually shipped — e.g. one "devops-toolkit" plugin with 5 skills + 2 agents, not 5
  separate single-skill plugins). Workflows already do this for one specific
  composition, but there's no general "package these N artifacts as one plugin" path.
  Reuses the plugin-writing code that already exists; the new part is feeding it a set
  instead of a singleton. Done: `agentworks export skills/s1 tools/t1 --target
  claude-code --bundle my-kit` (see [AGENTS.md](AGENTS.md)).
- ✅ **Bulk export.** `agentworks export --kind skill --target claude-code` or `--all` —
  exporting a whole project today means one manual invocation per artifact.
  `test`/`validate` already support "no path = whole project"; `export` doesn't. Done:
  `--all`/`--kind` flags on `agentworks export` (see [AGENTS.md](AGENTS.md)).

## Agent model & behavior testing

- ✅ **Enrich the agent kind.** An agent used to be just a name + prompt body —
  richer fields like tools/model were "left out rather than invented" (see the old
  comment in `internal/targets/claudecode/agent.go`). But which tools an agent may
  call and which model runs it are exactly where a vendor-agnostic model should earn
  its keep, since those differ per vendor more than the skill-file shape already
  solved. Done: a curated, closed `tools:`/`model:` vocabulary
  (`internal/targets/agentcaps`), validated by `agentworks validate`, mapped to Claude
  Code's real subagent fields and Microsoft 365's declarative-agent `capabilities`
  array (see [AGENTS.md](AGENTS.md)). GitHub Copilot's mapping is deliberately
  deferred, not overlooked — its custom-agent spec doesn't publicly confirm tools/
  model fields exist yet.
- ✅ **A lightweight eval/behavior-test mechanism.** `agentworks test` only validates
  code correctness (it shells out to a `test:` command) — skills and agents are
  prompts, not code, and "the LLM ignores the instructions" had no test at all. Done:
  `agentworks eval` (see `internal/evalspec`, [AGENTS.md](AGENTS.md)) runs an
  artifact's `evals/*.yaml` cases against a project-supplied `eval_runner` command,
  checking deterministic assertions (`contains`/`matches`/length) on its output.
  AgentWorks still never calls a model itself, matching the "vendor's own loop
  executes, AgentWorks orchestrates" principle already used for workflow export.
  Deliberately deferred, not overlooked: an LLM-graded rubric assertion (a response
  graded by a second model call) and assertions on an agent's actual tool-call trace
  rather than just its final text output — both need a runner protocol richer than
  "prompt in, text out," which is a real design question to come back to.

## Tool development experience

- **An MCP inspector / interactive run mode.** Testing a tool today means unit-testing
  its logic with mocked HTTP calls (right for CI), but there's no way to spin up the
  real MCP server and poke at it interactively the way an agent actually would. A
  `agentworks run tools/x` that starts the server and drops into a simple client REPL
  would close that loop.
- **`agentworks doctor`.** Checks environment prerequisites (is `mcp` installed? is the
  declared `command`'s interpreter on PATH?) — jira-fetch already needs
  `pip install mcp`, and "why did my test fail" is currently just a raw traceback.

## Ecosystem / scale

- ✅ **A skill/tool registry — pull, not just push.** Everything today is author-your-own.
  Agent Plugins is an open spec; importing someone else's published skill/tool into a
  project (`agentworks add <url>`) would make AgentWorks two-way, not just an export
  pipeline. Done: `agentworks add <url>` imports a bare Agent Skill or decomposes a
  Claude Code plugin's skills/agents; `agentworks tui`'s `a` key (or `add` with no
  argument in a terminal) searches agentskills.codes plus the well-known Claude Code and
  GitHub Copilot marketplaces (see [AGENTS.md](AGENTS.md)). Tool/hook import from a
  bundled plugin is deliberately deferred (lossy many-to-one), not overlooked.
- ✅ **Starter templates.** `agentworks new skill --from-template api-wrapper` instead of
  always starting from the generic blank scaffold. Done: ten built-in templates (two
  per kind), `agentworks templates [kind]` to list them, `--from-template <id>` on
  `agentworks new`, and `agentworks tui`'s `b` key to browse/search/create from one (see
  [AGENTS.md](AGENTS.md)). Project-defined custom templates are deliberately deferred,
  not overlooked — built-in only for now.
- **Shared snippets/partials.** A way for multiple agents to reference common guidance
  text without copy-pasting it into every `agent.md`.

## Quality of life

- **Watch mode** (`agentworks test --watch`) for active development.
- **Skip-unchanged export.** Hash-based, so re-exporting a large project doesn't
  rewrite everything every time.
