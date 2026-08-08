-- +goose Up
CREATE UNIQUE INDEX idx_tasks_id_owner_tool ON tasks(id, owner_session_id);

CREATE TABLE tool_tasks (
    task_id TEXT NOT NULL PRIMARY KEY,
    target_name TEXT NOT NULL,
    arguments_json TEXT NOT NULL,
    cwd TEXT NOT NULL,
    owner_session_id TEXT NOT NULL,
    invocation_id TEXT NOT NULL,
    wrapper_call_id TEXT NOT NULL,
    parent_call_id TEXT NOT NULL DEFAULT '',
    source_sequence INTEGER NOT NULL DEFAULT 0,
    initiating_entry_id TEXT NOT NULL DEFAULT '',
    timeout_seconds INTEGER NOT NULL,
    policy_json TEXT NOT NULL DEFAULT '{}',
    definition_json TEXT NOT NULL,
    outcome_version INTEGER,
    outcome_json TEXT,
    FOREIGN KEY (task_id, owner_session_id) REFERENCES tasks(id, owner_session_id) ON DELETE CASCADE,
    CHECK (length(target_name) > 0),
    CHECK (length(cwd) > 0),
    CHECK (length(invocation_id) > 0),
    CHECK (length(wrapper_call_id) > 0),
    CHECK (json_valid(arguments_json) AND json_valid(policy_json) AND json_valid(definition_json)),
    CHECK (
        json_type(arguments_json) = json_type(policy_json)
        AND json_type(policy_json) = json_type(definition_json)
        AND json_type(arguments_json) = 'object'
    ),
    CHECK (timeout_seconds > 0),
    CHECK (outcome_version IS NULL OR outcome_version > 0),
    CHECK (outcome_json IS NULL OR json_valid(outcome_json))
);

CREATE UNIQUE INDEX idx_tool_tasks_owner_invocation ON tool_tasks(owner_session_id, invocation_id);
CREATE INDEX idx_tool_tasks_owner_target ON tool_tasks(owner_session_id, target_name, task_id);

-- +goose Down
DROP INDEX IF EXISTS idx_tool_tasks_owner_target;
DROP INDEX IF EXISTS idx_tool_tasks_owner_invocation;
DROP TABLE IF EXISTS tool_tasks;
DROP INDEX IF EXISTS idx_tasks_id_owner_tool;
