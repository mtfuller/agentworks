CREATE TABLE runtime_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    topic TEXT NOT NULL CHECK (length(topic) > 0),
    entity_type TEXT NOT NULL CHECK (length(entity_type) > 0),
    entity_id TEXT NOT NULL CHECK (length(entity_id) > 0),
    data_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(data_json)),
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX runtime_events_entity_idx
    ON runtime_events(entity_type, entity_id, sequence);
