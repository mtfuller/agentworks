---
name: agentworks-author-skill
description: >
  How to write or edit a skill in this AgentWorks project: skill.md frontmatter, the
  description that decides when it triggers, entrypoint/scripts/tests layout, and
  build/test commands. Use when creating or changing anything under skills/.
---

# Authoring a skill

A skill is a reusable, model-invoked capability: instructions plus optional scripts. It
exports to the Agent Skills format that Claude Code, ChatGPT, GitHub Copilot, and others
build on.

## Create it

```bash
agentworks new skill <name> --description "What it does and when to use it"
```

Or `--from-template <id>` (`agentworks templates skill`: `checklist`, `document-analyzer`,
`node-skill`, ...). Names are lowercase letters, digits, and hyphens, and must match the
directory.

```
skills/<name>/
  skill.md            manifest: frontmatter + instructions
  scripts/main.py     the entrypoint
  tests/              tests for the scripts
  samples/            example inputs
  evals/*.yaml        behavior-eval cases
```

## skill.md frontmatter

```yaml
---
kind: skill
name: csv-analyzer            # required; matches directory
description: Summarizes and profiles CSV files. Use when the user shares a CSV or asks for column stats.
version: 0.1.0
entrypoint: scripts/main.py   # file must exist (agentworks doctor checks)
build: pip install -r requirements.txt   # optional
test: pytest tests            # optional; run by `agentworks test`
eval_runner: claude -p        # optional; see agentworks-evals
---
```

`build`, `test`, and `eval_runner` are shell commands run from the artifact's directory.

## Write the description first

The description is the only text an agent sees when deciding whether to load the skill.
State **what it does and when to use it**, in third person, naming the trigger phrases or
inputs. `agentworks validate` warns under 20 characters, over 1024, or when it overlaps
heavily (Jaccard > 0.7 on significant words) with another skill's description.

## Body

Imperative instructions the agent follows once loaded: steps, decision points, output
format, and how to call `scripts/`. Keep it focused; move long reference material into
supporting files and link to them so it loads only when needed.

## Finish

1. Replace the placeholder script and test (`NotImplementedError` / `assert True`).
2. Set `test:` once real tests exist; `agentworks test skills/<name>`.
3. Add real cases under `evals/` (see `agentworks-evals`) and delete the placeholder one.
4. `agentworks validate` and `agentworks doctor skills/<name>`.
5. `agentworks export`.
