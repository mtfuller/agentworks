CREATE TABLE sources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    config_revision TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'unknown'
        CHECK (state IN ('unknown', 'healthy', 'degraded', 'failed', 'disabled')),
    last_error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE source_cursors (
    source_id TEXT PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    cursor TEXT,
    last_success_at INTEGER,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY,
    revision TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    priority INTEGER NOT NULL DEFAULT 0,
    loaded_at INTEGER NOT NULL
) STRICT;

CREATE TABLE work_items (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    external_key TEXT NOT NULL,
    title TEXT,
    state TEXT,
    data_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(data_json)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (kind, external_key)
) STRICT;

CREATE TABLE events (
    record_id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    type TEXT NOT NULL,
    subject TEXT,
    occurred_at INTEGER NOT NULL,
    received_at INTEGER NOT NULL,
    data_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(data_json)),
    status TEXT NOT NULL DEFAULT 'ingested'
        CHECK (status IN ('ingested', 'routed', 'unrouted')),
    routing_reason TEXT,
    routing_decided_at INTEGER,
    routed_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    work_item_id TEXT REFERENCES work_items(id) ON DELETE SET NULL,
    UNIQUE (source, external_id)
) STRICT;

CREATE INDEX events_received_at_idx ON events(received_at DESC);
CREATE INDEX events_status_idx ON events(status, received_at);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    event_record_id TEXT REFERENCES events(record_id) ON DELETE SET NULL,
    parent_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    team TEXT,
    agent TEXT NOT NULL,
    workspace TEXT NOT NULL,
    permission TEXT NOT NULL
        CHECK (permission IN ('readonly', 'readwrite', 'collaborate', 'autonomous')),
    state TEXT NOT NULL
        CHECK (state IN (
            'pending', 'leased', 'preparing', 'running', 'waiting-for-approval',
            'retry-scheduled', 'succeeded', 'failed', 'cancelled', 'interrupted'
        )),
    conclusion TEXT,
    pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX runs_created_at_idx ON runs(created_at DESC);
CREATE INDEX runs_state_idx ON runs(state, created_at);
CREATE INDEX runs_event_idx ON runs(event_record_id);

CREATE TABLE run_attempts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    number INTEGER NOT NULL CHECK (number > 0),
    worker_id TEXT,
    process_id INTEGER,
    state TEXT NOT NULL
        CHECK (state IN ('preparing', 'running', 'succeeded', 'failed', 'cancelled', 'interrupted')),
    started_at INTEGER,
    finished_at INTEGER,
    result_json TEXT CHECK (result_json IS NULL OR json_valid(result_json)),
    UNIQUE (run_id, number)
) STRICT;

CREATE TABLE run_leases (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    workspace TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('readonly', 'write')),
    owner TEXT NOT NULL,
    acquired_at INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
) STRICT;

CREATE INDEX run_leases_workspace_idx ON run_leases(workspace, mode, expires_at);

CREATE TABLE timers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    due_at INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'claimed', 'completed', 'cancelled')),
    payload_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(payload_json)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX timers_due_idx ON timers(state, due_at);

CREATE TABLE approvals (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    attempt_id TEXT REFERENCES run_attempts(id) ON DELETE SET NULL,
    kind TEXT NOT NULL,
    scope_json TEXT NOT NULL CHECK (json_valid(scope_json)),
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'approved', 'denied', 'cancelled', 'expired')),
    requested_at INTEGER NOT NULL,
    decided_at INTEGER,
    decision_reason TEXT
) STRICT;

CREATE INDEX approvals_run_idx ON approvals(run_id, requested_at);
CREATE INDEX approvals_state_idx ON approvals(state, requested_at);

CREATE TABLE outcomes (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    subject TEXT,
    event_record_id TEXT REFERENCES events(record_id) ON DELETE SET NULL,
    run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    work_item_id TEXT REFERENCES work_items(id) ON DELETE SET NULL,
    occurred_at INTEGER NOT NULL,
    data_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(data_json))
) STRICT;

CREATE INDEX outcomes_work_item_idx ON outcomes(work_item_id, occurred_at);
CREATE INDEX outcomes_run_idx ON outcomes(run_id, occurred_at);

CREATE TABLE monitor_states (
    monitor_id TEXT PRIMARY KEY,
    revision TEXT NOT NULL,
    state TEXT NOT NULL
        CHECK (state IN ('unknown', 'healthy', 'warning', 'unhealthy')),
    window_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(window_json)),
    details_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(details_json)),
    evaluated_at INTEGER NOT NULL
) STRICT;
