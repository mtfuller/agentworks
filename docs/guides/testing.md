# Testing MCP servers and behavior

## Code: `test:`

Give an artifact a `test:` command and `agentworks test` runs it from the artifact's
directory. For an MCP server that declares none, `test` starts the server, performs the MCP
handshake, and lists its tools. Remote servers (`transport: http` or `sse`) are only
contacted with `--remote`, since that makes network calls.

## Try a server by hand: `run`

```bash
agentworks run mcp/jira-fetch
```

Opens a full-screen inspector: browse the tools (and resources and prompts, if the server
offers them), fill in a call, and see the result the way an agent would. It works for stdio
and remote servers, uses your real environment, and never logs headers (they come from
`${VAR}` references).

## Behavior: `eval`

Cases live in `<artifact>/evals/*.yaml`. AgentWorks never calls a model itself: a *runner*
you provide receives each case's prompt and prints the response, and assertions check it.

```yaml
cases:
  - name: cites a source
    prompt: "Is library X really 2x faster?"
    runs: 5                       # repeat a nondeterministic case...
    pass_threshold: 0.8           # ...and pass if 80% of runs do
    timeout: 60                   # seconds per run (default 120)
    assert:
      contains: ["benchmark"]
      tool_called: [web-search]   # needs eval_protocol: json
      rubric: "Says whether the claim is confirmed, and cites where."   # graded by judge_runner
  - name: is chosen for a research request
    prompt: "Can you research whether library X is really 2x faster?"
    should_trigger: true          # tests the artifact's description
```

- `eval_protocol: text` (default) reads stdout; `json` reads
  `{text, tool_calls, activated, usage}` and unlocks trace and trigger assertions.
- The full assertion list (`contains`, `matches`, `max_length`, `tool_args`, ...) is in the README's "Behavior evals".
- `--junit report.xml` writes results for CI.
- Working runners for Claude Code are in `examples/eval-runners/`.
