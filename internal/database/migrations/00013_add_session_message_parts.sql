-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_entries_id_session
    ON session_entries(id, session_id);

CREATE TABLE IF NOT EXISTS session_message_parts (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    sequence INTEGER NOT NULL CHECK(sequence >= 0),
    type TEXT NOT NULL CHECK(type IN ('text', 'image')),
    text TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    data BLOB,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id, session_id) REFERENCES session_entries(id, session_id) ON DELETE CASCADE,
    UNIQUE (entry_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_session_message_parts_session_entry_sequence
    ON session_message_parts(session_id, entry_id, sequence);

-- +goose Down
DROP INDEX IF EXISTS idx_session_message_parts_session_entry_sequence;
DROP TABLE IF EXISTS session_message_parts;
DROP INDEX IF EXISTS idx_session_entries_id_session;
