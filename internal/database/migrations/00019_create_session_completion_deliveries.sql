-- +goose Up
CREATE TABLE session_completion_sequences (
    owner_session_id TEXT PRIMARY KEY,
    next_sequence INTEGER NOT NULL CHECK(next_sequence > 0),
    FOREIGN KEY (owner_session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE session_completion_deliveries (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    mapping_version TEXT NOT NULL,
    owner_session_id TEXT NOT NULL,
    delivery_sequence INTEGER NOT NULL CHECK(delivery_sequence > 0),
    source_kind TEXT NOT NULL CHECK(source_kind IN ('agent_task', 'workflow')),
    terminal_state TEXT NOT NULL CHECK(terminal_state IN ('succeeded', 'failed', 'canceled', 'interrupted')),
    outcome_ref TEXT NOT NULL,
    envelope_json TEXT NOT NULL CHECK(json_valid(envelope_json)),
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending', 'consumed')),
    created_at TEXT NOT NULL,
    consumed_at TEXT,
    UNIQUE(owner_session_id, event_id),
    UNIQUE(owner_session_id, delivery_sequence),
    FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE RESTRICT,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE session_completion_entry_deliveries (
    entry_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL UNIQUE,
    PRIMARY KEY(entry_id, delivery_id),
    FOREIGN KEY (entry_id) REFERENCES session_entries(id) ON DELETE CASCADE,
    FOREIGN KEY (delivery_id) REFERENCES session_completion_deliveries(id) ON DELETE CASCADE
);

CREATE TABLE completion_projection_diagnostics (
    owner_session_id TEXT NOT NULL,
    code TEXT NOT NULL,
    occurrences INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(owner_session_id, code),
    FOREIGN KEY (owner_session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_session_completion_pending
    ON session_completion_deliveries(owner_session_id, state, delivery_sequence);
CREATE INDEX idx_session_completion_global_pending
    ON session_completion_deliveries(state, created_at, id);
CREATE INDEX idx_tasks_completion_repair
    ON tasks(kind, state, finished_at, id);
CREATE INDEX idx_events_completion_repair
    ON events(created_at, id, kind);

-- +goose Down
DROP INDEX IF EXISTS idx_events_completion_repair;
DROP INDEX IF EXISTS idx_tasks_completion_repair;
DROP INDEX IF EXISTS idx_session_completion_global_pending;
DROP INDEX IF EXISTS idx_session_completion_pending;
DROP TABLE IF EXISTS completion_projection_diagnostics;
DROP TABLE IF EXISTS session_completion_entry_deliveries;
DROP TABLE IF EXISTS session_completion_deliveries;
DROP TABLE IF EXISTS session_completion_sequences;
