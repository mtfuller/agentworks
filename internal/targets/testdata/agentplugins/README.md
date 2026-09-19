# Agent Plugins schemas

Vendored copies of the Agent Plugins 1.0.0 JSON Schemas, fetched from
https://agent-plugins.org/schemas/1.0.0/ on 2026-09-18:

- `plugin.schema.json` (a plugin's `plugin.json`)
- `mcp.schema.json` (a plugin's `mcp.json`)

The exporters' tests validate real output against them. There is **no**
marketplace schema: the spec defines none, and
https://agent-plugins.org/schemas/1.0.0/marketplace.schema.json returns 404
(AgentWorks used to write it as a `$schema` and no longer does).

To refresh, fetch the two files again and run `go test ./internal/targets/...`;
a schema change that breaks the exporters shows up there.
