-- +goose Up
CREATE TABLE workflow_runs (
    task_id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    source_version TEXT NOT NULL DEFAULT '',
    arguments_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    CHECK (length(source) > 0),
    CHECK (length(source_hash) > 0),
    CHECK (json_valid(arguments_json))
);

CREATE TABLE workflow_agent_tasks (
    workflow_task_id TEXT NOT NULL,
    agent_task_id TEXT NOT NULL UNIQUE,
    sequence INTEGER NOT NULL,
    node_key TEXT NOT NULL DEFAULT '',
    invocation_index INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (workflow_task_id, agent_task_id),
    UNIQUE (workflow_task_id, sequence),
    UNIQUE (workflow_task_id, node_key, invocation_index),
    FOREIGN KEY (workflow_task_id) REFERENCES workflow_runs(task_id) ON DELETE CASCADE,
    FOREIGN KEY (agent_task_id) REFERENCES agent_tasks(task_id) ON DELETE CASCADE,
    CHECK (sequence > 0),
    CHECK (invocation_index >= 0)
);

CREATE INDEX idx_workflow_runs_source_hash
    ON workflow_runs(source_hash, task_id);
CREATE INDEX idx_workflow_agent_tasks_replay
    ON workflow_agent_tasks(workflow_task_id, sequence);

-- +goose Down
DROP TABLE IF EXISTS workflow_agent_tasks;
DROP TABLE IF EXISTS workflow_runs;
