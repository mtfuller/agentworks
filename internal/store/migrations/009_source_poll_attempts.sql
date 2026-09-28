CREATE TABLE source_poll_attempts (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('running', 'committed', 'succeeded', 'failed')),
    cursor_before TEXT,
    cursor_after TEXT,
    events_observed INTEGER NOT NULL DEFAULT 0 CHECK (events_observed >= 0),
    events_inserted INTEGER NOT NULL DEFAULT 0 CHECK (events_inserted >= 0),
    outcomes_observed INTEGER NOT NULL DEFAULT 0 CHECK (outcomes_observed >= 0),
    outcomes_inserted INTEGER NOT NULL DEFAULT 0 CHECK (outcomes_inserted >= 0),
    routed_runs INTEGER NOT NULL DEFAULT 0 CHECK (routed_runs >= 0),
    event_ids_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(event_ids_json)),
    run_ids_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(run_ids_json)),
    error_code TEXT,
    error_message TEXT,
    started_at INTEGER NOT NULL,
    finished_at INTEGER,
    next_poll_at INTEGER,
    rate_limit_reset_at INTEGER
) STRICT;

CREATE INDEX source_poll_attempts_source_started_idx
    ON source_poll_attempts(source_id, started_at DESC);
