-- Migration 23 is intentionally irreversible. Operators must stop librecode, make and
-- integrity-check a manual backup, and provide measured free-space headroom before Up.
-- Goose runs this file in one transaction. foreign_keys must already be enabled; turning
-- it off in a transaction is both ineffective and unsafe.
-- +goose Up

-- Migration 5 introduced this lookup table for UUID validation triggers. Some v22
-- databases can lack it while retaining those triggers, so restore its canonical
-- contents before preflight and before recreating the final triggers.
CREATE TABLE IF NOT EXISTS uuid_v7_pattern (pattern TEXT NOT NULL);
-- Reset every canonical-pattern row. The predicate is exhaustive because pattern is NOT NULL.
DELETE FROM uuid_v7_pattern WHERE pattern IS NOT NULL;
INSERT INTO uuid_v7_pattern (pattern) VALUES ('[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-7[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-[89aAbB][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]');

CREATE TEMP TABLE migration_23_assert (failed INTEGER NOT NULL);
-- +goose StatementBegin
CREATE TEMP TRIGGER migration_23_abort
AFTER INSERT ON migration_23_assert
FOR EACH ROW WHEN NEW.failed <> 0
BEGIN
    SELECT RAISE(ABORT, 'migration 23 preflight or verification failed at assertion ' || NEW.rowid);
END;
-- +goose StatementEnd

-- All assertions below are sentinel writes through an aborting trigger. A bare SELECT,
-- out-of-trigger RAISE(), or non-aborting PRAGMA is not used as an invariant check.
INSERT INTO migration_23_assert SELECT foreign_keys <> 1 FROM pragma_foreign_keys;
-- Check only tables whose rows or foreign-key definitions this migration rewrites. A
-- database-wide check scans unrelated multi-gigabyte task and event payload tables.
INSERT INTO migration_23_assert SELECT
 EXISTS(SELECT 1 FROM pragma_foreign_key_check('sessions'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_entries'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_messages'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_message_parts'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_completion_entry_deliveries'));

-- Graph ownership and retained duplicated-envelope preflight. Legacy message sender and
-- created_at projections are intentionally discarded, so differences in those fields do
-- not block normalization.
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM sessions AS child
    WHERE child.parent_session <> ''
      AND NOT EXISTS (SELECT 1 FROM sessions AS parent WHERE parent.id = child.parent_session)
);
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM session_entries AS child
    LEFT JOIN sessions AS owner ON owner.id = child.session_id
    LEFT JOIN session_entries AS parent ON parent.id = child.parent_id
    WHERE owner.id IS NULL
       OR (child.parent_id IS NOT NULL AND (parent.id IS NULL OR parent.session_id <> child.session_id))
);
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM session_entries AS entry
    LEFT JOIN session_messages AS message ON message.entry_id = entry.id
    WHERE (entry.role <> '' AND message.entry_id IS NULL)
       OR (entry.role = '' AND message.entry_id IS NOT NULL)
);
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1
    FROM session_messages AS message
    LEFT JOIN session_entries AS entry ON entry.id = message.entry_id
    WHERE entry.id IS NULL
       OR message.session_id <> entry.session_id
       OR message.role <> entry.role
       OR message.provider <> entry.provider
       OR message.model <> entry.model
       OR message.content <> entry.content
);

-- Parts must be uniquely and nonnegatively ordered, owned by the same entry/session, and
-- have an envelope. Their ordered text projection is the sole scalar-content source.
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM session_message_parts AS part
    LEFT JOIN session_entries AS entry ON entry.id = part.entry_id
    LEFT JOIN session_messages AS message ON message.entry_id = part.entry_id
    WHERE entry.id IS NULL OR message.entry_id IS NULL
       OR part.session_id <> entry.session_id
       OR part.session_id <> message.session_id
       OR typeof(part.sequence) <> 'integer' OR part.sequence < 0
);
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM session_message_parts
    GROUP BY entry_id, sequence HAVING count(*) <> 1
);
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM session_messages AS message
    WHERE message.content <> COALESCE((
        SELECT group_concat(ordered.text, '')
        FROM (
            SELECT part.text
            FROM session_message_parts AS part
            WHERE part.session_id = message.session_id
              AND part.entry_id = message.entry_id
              AND part.type = 'text'
            ORDER BY part.sequence
        ) AS ordered
    ), '')
);

-- Metadata JSON must be an object. CASE is deliberate: malformed JSON is never passed to
-- json_type/json_extract. Every present non-null alias is checked independently.
INSERT INTO migration_23_assert
SELECT EXISTS (
    SELECT 1 FROM session_entries
    WHERE CASE WHEN NOT json_valid(data_json) THEN 1 ELSE json_type(data_json) <> 'object' END
);

-- String projection keys. Empty/whitespace strings are values; tool_args_json compares
-- as decoded string bytes, not as semantically normalized JSON text.
INSERT INTO migration_23_assert
SELECT EXISTS (
 SELECT 1 FROM session_entries AS e WHERE
 (CASE WHEN json_valid(e.data_json) THEN
   (json_type(e.data_json,'$.tool_name') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.tool_name')='text' AND json_extract(e.data_json,'$.tool_name')<>e.tool_name) OR
    json_type(e.data_json,'$.tool_status') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.tool_status')='text' AND json_extract(e.data_json,'$.tool_status')<>e.tool_status) OR
    json_type(e.data_json,'$.tool_args_json') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.tool_args_json')='text' AND json_extract(e.data_json,'$.tool_args_json')<>e.tool_args_json))
  ELSE 0 END)
);

-- Integer and boolean projection keys reject reals, numeric strings, and booleans/numbers
-- used in place of the required JSON type. Typed booleans must themselves be 0 or 1.
INSERT INTO migration_23_assert
SELECT EXISTS (
 SELECT 1 FROM session_entries AS e WHERE
 e.model_facing NOT IN (0,1) OR e.display NOT IN (0,1) OR
 (CASE WHEN json_valid(e.data_json) THEN
   (json_type(e.data_json,'$.token_estimate') NOT IN ('integer','null') OR
    (json_type(e.data_json,'$.token_estimate')='integer' AND json_extract(e.data_json,'$.token_estimate')<>e.token_estimate) OR
    json_type(e.data_json,'$.model_facing') NOT IN ('true','false','null') OR
    (json_type(e.data_json,'$.model_facing') IN ('true','false') AND json_extract(e.data_json,'$.model_facing')<>e.model_facing) OR
    json_type(e.data_json,'$.display') NOT IN ('true','false','null') OR
    (json_type(e.data_json,'$.display') IN ('true','false') AND json_extract(e.data_json,'$.display')<>e.display) OR
    json_type(e.data_json,'$.compaction_tokens_before') NOT IN ('integer','null') OR
    (json_type(e.data_json,'$.compaction_tokens_before')='integer' AND json_extract(e.data_json,'$.compaction_tokens_before')<>e.compaction_tokens_before) OR
    json_type(e.data_json,'$.tokens_before') NOT IN ('integer','null') OR
    (json_type(e.data_json,'$.tokens_before')='integer' AND json_extract(e.data_json,'$.tokens_before')<>e.compaction_tokens_before) OR
    json_type(e.data_json,'$.tokensBefore') NOT IN ('integer','null') OR
    (json_type(e.data_json,'$.tokensBefore')='integer' AND json_extract(e.data_json,'$.tokensBefore')<>e.compaction_tokens_before))
  ELSE 0 END)
);

-- ID aliases are exact strings and every nonempty value must independently be UUIDv7.
INSERT INTO migration_23_assert
SELECT EXISTS (
 SELECT 1 FROM session_entries AS e WHERE CASE WHEN json_valid(e.data_json) THEN
   (json_type(e.data_json,'$.compaction_first_kept_entry_id') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.compaction_first_kept_entry_id')='text' AND
      (json_extract(e.data_json,'$.compaction_first_kept_entry_id')<>e.compaction_first_kept_entry_id OR
       (json_extract(e.data_json,'$.compaction_first_kept_entry_id')<>'' AND json_extract(e.data_json,'$.compaction_first_kept_entry_id') NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)))) OR
    json_type(e.data_json,'$.first_kept_entry_id') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.first_kept_entry_id')='text' AND
      (json_extract(e.data_json,'$.first_kept_entry_id')<>e.compaction_first_kept_entry_id OR
       (json_extract(e.data_json,'$.first_kept_entry_id')<>'' AND json_extract(e.data_json,'$.first_kept_entry_id') NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)))) OR
    json_type(e.data_json,'$.firstKeptEntryId') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.firstKeptEntryId')='text' AND
      (json_extract(e.data_json,'$.firstKeptEntryId')<>e.compaction_first_kept_entry_id OR
       (json_extract(e.data_json,'$.firstKeptEntryId')<>'' AND json_extract(e.data_json,'$.firstKeptEntryId') NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)))) OR
    json_type(e.data_json,'$.branch_from_entry_id') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.branch_from_entry_id')='text' AND
      (json_extract(e.data_json,'$.branch_from_entry_id')<>e.branch_from_entry_id OR
       (json_extract(e.data_json,'$.branch_from_entry_id')<>'' AND json_extract(e.data_json,'$.branch_from_entry_id') NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)))) OR
    json_type(e.data_json,'$.from_id') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.from_id')='text' AND
      (json_extract(e.data_json,'$.from_id')<>e.branch_from_entry_id OR
       (json_extract(e.data_json,'$.from_id')<>'' AND json_extract(e.data_json,'$.from_id') NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)))) OR
    json_type(e.data_json,'$.fromId') NOT IN ('text','null') OR
    (json_type(e.data_json,'$.fromId')='text' AND
      (json_extract(e.data_json,'$.fromId')<>e.branch_from_entry_id OR
       (json_extract(e.data_json,'$.fromId')<>'' AND json_extract(e.data_json,'$.fromId') NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)))))
 ELSE 0 END
);

-- Preserve comparison totals before rebuilding. The migration intentionally leaves every
-- table outside the normalized entry/message graph untouched.
CREATE TEMP TABLE migration_23_audit AS
SELECT
 (SELECT count(*) FROM sessions) AS sessions_count,
 (SELECT count(*) FROM session_entries) AS entries_count,
 (SELECT count(*) FROM session_messages) AS messages_count,
 (SELECT count(*) FROM session_message_parts) AS parts_count,
 (SELECT total(length(text)) FROM session_message_parts) AS text_bytes,
 (SELECT total(length(data)) FROM session_message_parts) AS blob_bytes,
 (SELECT count(*) FROM session_completion_entry_deliveries) AS entry_deliveries_count;

PRAGMA defer_foreign_keys = ON;

-- Only objects owned by the tables being normalized are replaced. In particular, task,
-- event, completion-delivery, and workflow indexes and schemas are not canonicalized here.
DROP TRIGGER IF EXISTS validate_sessions_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_sessions_id_uuid_update;
DROP TRIGGER IF EXISTS validate_sessions_parent_session_uuid_insert;
DROP TRIGGER IF EXISTS validate_sessions_parent_session_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_session_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_session_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_parent_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_parent_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_compaction_first_kept_entry_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_compaction_first_kept_entry_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_branch_from_entry_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_branch_from_entry_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_messages_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_messages_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_messages_session_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_messages_session_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_messages_entry_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_messages_entry_id_uuid_update;

DROP INDEX IF EXISTS idx_sessions_cwd_updated;
DROP INDEX IF EXISTS idx_session_entries_session_created;
-- Keep idx_session_entries_parent until the legacy entries table is dropped. With
-- foreign keys enabled, SQLite implements DROP TABLE as an implicit DELETE; the
-- self-referencing parent FK needs this index to avoid a quadratic child-row scan.
DROP INDEX IF EXISTS idx_session_messages_session_created;
DROP INDEX IF EXISTS idx_session_messages_sender;
DROP INDEX IF EXISTS idx_session_entries_model_facing;
DROP INDEX IF EXISTS idx_session_entries_tool_name;
DROP INDEX IF EXISTS idx_session_entries_operation_id;
DROP INDEX IF EXISTS idx_session_entries_compaction_parent;
DROP INDEX IF EXISTS idx_session_message_parts_session_entry_sequence;
DROP INDEX IF EXISTS idx_session_message_parts_session_entry_image;

-- Adding and dropping columns preserves the sessions table identity and therefore leaves
-- all existing foreign keys that reference sessions unchanged. The nullable added column
-- can carry the normalized self-reference directly.
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL;
-- The new column is already NULL for root sessions, so only copy nonempty parent IDs.
UPDATE sessions SET parent_session_id=parent_session WHERE parent_session <> '';
ALTER TABLE sessions DROP COLUMN parent_session;

-- Rename the entry-delivery edge first so its FK can be rebuilt against the new entries.
-- No other session-referencing table is renamed or copied.
ALTER TABLE session_completion_entry_deliveries RENAME TO session_completion_entry_deliveries__v22_old;
ALTER TABLE session_message_parts RENAME TO session_message_parts__v22_old;
ALTER TABLE session_messages RENAME TO session_messages__v22_old;
ALTER TABLE session_entries RENAME TO session_entries__v22_old;

CREATE TABLE session_entries (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL, parent_id TEXT,
 entry_type TEXT NOT NULL, custom_type TEXT NOT NULL DEFAULT '',
 data_json TEXT NOT NULL DEFAULT '{}', summary TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
 tool_name TEXT NOT NULL DEFAULT '', tool_status TEXT NOT NULL DEFAULT '',
 tool_args_json TEXT NOT NULL DEFAULT '', token_estimate INTEGER NOT NULL DEFAULT 0,
 model_facing INTEGER NOT NULL DEFAULT 0, display INTEGER NOT NULL DEFAULT 1,
 compaction_first_kept_entry_id TEXT NOT NULL DEFAULT '', compaction_tokens_before INTEGER NOT NULL DEFAULT 0,
 branch_from_entry_id TEXT NOT NULL DEFAULT '', operation_id TEXT NOT NULL DEFAULT '',
 UNIQUE(id, session_id),
 FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE,
 FOREIGN KEY(parent_id, session_id) REFERENCES session_entries(id, session_id) ON DELETE CASCADE
);
CREATE TABLE session_messages (
 entry_id TEXT PRIMARY KEY REFERENCES session_entries(id) ON DELETE CASCADE,
 role TEXT NOT NULL, provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT ''
);
CREATE TABLE session_message_parts (
 entry_id TEXT NOT NULL REFERENCES session_messages(entry_id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL CHECK(typeof(sequence)='integer' AND sequence>=0),
 type TEXT NOT NULL CHECK(type IN ('text','image')), text TEXT NOT NULL DEFAULT '',
 mime_type TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', width INTEGER NOT NULL DEFAULT 0,
 height INTEGER NOT NULL DEFAULT 0, data BLOB, PRIMARY KEY(entry_id, sequence)
);
CREATE TABLE session_completion_entry_deliveries (
 entry_id TEXT NOT NULL, delivery_id TEXT NOT NULL UNIQUE, PRIMARY KEY(entry_id,delivery_id),
 FOREIGN KEY(entry_id) REFERENCES session_entries(id) ON DELETE CASCADE,
 FOREIGN KEY(delivery_id) REFERENCES session_completion_deliveries(id) ON DELETE CASCADE
);

INSERT INTO session_entries
SELECT id,session_id,parent_id,entry_type,custom_type,
 json_remove(data_json,
  '$.tool_name','$.tool_status','$.tool_args_json','$.token_estimate','$.model_facing','$.display',
  '$.compaction_first_kept_entry_id','$.first_kept_entry_id','$.firstKeptEntryId',
  '$.compaction_tokens_before','$.tokens_before','$.tokensBefore',
  '$.branch_from_entry_id','$.from_id','$.fromId'),
 summary,created_at,tool_name,tool_status,tool_args_json,token_estimate,model_facing,display,
 compaction_first_kept_entry_id,compaction_tokens_before,branch_from_entry_id,operation_id
FROM session_entries__v22_old;
INSERT INTO session_messages(entry_id,role,provider,model)
SELECT entry_id,role,provider,model FROM session_messages__v22_old;
INSERT INTO session_message_parts(entry_id,sequence,type,text,mime_type,name,width,height,data)
SELECT entry_id,sequence,type,text,mime_type,name,width,height,data FROM session_message_parts__v22_old;
INSERT INTO session_completion_entry_deliveries(entry_id,delivery_id)
SELECT entry_id,delivery_id FROM session_completion_entry_deliveries__v22_old;

-- Prove transformed identity and payload preservation before destructive drops.
INSERT INTO migration_23_assert SELECT
 (SELECT count(*) FROM sessions)<>(SELECT sessions_count FROM migration_23_audit)
 OR (SELECT count(*) FROM session_entries)<>(SELECT entries_count FROM migration_23_audit)
 OR (SELECT count(*) FROM session_messages)<>(SELECT messages_count FROM migration_23_audit)
 OR (SELECT count(*) FROM session_message_parts)<>(SELECT parts_count FROM migration_23_audit)
 OR (SELECT total(length(text)) FROM session_message_parts)<>(SELECT text_bytes FROM migration_23_audit)
 OR (SELECT total(length(data)) FROM session_message_parts)<>(SELECT blob_bytes FROM migration_23_audit)
 OR (SELECT count(*) FROM session_completion_entry_deliveries)<>(SELECT entry_deliveries_count FROM migration_23_audit);
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM session_entries__v22_old o LEFT JOIN session_entries n ON n.id=o.id
 WHERE n.id IS NULL OR n.session_id<>o.session_id OR n.parent_id IS NOT o.parent_id
    OR n.entry_type<>o.entry_type OR n.custom_type<>o.custom_type
    OR n.summary<>o.summary OR n.created_at<>o.created_at
    OR n.tool_name<>o.tool_name OR n.tool_status<>o.tool_status
    OR n.tool_args_json<>o.tool_args_json OR n.token_estimate<>o.token_estimate
    OR n.model_facing<>o.model_facing OR n.display<>o.display
    OR n.compaction_first_kept_entry_id<>o.compaction_first_kept_entry_id
    OR n.compaction_tokens_before<>o.compaction_tokens_before
    OR n.branch_from_entry_id<>o.branch_from_entry_id OR n.operation_id<>o.operation_id
);
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM session_messages__v22_old o LEFT JOIN session_messages n ON n.entry_id=o.entry_id
 WHERE n.entry_id IS NULL OR n.role<>o.role OR n.provider<>o.provider OR n.model<>o.model
);
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM session_message_parts__v22_old o LEFT JOIN session_message_parts n
 ON n.entry_id=o.entry_id AND n.sequence=o.sequence
 WHERE n.entry_id IS NULL OR n.type<>o.type OR n.mime_type<>o.mime_type
    OR n.name<>o.name OR n.width<>o.width OR n.height<>o.height
    OR length(n.text)<>length(o.text) OR length(n.data) IS NOT length(o.data)
);
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM session_completion_entry_deliveries__v22_old o
 LEFT JOIN session_completion_entry_deliveries n
 ON n.entry_id=o.entry_id AND n.delivery_id=o.delivery_id WHERE n.entry_id IS NULL
);
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM session_entries WHERE
 json_type(data_json,'$.tool_name') IS NOT NULL OR json_type(data_json,'$.tool_status') IS NOT NULL OR
 json_type(data_json,'$.tool_args_json') IS NOT NULL OR json_type(data_json,'$.token_estimate') IS NOT NULL OR
 json_type(data_json,'$.model_facing') IS NOT NULL OR json_type(data_json,'$.display') IS NOT NULL OR
 json_type(data_json,'$.compaction_first_kept_entry_id') IS NOT NULL OR json_type(data_json,'$.first_kept_entry_id') IS NOT NULL OR
 json_type(data_json,'$.firstKeptEntryId') IS NOT NULL OR json_type(data_json,'$.compaction_tokens_before') IS NOT NULL OR
 json_type(data_json,'$.tokens_before') IS NOT NULL OR json_type(data_json,'$.tokensBefore') IS NOT NULL OR
 json_type(data_json,'$.branch_from_entry_id') IS NOT NULL OR json_type(data_json,'$.from_id') IS NOT NULL OR
 json_type(data_json,'$.fromId') IS NOT NULL
);

DROP TABLE session_completion_entry_deliveries__v22_old;
DROP TABLE session_message_parts__v22_old;
DROP TABLE session_messages__v22_old;
DROP INDEX IF EXISTS idx_session_entries_id_session;
DROP TABLE session_entries__v22_old;

CREATE INDEX idx_sessions_cwd_parent_updated ON sessions(cwd,parent_session_id,updated_at DESC);
CREATE INDEX idx_sessions_parent_updated ON sessions(parent_session_id,updated_at DESC);
CREATE INDEX idx_session_entries_session_created_id ON session_entries(session_id,created_at,id);
CREATE INDEX idx_session_entries_transcript_cursor ON session_entries(session_id,created_at,id) WHERE display=1;
CREATE INDEX idx_session_entries_parent ON session_entries(parent_id);
CREATE INDEX idx_session_entries_model_facing ON session_entries(session_id,model_facing,created_at);
CREATE INDEX idx_session_entries_tool_name ON session_entries(session_id,tool_name,created_at);
CREATE UNIQUE INDEX idx_session_entries_operation_id ON session_entries(operation_id) WHERE operation_id<>'';
CREATE UNIQUE INDEX idx_session_entries_compaction_parent ON session_entries(session_id,COALESCE(parent_id,''))
 WHERE entry_type='compaction' AND operation_id<>'';
CREATE INDEX idx_session_message_parts_entry_image ON session_message_parts(entry_id) WHERE type='image';

-- +goose StatementBegin
CREATE TRIGGER validate_sessions_id_uuid_insert BEFORE INSERT ON sessions FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'sessions.id must be a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_sessions_id_uuid_update BEFORE UPDATE OF id ON sessions FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'sessions.id must be a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_sessions_parent_session_id_uuid_insert BEFORE INSERT ON sessions FOR EACH ROW
WHEN NEW.parent_session_id IS NOT NULL AND NEW.parent_session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'sessions.parent_session_id must be NULL or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_sessions_parent_session_id_uuid_update BEFORE UPDATE OF parent_session_id ON sessions FOR EACH ROW
WHEN NEW.parent_session_id IS NOT NULL AND NEW.parent_session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'sessions.parent_session_id must be NULL or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_session_id_uuid_insert BEFORE INSERT ON session_entries FOR EACH ROW
WHEN NEW.session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.session_id must be a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_session_id_uuid_update BEFORE UPDATE OF session_id ON session_entries FOR EACH ROW
WHEN NEW.session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.session_id must be a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_id_uuid_insert BEFORE INSERT ON session_entries FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.id must be a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_id_uuid_update BEFORE UPDATE OF id ON session_entries FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.id must be a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_parent_id_uuid_insert BEFORE INSERT ON session_entries FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL AND NEW.parent_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.parent_id must be NULL or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_parent_id_uuid_update BEFORE UPDATE OF parent_id ON session_entries FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL AND NEW.parent_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.parent_id must be NULL or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_compaction_first_kept_entry_id_uuid_insert BEFORE INSERT ON session_entries FOR EACH ROW
WHEN NEW.compaction_first_kept_entry_id<>'' AND NEW.compaction_first_kept_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.compaction_first_kept_entry_id must be empty or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_compaction_first_kept_entry_id_uuid_update BEFORE UPDATE OF compaction_first_kept_entry_id ON session_entries FOR EACH ROW
WHEN NEW.compaction_first_kept_entry_id<>'' AND NEW.compaction_first_kept_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.compaction_first_kept_entry_id must be empty or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_branch_from_entry_id_uuid_insert BEFORE INSERT ON session_entries FOR EACH ROW
WHEN NEW.branch_from_entry_id<>'' AND NEW.branch_from_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.branch_from_entry_id must be empty or a UUIDv7'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER validate_session_entries_branch_from_entry_id_uuid_update BEFORE UPDATE OF branch_from_entry_id ON session_entries FOR EACH ROW
WHEN NEW.branch_from_entry_id<>'' AND NEW.branch_from_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN SELECT RAISE(ABORT,'session_entries.branch_from_entry_id must be empty or a UUIDv7'); END;
-- +goose StatementEnd

-- Exact final FK, schema, index, and trigger checks. No final object may retain a temporary
-- target, and all untouched session references must still point at the original sessions.
INSERT INTO migration_23_assert SELECT
 EXISTS(SELECT 1 FROM pragma_foreign_key_check('sessions'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_entries'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_messages'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_message_parts'))
 OR EXISTS(SELECT 1 FROM pragma_foreign_key_check('session_completion_entry_deliveries'));
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM sqlite_schema WHERE name NOT LIKE '%__v22_old' AND sql LIKE '%__v22_old%'
);
INSERT INTO migration_23_assert SELECT
 (SELECT count(*) FROM pragma_foreign_key_list('sessions') WHERE "table"='sessions' AND "from"='parent_session_id' AND "to"='id' AND on_delete='SET NULL')<>1;
INSERT INTO migration_23_assert SELECT
 (SELECT count(DISTINCT id) FROM pragma_foreign_key_list('session_entries') WHERE on_delete='CASCADE')<>2
 OR (SELECT count(*) FROM pragma_foreign_key_list('session_entries') WHERE "table"='sessions' AND "from"='session_id' AND "to"='id' AND on_delete='CASCADE')<>1
 OR (SELECT count(*) FROM (
      SELECT id FROM pragma_foreign_key_list('session_entries')
      WHERE "table"='session_entries' AND on_delete='CASCADE' GROUP BY id
      HAVING count(*)=2
       AND sum(seq=0 AND "from"='parent_id' AND "to"='id')=1
       AND sum(seq=1 AND "from"='session_id' AND "to"='session_id')=1
    ))<>1;
INSERT INTO migration_23_assert SELECT
 (SELECT count(*) FROM pragma_foreign_key_list('session_messages') WHERE "table"='session_entries' AND "from"='entry_id' AND "to"='id' AND on_delete='CASCADE')<>1
 OR (SELECT count(*) FROM pragma_foreign_key_list('session_message_parts') WHERE "table"='session_messages' AND "from"='entry_id' AND "to"='entry_id' AND on_delete='CASCADE')<>1
 OR (SELECT count(*) FROM pragma_foreign_key_list('session_completion_entry_deliveries') WHERE "table"='session_entries' AND "from"='entry_id' AND "to"='id' AND on_delete='CASCADE')<>1
 OR (SELECT count(*) FROM pragma_foreign_key_list('session_completion_entry_deliveries') WHERE "table"='session_completion_deliveries' AND "from"='delivery_id' AND "to"='id' AND on_delete='CASCADE')<>1;
INSERT INTO migration_23_assert SELECT EXISTS(
 SELECT 1 FROM pragma_foreign_key_list('tasks') WHERE "from"='owner_session_id' AND "table"<>'sessions'
) OR EXISTS(
 SELECT 1 FROM pragma_foreign_key_list('agent_tasks') WHERE "from"='child_session_id' AND "table"<>'sessions'
) OR EXISTS(
 SELECT 1 FROM pragma_foreign_key_list('session_completion_deliveries') WHERE "from"='owner_session_id' AND "table"<>'sessions'
);
INSERT INTO migration_23_assert SELECT
 (SELECT count(*) FROM pragma_table_info('sessions'))<>6
 OR NOT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name='parent_session_id' AND "notnull"=0)
 OR EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name='parent_session')
 OR (SELECT count(*) FROM pragma_table_info('session_messages'))<>4
 OR (SELECT count(*) FROM pragma_table_info('session_message_parts'))<>9
 OR EXISTS(SELECT 1 FROM pragma_table_info('session_entries') WHERE name IN ('role','content','provider','model'));
INSERT INTO migration_23_assert SELECT
 (SELECT group_concat(name,',') FROM (SELECT name FROM pragma_index_info('idx_session_entries_session_created_id') ORDER BY seqno))<>'session_id,created_at,id'
 OR (SELECT group_concat(name,',') FROM (SELECT name FROM pragma_index_info('idx_session_entries_transcript_cursor') ORDER BY seqno))<>'session_id,created_at,id'
 OR (SELECT group_concat(name,',') FROM (SELECT name FROM pragma_index_info('idx_sessions_cwd_parent_updated') ORDER BY seqno))<>'cwd,parent_session_id,updated_at'
 OR (SELECT group_concat(name,',') FROM (SELECT name FROM pragma_index_info('idx_sessions_parent_updated') ORDER BY seqno))<>'parent_session_id,updated_at'
 OR (SELECT sql FROM sqlite_schema WHERE type='index' AND name='idx_session_entries_transcript_cursor') NOT LIKE '%WHERE display=1%'
 OR EXISTS(SELECT 1 FROM sqlite_schema WHERE type='index' AND name IN ('idx_session_messages_session_created','idx_session_messages_sender'));
INSERT INTO migration_23_assert SELECT
 (SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND name IN (
  'validate_sessions_id_uuid_insert','validate_sessions_id_uuid_update',
  'validate_sessions_parent_session_id_uuid_insert','validate_sessions_parent_session_id_uuid_update',
  'validate_session_entries_session_id_uuid_insert','validate_session_entries_session_id_uuid_update',
  'validate_session_entries_id_uuid_insert','validate_session_entries_id_uuid_update',
  'validate_session_entries_parent_id_uuid_insert','validate_session_entries_parent_id_uuid_update',
  'validate_session_entries_compaction_first_kept_entry_id_uuid_insert','validate_session_entries_compaction_first_kept_entry_id_uuid_update',
  'validate_session_entries_branch_from_entry_id_uuid_insert','validate_session_entries_branch_from_entry_id_uuid_update'))<>14
 OR EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name LIKE 'validate_session_messages_%')
 OR (SELECT sql FROM sqlite_schema WHERE type='trigger' AND name='validate_sessions_parent_session_id_uuid_insert') NOT LIKE '%NEW.parent_session_id IS NOT NULL%';

DROP TRIGGER migration_23_abort;
DROP TABLE migration_23_assert;
DROP TABLE migration_23_audit;

-- +goose Down
-- Irreversible by policy: an automatic rollback cannot reconstruct removed scalar/identity
-- columns or the original byte representation of cleaned metadata JSON.
CREATE TABLE migration_23_is_irreversible (
    guard INTEGER CONSTRAINT migration_23_is_irreversible CHECK (guard = 0)
);
INSERT INTO migration_23_is_irreversible (guard) VALUES (1);
