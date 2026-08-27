-- +goose Up
DROP INDEX IF EXISTS idx_session_messages_session_created;
CREATE INDEX idx_session_messages_session_created
    ON session_messages(session_id, created_at, entry_id);

-- +goose Down
DROP INDEX IF EXISTS idx_session_messages_session_created;
CREATE INDEX idx_session_messages_session_created
    ON session_messages(session_id, created_at);
