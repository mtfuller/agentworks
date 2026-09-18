---
name: agentworks-evals
description: >
  How to write and run behavior-eval cases for a skill or agent in this AgentWorks project:
  the evals/*.yaml format, assertion types, configuring an eval_runner, and
  agentworks eval. Use when asked to test, evaluate, or regression-check how a skill or
  agent responds.
---

# Behavior evals

`agentworks test` runs unit tests. `agentworks eval` checks how an artifact **responds**:
each case sends a prompt to a runner command and asserts on its stdout. AgentWorks never
calls a model itself.

## Case files

Put one or more `*.yaml` / `*.yml` files directly in `<artifact>/evals/` (no subfolders).
Scaffolded skills and agents start with `evals/example.yaml`; replace its placeholder.

```yaml
cases:
  - name: summarizes a csv
    prompt: "Profile the attached sales.csv and report row count."
    assert:
      contains: ["rows", "columns"]        # case-insensitive substrings, all required
      not_contains: ["as an AI"]           # none may appear
      matches: "\\d+ rows"                 # Go regex that must match
      not_matches: "(?i)error"             # Go regex that must not match
      min_length: 40
      max_length: 800
```

Every assertion set on a case must pass. All fields are optional, but a case with no
assertions proves nothing. Names identify cases for `--case`, so keep them unique.
Assertions are deterministic string checks: assert on facts, keywords, and format, not on
exact prose. Cover the happy path, a should-refuse or should-not-trigger case, and one
edge input.

## Runner

The runner is a shell command that reads the prompt on **stdin** and prints the response on
**stdout**. Set it per artifact in frontmatter:

```yaml
eval_runner: claude -p
```

or once for the project in `agentworks.yaml`:

```yaml
eval:
  default_runner: claude -p
```

An artifact's own `eval_runner` wins. With no runner, artifacts with cases are skipped with
a warning, not failed. To make a runner point at *this* artifact, wrap the call in a script
that injects the artifact's body as the system prompt.

## Run

```bash
agentworks eval                  # every artifact with an evals/ directory
agentworks eval skills/<name>    # one artifact
agentworks eval --case "<name>"  # one case by exact name
```

`agentworks validate` also parses eval files, so malformed YAML is caught early.
`agentworks doctor` verifies the runner's binary is on PATH.
