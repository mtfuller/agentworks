CREATE TABLE run_execution_plans (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    plan_digest TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('host', 'container', 'remote')),
    runtime TEXT NOT NULL,
    runtime_version TEXT,
    image_digest TEXT,
    network TEXT NOT NULL,
    plan_json TEXT NOT NULL CHECK (json_valid(plan_json)),
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX run_execution_plans_created_at_idx ON run_execution_plans(created_at);

ALTER TABLE runs ADD COLUMN detail_pruned_at INTEGER;
