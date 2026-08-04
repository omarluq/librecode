-- +goose Up
CREATE INDEX IF NOT EXISTS idx_session_message_parts_session_entry_image
    ON session_message_parts(session_id, entry_id)
    WHERE type = 'image';

-- +goose Down
DROP INDEX IF EXISTS idx_session_message_parts_session_entry_image;
