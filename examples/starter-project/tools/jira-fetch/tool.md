---
kind: tool
name: jira-fetch
description: Fetch a Jira ticket and extract feature-relevant information.
version: 0.1.0
targets:
  - claude-code
  - github-copilot
entrypoint: src/main
auth:
  - JIRA_BASE_URL
  - JIRA_API_TOKEN
---

# Jira Fetch

Given a ticket key (e.g. `PROJ-123`), fetches it from the Jira REST API and
extracts what's useful for implementing it: title, description, acceptance
criteria, and linked issues.

## Interface

- **Input:** a ticket key.
- **Output:** structured JSON (title, description, acceptance criteria,
  linked issues) an agent can consume directly.
- **Auth:** reads `JIRA_BASE_URL` and `JIRA_API_TOKEN` from the environment
  (see `auth:` in this file's frontmatter -- that's what a vendor exporter
  should surface as required configuration).

## Implementation

Add real source under `src/` (this is intentionally language-agnostic --
Go, Python, whatever fits the target vendor). For tests, simulate the Jira
REST API rather than hitting a real instance, and set a `test:` command in
this file's frontmatter once they exist.
