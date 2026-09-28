CREATE TABLE run_conclusions (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    conclusion_json TEXT NOT NULL CHECK (json_valid(conclusion_json)),
    private_memory TEXT NOT NULL DEFAULT '',
    structured INTEGER NOT NULL DEFAULT 1 CHECK (structured IN (0, 1)),
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE memory_proposals (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('team', 'agent')),
    target_path TEXT NOT NULL,
    base_sha256 TEXT NOT NULL,
    before_markdown TEXT NOT NULL,
    after_markdown TEXT NOT NULL,
    diff TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'approved', 'rejected', 'conflict')),
    created_at INTEGER NOT NULL,
    decided_at INTEGER
) STRICT;

CREATE INDEX memory_proposals_run_idx ON memory_proposals(run_id, created_at);

CREATE TABLE memory_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id TEXT NOT NULL REFERENCES memory_proposals(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    details_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details_json)),
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX memory_audits_proposal_idx ON memory_audits(proposal_id, created_at);
