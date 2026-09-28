# Architecture decision records

Accepted runtime decisions are recorded here when the choice materially constrains future
implementation. The runtime specification remains the cohesive design; these records explain
why a specific dependency or boundary was selected.

| ADR | Status | Decision |
|---|---|---|
| [0001](0001-local-storage-and-embedded-ui.md) | Accepted | Pure-Go SQLite/WAL storage and an embedded dependency-free Studio UI |
| [0002](0002-runtime-cutover.md) | Accepted for M9 | Runtime-first product center with a gated compatibility cutover |
