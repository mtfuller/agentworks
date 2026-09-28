# ADR 0001: local storage and embedded UI

Status: accepted on 2026-09-24.

## Context

Studio must be a single cross-platform executable that a developer can start without Node,
Docker, CGO, or a separately managed database. Runtime state must survive restarts, allow UI
reads while a worker writes, and support transactional schema evolution. Large logs and
artifacts belong on disk rather than in database BLOBs.

## Decision

Use [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) through Go's standard
`database/sql` interface. Pin v1.45.0: it is the newest checked release declaring Go 1.24
support; later checked releases require Go 1.25. The driver is a CGo-free SQLite translation,
which preserves ordinary cross-compilation. SQLite runs in WAL mode with foreign keys,
`synchronous=NORMAL`, a five-second busy timeout, short write transactions, and an eight-
connection pool. Embedded forward-only SQL migrations are transactional and their names and
SHA-256 checksums are recorded so an applied migration cannot be silently edited or renamed.

Store each project's database under the platform-native per-user data directory, keyed by a
digest of its canonical absolute path. Operational state never appears in the checkout.

Keep the first Studio frontend as embedded HTML, CSS, and JavaScript served by Go's standard
HTTP server. A frontend build tool may be introduced when UI complexity justifies it, but its
compiled output must remain embedded so Node is never a runtime dependency.

## Consequences

- Releases do not need a platform C compiler or an external database service.
- WAL permits a stable UI read snapshot while a worker commits.
- Database commits, not in-memory channels, are the source of truth.
- A project moved to a different canonical path gets a different local runtime directory;
  migration/reattachment UX can be added if that becomes common.
- Studio remains one-process in v1, while repository interfaces and normalized state leave a
  future PostgreSQL/server-worker split possible.

Primary implementation references: the [modernc SQLite repository](https://github.com/modernc-org/sqlite)
and [SQLite WAL documentation](https://sqlite.org/wal.html).
