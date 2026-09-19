# Author, test, export, publish

A project is a directory of artifacts. Each artifact is one file, `<kind>.md` (YAML
frontmatter, then a Markdown body), in its own folder under `agents/`, `skills/`, `mcp/`, or
`hooks/`.

## 1. Start a project

```bash
agentworks init my-kit --target claude-code --target github-copilot
cd my-kit
```

`init` writes `agentworks.yaml` (name, `targets:`, and optional publishing metadata:
`version`, `author`, `license`, `homepage`, `repository`) plus an `AGENTS.md` that teaches a
coding agent how to drive the CLI. Add `--ci` for a GitHub Actions workflow.

## 2. Add artifacts

```bash
agentworks new skill csv-analyzer
agentworks new mcp jira-fetch      # a runnable, dependency-free server
agentworks templates               # the built-in starting points
```

Edit the generated file. `agentworks validate` checks structure and lints descriptions (a
description is how an agent decides to use the artifact, so vague ones are flagged).
[The field reference](../reference/frontmatter.md) lists every supported key.

## 3. Test

```bash
agentworks validate --strict   # warnings fail too
agentworks doctor              # are the declared commands and binaries runnable here?
agentworks test                # each artifact's test: command; MCP servers get a smoke test
agentworks eval                # behavior cases; see the testing guide
```

## 4. Export

```bash
agentworks export              # one plugin per target under dist/<target>/
agentworks export --namespace team-a --namespace .   # one plugin per namespace
agentworks export --format skills.zip                # every skill in one zip
```

Exports are checked against the vendors' real formats (see `agentworks targets` for the
format and when it was last verified). An export that would ship an artifact without
something it `requires:` fails rather than producing a broken plugin.

## 5. Publish

To let a team install straight from your repository:

```bash
agentworks marketplace
git add plugins .claude-plugin .github/plugin && git commit -m "Publish plugins"
```

This writes committed plugin directories and a `marketplace.json` for Claude Code
(`.claude-plugin/`) and GitHub Copilot (`.github/plugin/`). In Claude Code:
`/plugin marketplace add <owner>/<repo>`. Keep it fresh in CI with
`agentworks marketplace --check`.

## Shell completion

```bash
agentworks completion zsh > "${fpath[1]}/_agentworks"     # zsh
agentworks completion bash > /etc/bash_completion.d/agentworks
agentworks completion fish > ~/.config/fish/completions/agentworks.fish
```

Run `agentworks completion --help` for other shells and per-shell setup notes.
