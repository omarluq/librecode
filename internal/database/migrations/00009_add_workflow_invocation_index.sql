-- +goose Up
CREATE TABLE workflow_agent_tasks_v9 (
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

INSERT INTO workflow_agent_tasks_v9
    (workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at)
SELECT current.workflow_task_id,
       current.agent_task_id,
       current.sequence,
       current.node_key,
       (
           SELECT COUNT(*) - 1
           FROM workflow_agent_tasks AS preceding
           WHERE preceding.workflow_task_id = current.workflow_task_id
             AND preceding.node_key = current.node_key
             AND preceding.sequence <= current.sequence
       ),
       current.created_at
FROM workflow_agent_tasks AS current;

DROP TABLE workflow_agent_tasks;
ALTER TABLE workflow_agent_tasks_v9 RENAME TO workflow_agent_tasks;

CREATE INDEX idx_workflow_agent_tasks_replay
    ON workflow_agent_tasks(workflow_task_id, sequence);

-- +goose Down
CREATE TABLE workflow_agent_tasks_v8 (
    workflow_task_id TEXT NOT NULL,
    agent_task_id TEXT NOT NULL UNIQUE,
    sequence INTEGER NOT NULL,
    node_key TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (workflow_task_id, agent_task_id),
    UNIQUE (workflow_task_id, sequence),
    FOREIGN KEY (workflow_task_id) REFERENCES workflow_runs(task_id) ON DELETE CASCADE,
    FOREIGN KEY (agent_task_id) REFERENCES agent_tasks(task_id) ON DELETE CASCADE,
    CHECK (sequence > 0)
);

INSERT INTO workflow_agent_tasks_v8
    (workflow_task_id, agent_task_id, sequence, node_key, created_at)
SELECT workflow_task_id, agent_task_id, sequence, node_key, created_at
FROM workflow_agent_tasks;

DROP TABLE workflow_agent_tasks;
ALTER TABLE workflow_agent_tasks_v8 RENAME TO workflow_agent_tasks;

CREATE INDEX idx_workflow_agent_tasks_replay
    ON workflow_agent_tasks(workflow_task_id, sequence);
