---
kind: tool
name: jira-fetch
description: Fetch a Jira ticket and extract feature-relevant information.
version: 0.1.0
targets:
  - claude-code
  - github-copilot
entrypoint: src/main.py
command: python3 src/main.py
test: python3 -m unittest discover -s tests -p "test_*.py"
auth:
  - JIRA_BASE_URL
  - JIRA_EMAIL
  - JIRA_API_TOKEN
---

# Jira Fetch

An MCP server exposing one tool, `fetch_issue`, that fetches a Jira Cloud
ticket (e.g. `PROJ-123`) and returns what's useful for implementing it:
title, description, status, linked issues, and acceptance criteria (if
configured -- see below).

## Interface

- **Tool:** `fetch_issue(issue_key: str) -> dict`
- **Auth:** reads `JIRA_BASE_URL`, `JIRA_EMAIL`, and `JIRA_API_TOKEN` from
  the environment (see `auth:` above -- when exported, these become
  `${VAR}` references in the generated MCP server config, never literal
  secrets baked into a file).
- **Optional:** `JIRA_ACCEPTANCE_CRITERIA_FIELD` (e.g. `customfield_10059`).
  Acceptance criteria live in a custom field in most Jira instances, so
  there's no universal field name for it -- set this if your instance has
  one; it's simply omitted from the result otherwise. Not listed under
  `auth:` since it's optional configuration, not a required secret.

## Running it

`command:` above is what `agentworks export --target claude-code` and
`--target github-copilot` turn into an MCP server registration
(`.mcp.json`) -- Claude Code or Copilot spawn it as a subprocess and speak
MCP over stdio, they don't invoke it as a one-shot CLI command.

## Implementation

`src/main.py` (requires `pip install mcp`) and `tests/test_main.py`
(stdlib `unittest`, simulating the Jira REST API rather than calling a
real instance -- run via the `test:` command above). Uses Jira's API v2
(not v3) so `description` comes back as a plain string instead of
Atlassian Document Format's nested JSON.
