# Dependencies and requirements

## `requires:` between artifacts

An agent that leans on a skill, or a skill that expects an MCP server, should say so:

```yaml
kind: agent
name: researcher
requires: [skill:csv-analyzer, mcp:jira-fetch, skill:team-a/style-guide]
```

A reference is `kind:name`, or `kind:namespace/name` for a namespaced artifact.

- `agentworks validate` fails on a reference to something that doesn't exist (with a
  "did you mean" when the name exists under another kind or namespace) and on cycles.
- `agentworks export` and `agentworks marketplace` fail when a plugin would ship an artifact
  without something it requires. Include the dependency, or keep both in one namespace (or
  pass `--single` to `marketplace`).
- An agent's exported instructions gain a short **Requires** section, because no vendor's
  subagent format has a field for depending on other artifacts.
- `agentworks graph` prints the graph (`--dot` for Graphviz); `agentworks list --json`
  includes each artifact's `requires`.

## `bins:` runtime requirements

```yaml
bins: [python3, node>=20]
```

`agentworks doctor` checks each is on `PATH` and, with `>=`, runs `<bin> --version` and
compares. Only `>=` is supported.
