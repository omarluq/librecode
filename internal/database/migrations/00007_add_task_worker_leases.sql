-- +goose Up
ALTER TABLE tasks ADD COLUMN lease_owner TEXT;
ALTER TABLE tasks ADD COLUMN lease_expires_at TEXT;

CREATE INDEX idx_tasks_recoverable_leases
    ON tasks(kind, state, lease_expires_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_recoverable_leases;
ALTER TABLE tasks DROP COLUMN lease_expires_at;
ALTER TABLE tasks DROP COLUMN lease_owner;
