package database

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	zog "github.com/Oudwins/zog"
)

func validateSessionEntity(entity *SessionEntity) error {
	if err := validateRequiredText("session.id", entity.ID); err != nil {
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
	if err := validateRequiredText("entry.id", entity.ID); err != nil {
		return err
	}
	if err := validateRequiredText("entry.session_id", entity.SessionID); err != nil {
		return err
	}
	if err := validateRequiredText("entry.type", string(entity.Type)); err != nil {
		return err
	}
	if err := validateRequiredTime("entry.created_at", entity.CreatedAt); err != nil {
		return err
	}
	if !json.Valid([]byte(normalizeDataJSON(entity.DataJSON))) {
		return fmt.Errorf("entry.data_json must be valid JSON")
	}

	return nil
}

func validateSessionMessageEntity(entity *SessionMessageEntity) error {
	if err := validateRequiredText("message.id", entity.ID); err != nil {
		return err
	}
	if err := validateRequiredText("message.session_id", entity.SessionID); err != nil {
		return err
	}
	if err := validateRequiredText("message.entry_id", entity.EntryID); err != nil {
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
		return fmt.Errorf("document.value_json must be valid JSON")
	}

	return nil
}

func validateKSQLRequestEntity(entity *KSQLRequestEntity) error {
	return validateRequiredText("ksql.statement", entity.KSQL)
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
