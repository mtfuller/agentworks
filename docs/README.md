# AgentWorks documentation

**Architecture and planning**
- [Runtime rearchitecture](architecture/runtime-rearchitecture.md): proposed format-2 agent-team, Studio, event, permission, and runtime design.
- [Harness conformance ledger](architecture/harness-conformance.md): observed Claude/Copilot CLI readiness and required fixture evidence.
- [Architecture decisions](architecture/decisions/README.md): accepted implementation choices and their tradeoffs.
- [Post-1.0 implementation plan](../PLAN-2.0.md): phased development plan and acceptance criteria.
- [Release and installation](release.md): unsigned RC archives, provenance, and native installers.

**Guides** (start here)
- [Runtime quickstart](guides/runtime-quickstart.md): create a format-2 team and run it locally from a fresh install.
- [Author, test, export, publish](guides/author-to-publish.md): the whole loop, end to end.
- [Dependencies and requirements](guides/dependencies.md): `requires:` and `bins:`.
- [Importing from others](guides/importing.md): `add`, `update`, and what is pinned.
- [Testing MCP servers and behavior](guides/testing.md): `test`, `run`, and evals.
- [Continuous integration](guides/ci.md): the GitHub Action, `--json`, exit codes.
- [Trust and security](../SECURITY.md): what `add` checks, and what it does not.
- [Runtime threat model](security/runtime-threat-model.md): Studio trust boundaries, controls, residual risks, and release gate.
- [M9 local runtime security review](security/m9-local-review.md): resolved implementation findings and remaining external gates.

**Reference**
- [Frontmatter fields](reference/frontmatter.md): generated from the registry, per kind.
- [`--json` schemas](schemas/): one JSON Schema per command's document.
- [Compatibility promises](../COMPATIBILITY.md) and the [changelog](../CHANGELOG.md).
- [Shell completion](guides/author-to-publish.md#shell-completion).

**Operations**
- [Studio release-candidate soak](operations/runtime-soak.md): required multi-day stability and recovery evidence.
- [Release-candidate diagnostics](operations/rc-diagnostics.md): source-poll history, safe failure evidence, and CI fixture coverage.
