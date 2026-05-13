-- +goose Up
ALTER TABLE session_entries ADD COLUMN tool_name TEXT NOT NULL DEFAULT '';
ALTER TABLE session_entries ADD COLUMN tool_status TEXT NOT NULL DEFAULT '';
ALTER TABLE session_entries ADD COLUMN tool_args_json TEXT NOT NULL DEFAULT '';
ALTER TABLE session_entries ADD COLUMN token_estimate INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_entries ADD COLUMN model_facing INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_entries ADD COLUMN display INTEGER NOT NULL DEFAULT 1;
ALTER TABLE session_entries ADD COLUMN compaction_first_kept_entry_id TEXT NOT NULL DEFAULT '';
ALTER TABLE session_entries ADD COLUMN compaction_tokens_before INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_entries ADD COLUMN branch_from_entry_id TEXT NOT NULL DEFAULT '';

UPDATE session_entries
SET
    token_estimate = CASE
        WHEN length(coalesce(content, '')) = 0 AND length(coalesce(summary, '')) = 0 THEN 0
        ELSE max(1, (length(coalesce(content, '')) + length(coalesce(summary, '')) + 3) / 4)
    END,
    model_facing = CASE
        WHEN entry_type = 'message' AND role IN ('user', 'assistant') THEN 1
        WHEN entry_type = 'custom_message' THEN 1
        WHEN entry_type IN ('compaction', 'branch_summary') THEN 1
        ELSE 0
    END,
    display = CASE
        WHEN entry_type IN ('model_change', 'thinking_level_change', 'label', 'session_info') THEN 0
        ELSE 1
    END,
    tool_name = CASE
        WHEN role = 'toolResult' AND instr(coalesce(content, ''), 'tool: ') = 1 THEN trim(substr(coalesce(content, ''), 7, CASE WHEN instr(coalesce(content, ''), char(10)) > 0 THEN instr(coalesce(content, ''), char(10)) - 7 ELSE length(coalesce(content, '')) END))
        ELSE ''
    END,
    tool_status = CASE
        WHEN role = 'toolResult' AND instr(coalesce(content, ''), char(10) || 'error:') > 0 THEN 'error'
        WHEN role = 'toolResult' THEN 'success'
        ELSE ''
    END,
    compaction_first_kept_entry_id = CASE
        WHEN entry_type = 'compaction' AND json_valid(data_json) THEN coalesce(json_extract(data_json, '$.firstKeptEntryId'), '')
        ELSE ''
    END,
    compaction_tokens_before = CASE
        WHEN entry_type = 'compaction' AND json_valid(data_json) THEN coalesce(json_extract(data_json, '$.tokensBefore'), 0)
        ELSE 0
    END,
    tool_args_json = CASE
        WHEN role = 'toolResult' AND instr(coalesce(content, ''), char(10) || 'arguments:') > 0 THEN trim(
            CASE
                WHEN instr(substr(coalesce(content, ''), instr(coalesce(content, ''), char(10) || 'arguments:') + length(char(10) || 'arguments:')), char(10)) > 0
                    THEN substr(
                        substr(coalesce(content, ''), instr(coalesce(content, ''), char(10) || 'arguments:') + length(char(10) || 'arguments:')),
                        1,
                        instr(substr(coalesce(content, ''), instr(coalesce(content, ''), char(10) || 'arguments:') + length(char(10) || 'arguments:')), char(10)) - 1
                    )
                ELSE substr(coalesce(content, ''), instr(coalesce(content, ''), char(10) || 'arguments:') + length(char(10) || 'arguments:'))
            END
        )
        ELSE ''
    END,
    branch_from_entry_id = CASE
        WHEN entry_type = 'branch_summary' AND json_valid(data_json) THEN coalesce(json_extract(data_json, '$.fromId'), '')
        ELSE ''
    END;

CREATE INDEX IF NOT EXISTS idx_session_entries_model_facing ON session_entries(session_id, model_facing, created_at);
CREATE INDEX IF NOT EXISTS idx_session_entries_tool_name ON session_entries(session_id, tool_name, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_session_entries_tool_name;
DROP INDEX IF EXISTS idx_session_entries_model_facing;
ALTER TABLE session_entries DROP COLUMN branch_from_entry_id;
ALTER TABLE session_entries DROP COLUMN compaction_tokens_before;
ALTER TABLE session_entries DROP COLUMN compaction_first_kept_entry_id;
ALTER TABLE session_entries DROP COLUMN display;
ALTER TABLE session_entries DROP COLUMN model_facing;
ALTER TABLE session_entries DROP COLUMN token_estimate;
ALTER TABLE session_entries DROP COLUMN tool_args_json;
ALTER TABLE session_entries DROP COLUMN tool_status;
ALTER TABLE session_entries DROP COLUMN tool_name;
