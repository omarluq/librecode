-- +goose Up
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    cwd TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    parent_session TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_entries (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    parent_id TEXT,
    entry_type TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    custom_type TEXT NOT NULL DEFAULT '',
    data_json TEXT NOT NULL DEFAULT '{}',
    summary TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES session_entries(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_cwd_updated ON sessions(cwd, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_entries_session_created ON session_entries(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_session_entries_parent ON session_entries(parent_id);

-- +goose Down
DROP TABLE IF EXISTS session_entries;
DROP TABLE IF EXISTS sessions;
