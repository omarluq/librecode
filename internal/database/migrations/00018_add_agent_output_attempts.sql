-- +goose Up
ALTER TABLE agent_tasks ADD COLUMN output_attempts_reserved INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_tasks ADD COLUMN output_attempts_completed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_tasks ADD COLUMN output_validation_summary TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agent_tasks DROP COLUMN output_validation_summary;
ALTER TABLE agent_tasks DROP COLUMN output_attempts_completed;
ALTER TABLE agent_tasks DROP COLUMN output_attempts_reserved;
