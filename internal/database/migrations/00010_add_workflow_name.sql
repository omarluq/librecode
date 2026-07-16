-- +goose Up
ALTER TABLE workflow_runs
    ADD COLUMN name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workflow_runs
    DROP COLUMN name;
