# Continuous integration

`agentworks init --ci` writes a workflow; or add the action to an existing one:

```yaml
- uses: actions/checkout@v4
- uses: mtfuller/agentworks@v0.22.0
  with:
    checks: validate,doctor,marketplace   # also: test, eval, status
    strict: "true"
```

Pin a release tag: a new release can then never change what your CI checks.

## Scripting against the CLI

Every reporting command takes `--json`: stdout is exactly one document and human messages go
to stderr. Each has a schema in [docs/schemas](../schemas/) and carries `schema_version`,
`command`, and `ok` (true exactly when the exit code is 0). Ignore keys you don't recognize;
a removed or retyped field bumps `schema_version`.

```bash
agentworks validate --json | jq '.artifacts[] | select(.errors | length > 0)'
```

## Useful gates

| Command | Fails when |
|---|---|
| `validate --strict` | any error or warning, including dangling `requires:` |
| `doctor` | a declared command or `bins:` entry isn't runnable |
| `marketplace --check` | committed `plugins/` or `marketplace.json` are stale |
| `status --fail-on-drift` | an export or import differs from what the lockfile recorded |
| `eval --junit out.xml` | an eval case fails; results in JUnit form |
