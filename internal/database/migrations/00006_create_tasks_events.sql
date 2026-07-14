-- +goose Up
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    state TEXT NOT NULL,
    parent_task_id TEXT,
    owner_session_id TEXT NOT NULL,
    concurrency_key TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (parent_task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (owner_session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    CHECK (kind <> ''),
    CHECK (state IN ('queued', 'running', 'canceling', 'succeeded', 'failed', 'canceled', 'interrupted'))
);

CREATE TABLE events (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    CHECK (kind <> ''),
    CHECK (json_valid(payload_json))
);

CREATE TABLE task_events (
    task_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    PRIMARY KEY (task_id, event_id),
    UNIQUE (task_id, sequence),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE,
    CHECK (sequence > 0)
);

CREATE TABLE agent_tasks (
    task_id TEXT PRIMARY KEY,
    child_session_id TEXT NOT NULL UNIQUE,
    agent_name TEXT NOT NULL,
    prompt TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    policy_json TEXT NOT NULL DEFAULT '{}',
    usage_json TEXT NOT NULL DEFAULT '{}',
    depth INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (child_session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    CHECK (agent_name <> ''),
    CHECK (prompt <> ''),
    CHECK (json_valid(policy_json)),
    CHECK (json_valid(usage_json)),
    CHECK (depth >= 1)
);

CREATE INDEX idx_tasks_kind_state_created
    ON tasks(kind, state, created_at, id);
CREATE INDEX idx_tasks_owner_updated
    ON tasks(owner_session_id, updated_at DESC, id DESC);
CREATE INDEX idx_tasks_owner_state_updated
    ON tasks(owner_session_id, state, updated_at DESC, id DESC);
CREATE INDEX idx_tasks_parent_updated
    ON tasks(parent_task_id, updated_at DESC, id DESC);
CREATE INDEX idx_task_events_replay
    ON task_events(task_id, sequence);

-- +goose Down
DROP TABLE IF EXISTS agent_tasks;
DROP TABLE IF EXISTS task_events;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS tasks;
