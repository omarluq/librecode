-- +goose Up
UPDATE workflow_runs
SET guest_api_version = '2'
WHERE guest_api_version IN ('', '1');

UPDATE agent_tasks
SET usage_json = json_set(
    usage_json,
    '$.reported',
    json(CASE
        WHEN COALESCE(json_extract(usage_json, '$.input_tokens'), 0) > 0
          OR COALESCE(json_extract(usage_json, '$.output_tokens'), 0) > 0
          OR COALESCE(json_extract(usage_json, '$.provider_round_trips'), 0) > 0
        THEN 'true'
        ELSE 'false'
    END)
)
WHERE json_type(usage_json, '$.reported') IS NULL
   OR json_type(usage_json, '$.reported') = 'null';

INSERT INTO session_message_parts (id, session_id, entry_id, sequence, type, text)
SELECT 'canonical-text-' || message.entry_id,
       message.session_id,
       message.entry_id,
       0,
       'text',
       message.content
FROM session_messages AS message
WHERE trim(message.content) <> ''
  AND NOT EXISTS (
      SELECT 1
      FROM session_message_parts AS part
      WHERE part.entry_id = message.entry_id
  );

-- +goose Down
SELECT 1;
