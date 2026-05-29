-- +goose Up
CREATE TABLE IF NOT EXISTS uuid_v7_pattern (pattern TEXT NOT NULL);
INSERT INTO uuid_v7_pattern (pattern) VALUES ('[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-7[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-[89aAbB][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]-[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]');

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_sessions_id_uuid_insert
BEFORE INSERT ON sessions
FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'sessions.id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_sessions_id_uuid_update
BEFORE UPDATE OF id ON sessions
FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'sessions.id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_sessions_parent_session_uuid_insert
BEFORE INSERT ON sessions
FOR EACH ROW
WHEN NEW.parent_session <> '' AND NEW.parent_session NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'sessions.parent_session must be empty or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_sessions_parent_session_uuid_update
BEFORE UPDATE OF parent_session ON sessions
FOR EACH ROW
WHEN NEW.parent_session <> '' AND NEW.parent_session NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'sessions.parent_session must be empty or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_session_id_uuid_insert
BEFORE INSERT ON session_entries
FOR EACH ROW
WHEN NEW.session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.session_id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_session_id_uuid_update
BEFORE UPDATE OF session_id ON session_entries
FOR EACH ROW
WHEN NEW.session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.session_id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_id_uuid_insert
BEFORE INSERT ON session_entries
FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_id_uuid_update
BEFORE UPDATE OF id ON session_entries
FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_parent_id_uuid_insert
BEFORE INSERT ON session_entries
FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL AND NEW.parent_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.parent_id must be NULL or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_parent_id_uuid_update
BEFORE UPDATE OF parent_id ON session_entries
FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL AND NEW.parent_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.parent_id must be NULL or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_compaction_first_kept_entry_id_uuid_insert
BEFORE INSERT ON session_entries
FOR EACH ROW
WHEN NEW.compaction_first_kept_entry_id <> '' AND NEW.compaction_first_kept_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.compaction_first_kept_entry_id must be empty or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_compaction_first_kept_entry_id_uuid_update
BEFORE UPDATE OF compaction_first_kept_entry_id ON session_entries
FOR EACH ROW
WHEN NEW.compaction_first_kept_entry_id <> '' AND NEW.compaction_first_kept_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.compaction_first_kept_entry_id must be empty or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_branch_from_entry_id_uuid_insert
BEFORE INSERT ON session_entries
FOR EACH ROW
WHEN NEW.branch_from_entry_id <> '' AND NEW.branch_from_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.branch_from_entry_id must be empty or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_entries_branch_from_entry_id_uuid_update
BEFORE UPDATE OF branch_from_entry_id ON session_entries
FOR EACH ROW
WHEN NEW.branch_from_entry_id <> '' AND NEW.branch_from_entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_entries.branch_from_entry_id must be empty or a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_messages_id_uuid_insert
BEFORE INSERT ON session_messages
FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_messages.id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_messages_id_uuid_update
BEFORE UPDATE OF id ON session_messages
FOR EACH ROW
WHEN NEW.id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_messages.id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_messages_session_id_uuid_insert
BEFORE INSERT ON session_messages
FOR EACH ROW
WHEN NEW.session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_messages.session_id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_messages_session_id_uuid_update
BEFORE UPDATE OF session_id ON session_messages
FOR EACH ROW
WHEN NEW.session_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_messages.session_id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_messages_entry_id_uuid_insert
BEFORE INSERT ON session_messages
FOR EACH ROW
WHEN NEW.entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_messages.entry_id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS validate_session_messages_entry_id_uuid_update
BEFORE UPDATE OF entry_id ON session_messages
FOR EACH ROW
WHEN NEW.entry_id NOT GLOB (SELECT pattern FROM uuid_v7_pattern LIMIT 1)
BEGIN
    SELECT RAISE(ABORT, 'session_messages.entry_id must be a UUIDv7');
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS uuid_v7_pattern;
DROP TRIGGER IF EXISTS validate_session_messages_entry_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_messages_entry_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_messages_session_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_messages_session_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_messages_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_messages_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_branch_from_entry_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_branch_from_entry_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_compaction_first_kept_entry_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_compaction_first_kept_entry_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_parent_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_parent_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_session_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_session_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_session_entries_id_uuid_update;
DROP TRIGGER IF EXISTS validate_session_entries_id_uuid_insert;
DROP TRIGGER IF EXISTS validate_sessions_parent_session_uuid_update;
DROP TRIGGER IF EXISTS validate_sessions_parent_session_uuid_insert;
DROP TRIGGER IF EXISTS validate_sessions_id_uuid_update;
DROP TRIGGER IF EXISTS validate_sessions_id_uuid_insert;
