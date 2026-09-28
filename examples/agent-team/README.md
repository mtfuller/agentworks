# Agent team example

This is the format-2 dogfood fixture for the AgentWorks rearchitecture. Two agents share
five native Agent Skills and one tool definition; resolution stores each shared component
once while preserving per-agent visibility.

From this directory:

```sh
agentworks plan engineering --provider host
agentworks studio --experimental --no-open
```

Before creating a manual run, copy `agentworks.local.yaml.example` to the gitignored
`agentworks.local.yaml` and bind `product` to an absolute folder on this machine. Studio never
accepts a workspace path from the browser request; it resolves the selected alias through this
local file.

Studio also loads the checked-in `sources/` and `routes/` definitions. The example includes a
manual implementation route and a weekly readonly review schedule. Route `tests:` are previews:
they run through the same deterministic evaluator shown in Studio, but never persist an event or
start an agent. The weekly source starts in 2035 so opening this example does not unexpectedly
dispatch work; adjust `start_at`, `every`, and `catch_up` when you are ready to exercise it.

The Jira and GitHub polling sources are checked in disabled. Update their base URL/repository,
provide `JIRA_EMAIL`, `JIRA_API_TOKEN`, and `GITHUB_TOKEN` in Studio's environment, then set
`enabled: true`. Studio can test either connection or trigger a poll immediately. Jira issue
keys found in pull request titles or bodies correlate both systems onto one work-item timeline;
ticket text and PR comments remain untrusted event data and can never select a workspace path.

To try manual routing, submit this event in Studio:

```json
{"id":"demo-1","source":"manual","type":"work.requested","subject":"demo","data":{"role":"implementer"}}
```

The Jira MCP tool remains a separate composition fixture for agent-initiated Jira actions;
its host and pinned-container variants exercise provider selection. The `jira-main` source is
the runtime-owned, read-only polling path that creates events and outcomes.
