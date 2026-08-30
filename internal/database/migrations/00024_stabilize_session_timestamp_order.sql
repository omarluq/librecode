-- +goose Up
DROP INDEX IF EXISTS idx_sessions_cwd_parent_updated;
DROP INDEX IF EXISTS idx_sessions_parent_updated;

CREATE INDEX idx_sessions_cwd_parent_updated
    ON sessions(cwd, parent_session_id, updated_at DESC, id DESC);
CREATE INDEX idx_sessions_parent_updated
    ON sessions(parent_session_id, updated_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_cwd_parent_updated;
DROP INDEX IF EXISTS idx_sessions_parent_updated;

CREATE INDEX idx_sessions_cwd_parent_updated
    ON sessions(cwd, parent_session_id, updated_at DESC);
CREATE INDEX idx_sessions_parent_updated
    ON sessions(parent_session_id, updated_at DESC);
