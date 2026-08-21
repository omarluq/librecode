PRAGMA foreign_keys = ON;

INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at) VALUES
('01900000-0000-7000-8000-000000000001', '/fixture', 'workflow owner', '', '2026-01-02T03:04:05Z', '2026-01-02T03:04:10Z'), -- NOSONAR: exported compatibility data intentionally repeats stable IDs and timestamps.
('01900000-0000-7000-8000-000000000002', '/fixture', 'first child', '01900000-0000-7000-8000-000000000001', '2026-01-02T03:04:06Z', '2026-01-02T03:04:10Z'), -- NOSONAR: exported compatibility data intentionally repeats stable IDs and timestamps.
('01900000-0000-7000-8000-000000000003', '/fixture', 'second child', '01900000-0000-7000-8000-000000000001', '2026-01-02T03:04:07Z', '2026-01-02T03:04:10Z');

INSERT INTO tasks
(id, kind, state, parent_task_id, owner_session_id, concurrency_key, result, error_code, error_message,
 created_at, started_at, finished_at, updated_at, lease_owner, lease_expires_at) VALUES
('01900000-0000-7000-8000-000000000010', 'workflow', 'succeeded', NULL, '01900000-0000-7000-8000-000000000001', '', 'verified', '', '', '2026-01-02T03:04:05Z', '2026-01-02T03:04:06Z', '2026-01-02T03:04:10Z', '2026-01-02T03:04:10Z', NULL, NULL), -- NOSONAR: exported compatibility data intentionally repeats stable IDs and timestamps.
('01900000-0000-7000-8000-000000000011', 'agent', 'succeeded', '01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000001', '', 'found', '', '', '2026-01-02T03:04:06Z', '2026-01-02T03:04:07Z', '2026-01-02T03:04:08Z', '2026-01-02T03:04:08Z', NULL, NULL), -- NOSONAR: exported compatibility data intentionally repeats stable IDs and timestamps.
('01900000-0000-7000-8000-000000000012', 'agent', 'succeeded', '01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000001', '', 'verified', '', '', '2026-01-02T03:04:08Z', '2026-01-02T03:04:09Z', '2026-01-02T03:04:10Z', '2026-01-02T03:04:10Z', NULL, NULL); -- NOSONAR: exported compatibility data intentionally repeats stable IDs and timestamps.

INSERT INTO workflow_runs (task_id, source, source_hash, source_version, arguments_json, name) VALUES
('01900000-0000-7000-8000-000000000010', 'import "librecode/workflow"; workflow.List()', 'sha256:fixture', 'v1', '{"scope":"terminal states"}', 'compatibility fixture');

INSERT INTO agent_tasks
(task_id, child_session_id, agent_name, prompt, model, provider, policy_json, usage_json, depth) VALUES
('01900000-0000-7000-8000-000000000011', '01900000-0000-7000-8000-000000000002', 'explore', 'find TaskState', 'fixture-model', 'fixture-provider', '{}', '{"input_tokens":10,"output_tokens":4}', 1),
('01900000-0000-7000-8000-000000000012', '01900000-0000-7000-8000-000000000003', 'review', 'verify terminal states', 'fixture-model', 'fixture-provider', '{}', '{"input_tokens":12,"output_tokens":3}', 1);

INSERT INTO workflow_agent_tasks
(workflow_task_id, agent_task_id, sequence, node_key, invocation_index, created_at) VALUES
('01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000011', 1, 'inspect', 0, '2026-01-02T03:04:06Z'),
('01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000012', 2, 'inspect', 1, '2026-01-02T03:04:08Z');

INSERT INTO events (id, kind, payload_json, created_at) VALUES
('01900000-0000-7000-8000-000000000020', 'task_queued', '{}', '2026-01-02T03:04:05Z'),
('01900000-0000-7000-8000-000000000021', 'task_started', '{}', '2026-01-02T03:04:06Z'),
('01900000-0000-7000-8000-000000000022', 'task_succeeded', '{}', '2026-01-02T03:04:10Z');

INSERT INTO task_events (task_id, event_id, sequence) VALUES
('01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000020', 1),
('01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000021', 2),
('01900000-0000-7000-8000-000000000010', '01900000-0000-7000-8000-000000000022', 3);
