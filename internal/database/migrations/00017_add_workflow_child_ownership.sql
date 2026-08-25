-- +goose Up
ALTER TABLE workflow_runs ADD COLUMN admission_closed_at TEXT;
ALTER TABLE agent_tasks ADD COLUMN output_schema_json TEXT;
ALTER TABLE agent_tasks ADD COLUMN output_schema_digest TEXT;

-- +goose Down
ALTER TABLE agent_tasks DROP COLUMN output_schema_digest;
ALTER TABLE agent_tasks DROP COLUMN output_schema_json;
ALTER TABLE workflow_runs DROP COLUMN admission_closed_at;
