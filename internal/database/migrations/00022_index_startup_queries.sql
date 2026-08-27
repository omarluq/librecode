-- +goose Up
CREATE INDEX idx_tasks_state_created
    ON tasks(state, created_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_state_created;
