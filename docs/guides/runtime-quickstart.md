# Runtime quickstart

This path creates and runs a format-2 agent team without requiring Git, Node, Docker, or a
project-specific build system. Claude Code or GitHub Copilot CLI must be installed and authenticated
before a real run; `--fake-harness` provides a deterministic local check without model usage.

```sh
agentworks init --runtime my-agents
cd my-agents
cp agentworks.local.yaml.example agentworks.local.yaml
```

Edit the workspace path in `agentworks.local.yaml` to an absolute folder. It may be a source checkout,
a notes directory, or any other local folder. Then verify and open the team:

```sh
agentworks plan default
agentworks harnesses
agentworks studio --experimental
```

Studio is localhost-only. Select `default`, `assistant`, the `workspace` binding, a harness, and
`readonly` for the first run. A write-capable run pauses for bounded approval before its first edit or
command. Live logs, conclusions, events, memory proposals, outcomes, monitors, storage health, and
provider plans remain inspectable after the run.

For a no-model smoke check, restart with `agentworks studio --experimental --fake-harness`. Prompts
may include `[fake:slow]`, `[fake:fail]`, `[fake:retry-once]`, `[fake:write]`, or `[fake:command]` to
exercise the corresponding durable path.

To move the team or render it for a harness:

```sh
agentworks pack default
agentworks export --team default --target claude-code --target github-copilot
```

The legacy artifact-first scaffold remains available through `agentworks init` without `--runtime`
during the release-candidate transition.
