ALTER TABLE approvals ADD COLUMN resume_state TEXT NOT NULL DEFAULT 'preparing'
    CHECK (resume_state IN ('preparing', 'running'));

ALTER TABLE approvals ADD COLUMN summary TEXT NOT NULL DEFAULT '';
