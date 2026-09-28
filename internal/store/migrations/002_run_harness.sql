ALTER TABLE runs ADD COLUMN harness TEXT NOT NULL DEFAULT 'claude-code'
    CHECK (length(harness) > 0);

CREATE INDEX runs_harness_state_idx ON runs(harness, state, created_at);
