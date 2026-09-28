# ADR 0002: Runtime-first release-candidate cutover

Status: Accepted for M9

## Decision

Format 2 agent teams and localhost Studio are the product center. `init --runtime`, `plan`,
`providers`, `pack`, `export --team`, `harnesses`, and `studio` form the supported runtime path.
New runtime capabilities are implemented only in the format-2 packages.

The format-1 artifact commands remain frozen compatibility tooling through the first runtime release:
`init` without `--runtime`, `new`, `list`, `validate`, `build`, `test`, `eval`, `doctor`, `run`,
`targets`, `add`, `update`, `status`, `marketplace`, and artifact-mode `export`. They receive security
and conformance fixes but no new runtime concepts. Static importing/exporting remains useful and is
not implemented inside Studio.

The artifact TUI is not part of the runtime product and will not gain format-2 behavior. It remains
available only during the release-candidate transition so existing regression coverage and legacy
project inspection do not have to be removed before the runtime soak. After the soak, the final M9
cutover makes runtime initialization the default, moves the old initializer behind an explicit
compatibility flag, and removes the TUI command before the stable runtime release.

## Why staged rather than immediate

Removing the working compatibility surface before authenticated connector dogfood and the multi-day
Studio soak would eliminate the reference behavior used to verify static exporters, import security,
and MCP tooling. Conversely, adding new concepts to both architectures would prolong the split. A
feature freeze plus an explicit runtime initializer gives fresh-install coverage now and makes the
last cutover a small CLI change contingent on evidence rather than optimism.

## Release gate

The final switch requires green authenticated harness and connector jobs, a completed soak record,
the software-factory dogfood findings, and no open critical/high security issue.
