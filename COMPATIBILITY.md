# Compatibility

What AgentWorks promises to keep working, what it doesn't, and how changes are announced.

Until 1.0, minor versions (`0.x`) may make breaking changes, always listed in
[CHANGELOG.md](CHANGELOG.md). From 1.0 on, everything under "Covered" below changes only in
a major version.

## Covered

**Project format.** The shape of `agentworks.yaml`, artifact frontmatter, and
`agentworks.lock`, taken together, has one version: the `format:` field in `agentworks.yaml`
(absent means 1). `agentworks version --json` reports the highest format a binary reads
(`project_format`).

- A binary refuses a project whose `format` is newer than it understands, with a message to
  upgrade, instead of misreading it.
- The format number is bumped only for a change an older binary could not read correctly.
  Adding an optional field, or a new artifact kind or target, does not bump it.

**Frontmatter fields.** The fields in [docs/reference/frontmatter.md](docs/reference/frontmatter.md)
are the supported set. Any other key is ignored on export and warned about by
`agentworks validate`. Keys prefixed `x-` are yours: they are never used, never warned about,
and never removed.

**Commands, flags, and exit codes.** Documented commands and flags keep their meaning. A
command exits 0 on success and non-zero on failure; the CI gates (`validate --strict`,
`doctor`, `marketplace --check`, `status --fail-on-drift`) are part of this.

**`--json` documents.** Every document carries `schema_version`, `command`, and `ok`, and is
described by a JSON Schema in [docs/schemas/](docs/schemas/).

- Adding a field is not breaking, so consumers must ignore keys they don't recognize.
- Removing a field, retyping it, or changing what it means bumps `schema_version`.
- The schemas are generated from the Go types and checked against real output in the tests.

## Platforms

Linux and macOS are supported. Windows is experimental (built and vetted in CI, tests not run): it needs a POSIX `sh` (WSL or Git
Bash) for declared commands; the full test suite runs on Linux and macOS only.

## Not covered

- **The text of human-readable output** (messages, table layout, colors, the TUI). Use
  `--json` to script against.
- **The exact bytes of exported files.** Vendor formats change, and AgentWorks follows them.
  A fix that changes exported output to match a vendor's real format is not a breaking change.
  What *is* covered is the input: an artifact that exported before still exports.
- **Go packages.** AgentWorks is a CLI; `internal/` is private and `pkg/` is unused.
- **Vendor-specific hook event names** and other values passed through to a target unchanged.

## Deprecation

A field, flag, or command slated for removal is first *deprecated*:

1. It keeps working, and is marked deprecated in the reference docs and CHANGELOG.
2. Using it produces a warning (`agentworks validate` for fields; a message on stderr for
   flags and commands) that names its replacement.
3. It is removed no sooner than the next minor version before 1.0, or the next major version
   after it.

## Removed without deprecation (pre-1.0)

Things removed before this policy existed are listed in the CHANGELOG under the release that
removed them.
