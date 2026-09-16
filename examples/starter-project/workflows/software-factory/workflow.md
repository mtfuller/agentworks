---
kind: workflow
name: software-factory
description: 'Research, fetch ticket context, then implement: an example multi-agent pipeline.'
version: 0.1.0
targets:
  - claude-code
steps:
  - agent: researcher
  - tool: jira-fetch
---

# Software Factory

An example pipeline composing two of this project's own artifacts:

1. **researcher** ([`agents/researcher`](../../agents/researcher)) gathers
   background on the feature area.
2. **jira-fetch** ([`tools/jira-fetch`](../../tools/jira-fetch)) pulls the
   specific ticket's acceptance criteria.

A real workflow engine (running these steps, passing output between them)
is future work -- for now this file documents the intended composition, and
is what a workflow exporter would read.
