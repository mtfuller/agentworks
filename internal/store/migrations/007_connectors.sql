ALTER TABLE sources ADD COLUMN definition_enabled INTEGER NOT NULL DEFAULT 1
    CHECK (definition_enabled IN (0, 1));
ALTER TABLE sources ADD COLUMN paused INTEGER NOT NULL DEFAULT 0
    CHECK (paused IN (0, 1));
ALTER TABLE sources ADD COLUMN last_poll_at INTEGER;
ALTER TABLE sources ADD COLUMN next_poll_at INTEGER;
ALTER TABLE sources ADD COLUMN rate_limit_reset_at INTEGER;
ALTER TABLE sources ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0);

CREATE INDEX sources_next_poll_idx
    ON sources(definition_enabled, paused, next_poll_at);

