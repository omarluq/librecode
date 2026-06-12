package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	zog "github.com/Oudwins/zog"
	"github.com/gofrs/uuid/v5"
)

func validateSessionEntity(entity *SessionEntity) error {
	if err := validateUUIDv7("session.id", entity.ID); err != nil {
		return err
	}
	if err := validateRequiredText("session.cwd", entity.CWD); err != nil {
		return err
	}
	if err := validateRequiredTime("session.created_at", entity.CreatedAt); err != nil {
		return err
	}

	return validateRequiredTime("session.updated_at", entity.UpdatedAt)
}

func validateEntryEntity(entity *EntryEntity) error {
	if err := validateUUIDv7("entry.id", entity.ID); err != nil {
		return err
	}
	if err := validateUUIDv7("entry.session_id", entity.SessionID); err != nil {
		return err
	}
	if entity.ParentID != nil {
		if err := validateUUIDv7("entry.parent_id", *entity.ParentID); err != nil {
			return err
		}
	}
	if err := validateRequiredText("entry.type", string(entity.Type)); err != nil {
		return err
	}
	if err := validateRequiredTime("entry.created_at", entity.CreatedAt); err != nil {
		return err
	}
	if !json.Valid([]byte(normalizeDataJSON(entity.DataJSON))) {
		return errors.New("entry.data_json must be valid JSON")
	}

	return nil
}

func validateSessionMessageEntity(entity *SessionMessageEntity) error {
	if err := validateUUIDv7("message.id", entity.ID); err != nil {
		return err
	}
	if err := validateUUIDv7("message.session_id", entity.SessionID); err != nil {
		return err
	}
	if err := validateUUIDv7("message.entry_id", entity.EntryID); err != nil {
		return err
	}
	if err := validateRequiredText("message.sender", entity.Sender); err != nil {
		return err
	}
	if err := validateRequiredText("message.role", string(entity.Role)); err != nil {
		return err
	}

	return validateRequiredTime("message.created_at", entity.CreatedAt)
}

func validateDocumentEntity(entity *DocumentEntity) error {
	if err := validateRequiredText("document.namespace", entity.Namespace); err != nil {
		return err
	}
	if err := validateRequiredText("document.key", entity.Key); err != nil {
		return err
	}
	if !json.Valid([]byte(entity.ValueJSON)) {
		return errors.New("document.value_json must be valid JSON")
	}

	return nil
}

func validateUUIDv7(name, value string) error {
	trimmed := strings.TrimSpace(value)
	parsed, err := uuid.FromString(trimmed)
	if trimmed == "" {
		return fmt.Errorf("%s is required", name)
	}
	if err != nil {
		return fmt.Errorf("%s must be a UUIDv7", name)
	}
	if parsed.Version() != 7 {
		return fmt.Errorf("%s must be a UUIDv7", name)
	}

	return nil
}

func validateRequiredText(name, value string) error {
	trimmed := strings.TrimSpace(value)
	issues := zog.String().Required(zog.Message(name + " is required")).Validate(&trimmed)
	if len(issues) > 0 {
		return fmt.Errorf("%s", issues[0].Message)
	}

	return nil
}

func validateRequiredTime(name string, value time.Time) error {
	issues := zog.Time().Required(zog.Message(name + " is required")).Validate(&value)
	if len(issues) > 0 || value.IsZero() {
		return fmt.Errorf("%s is required", name)
	}

	return nil
}
