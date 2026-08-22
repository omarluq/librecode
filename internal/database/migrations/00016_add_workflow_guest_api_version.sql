-- +goose Up
ALTER TABLE workflow_runs
    ADD COLUMN guest_api_version TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workflow_runs
    DROP COLUMN guest_api_version;
