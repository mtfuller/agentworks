ALTER TABLE events ADD COLUMN raw_data_json TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(raw_data_json));
ALTER TABLE events ADD COLUMN replay_of TEXT REFERENCES events(record_id) ON DELETE SET NULL;

CREATE INDEX events_source_time_idx ON events(source, occurred_at DESC);
