-- +goose Up
ALTER TABLE session_entries ADD COLUMN operation_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX idx_session_entries_operation_id
    ON session_entries(operation_id)
    WHERE operation_id <> '';

CREATE UNIQUE INDEX idx_session_entries_compaction_parent
    ON session_entries(session_id, COALESCE(parent_id, ''))
    WHERE entry_type = 'compaction' AND operation_id <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_session_entries_compaction_parent;
DROP INDEX IF EXISTS idx_session_entries_operation_id;
ALTER TABLE session_entries DROP COLUMN operation_id;
