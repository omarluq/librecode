-- +goose Up
CREATE TABLE IF NOT EXISTS session_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    entry_id TEXT NOT NULL UNIQUE,
    sender TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id) REFERENCES session_entries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_session_messages_session_created ON session_messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_session_messages_sender ON session_messages(sender);

INSERT INTO session_messages (id, session_id, entry_id, sender, role, content, provider, model, created_at)
SELECT
    lower(hex(randomblob(16))),
    session_id,
    id,
    role,
    role,
    content,
    provider,
    model,
    created_at
FROM session_entries
WHERE role <> ''
ON CONFLICT(entry_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS session_messages;
