# Brainstorm: missing capabilities

Ideas for what AgentWorks could add beyond what's implemented today. Not a roadmap —
a working list to pull from. See [AGENTS.md](AGENTS.md) for what's actually built and
what's been deliberately deferred (with reasons).

## Open

- **Import MCP servers and hooks from a fetched plugin.** `agentworks add` decomposes a
  plugin's skills and agents but reports `.mcp.json` and `hooks/hooks.json` as unsupported.
  MCP servers map cleanly now that the mcp kind covers stdio and remote transports; hooks
  need a richer model first (per-handler event, matcher, handler type, timeout), and both
  need `${CLAUDE_PLUGIN_ROOT}`-style paths rewritten and the referenced scripts copied in.
  Round-trip tests (export → import → export is stable) would keep it honest.
- **Artifact dependencies.** Nothing declares that an agent needs a particular skill or MCP
  server, so `validate` can't catch a dangling reference and `export` can't pull the needed
  pieces into a bundle. A `requires:` field, validated and honored by export, would replace
  what workflows used to check. MCP servers also have no declared runtime requirements
  beyond `doctor` finding the binary.
- **Evals against other vendors' CLIs.** The reference runners cover Claude Code; a Copilot
  CLI or Gemini CLI runner needs each one's headless mode, and the reference runners still
  need a check against a live authenticated run.
- **Remote MCP in `run` and the smoke test.** They only speak stdio; an HTTP/SSE transport
  in `internal/mcpclient` would let them inspect hosted servers too, and resources/prompts
  listing is cheap once there.
- **Exporters checked against the real vendor tools.** Output is tested against fixtures,
  never installed into Claude Code / Copilot / Gemini CLI. A scheduled CI job that installs
  the exported plugin where a headless mode exists would catch format drift early.
- **`--json` for `add`, `update`, `export`, `build`,** and stale-entry cleanup in the merged
  `.cursor/*.json` / `.gemini/settings.json` files.
- **Marketplace path for a vendor-neutral Agent Plugins target.** Copilot CLI reads
  `.github/plugin/marketplace.json` and `.claude-plugin/marketplace.json`, both of which
  `agentworks marketplace` writes; the Agent Plugins spec doesn't name a marketplace path.
  Revisit if it does.
- **Shared snippets/partials.** A way for multiple agents to reference common guidance text
  without copy-pasting it into every `agent.md`.
- **Watch mode** (`agentworks test --watch`) for active development.
- **Skip-unchanged export.** Hash-based, so re-exporting a large project doesn't rewrite
  everything every time.
- **Windows.** Releases and CI cover macOS and Linux; the `sh -c` command convention would
  need checking on Windows.

## Done

- Multi-artifact bundles and bulk export (`export` bundles the project, or one plugin per
  namespace).
- Richer agents: a vendor-agnostic `tools:`/`model:` vocabulary mapped to Claude Code and
  Gemini CLI.
- `agentworks eval`: text, tool-call, tool-argument, and activation assertions; trigger cases;
  rubrics graded by a project-supplied judge; repeated runs with a pass threshold; timeouts;
  JUnit output; a json runner protocol beside the text one.
- MCP development loop: `agentworks run` inspector, `doctor`, a built-in smoke test, and
  working scaffolds for Python, Node, and TypeScript servers plus `npx-wrapper` and
  `remote-http` templates.
- `agentworks add` / `update` (pull, not just push), and a namespaced, license-aware plugin
  browser.
- Starter templates (`agentworks templates`, `--from-template`).
- CI surface: `--json`, `validate --strict`, `marketplace --check`,
  `status --fail-on-drift`, `action.yml`, and `init --ci`.
- Removed on purpose: the workflow kind and the `m365-copilot` target.
