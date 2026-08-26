package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"

	zog "github.com/Oudwins/zog"
	"github.com/gofrs/uuid/v5"
)

func validateSessionEntity(entity *SessionEntity) error {
	if entity == nil {
		return errors.New("session is required")
	}

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
	if entity == nil {
		return errors.New("entry is required")
	}

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

const (
	maxMessageImages      = 4
	maxMessageImageBytes  = 5 << 20
	maxMessageImageTotal  = 20 << 20
	maxMessageImagePixels = 40_000_000
)

func validateSessionMessageEntity(entity *SessionMessageEntity) error {
	if entity == nil {
		return errors.New("message is required")
	}

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

	if err := validateMessageContent(entity); err != nil {
		return err
	}

	return validateRequiredTime("message.created_at", entity.CreatedAt)
}

func validateMessageContent(entity *SessionMessageEntity) error {
	if err := validateMessageParts(entity.Parts); err != nil {
		return err
	}

	if len(entity.Parts) > 0 && entity.Content != messagePartsText(entity.Parts) {
		return errors.New("message.content must match the text projection of message.parts")
	}

	if len(entity.Parts) == 0 && strings.TrimSpace(entity.Content) != "" {
		return errors.New("message.parts must contain nonblank message.content")
	}

	return nil
}

func messagePartsText(parts []MessagePartEntity) string {
	var text strings.Builder

	for index := range parts {
		if parts[index].Type == MessagePartText {
			text.WriteString(parts[index].Text)
		}
	}

	return text.String()
}

func validateMessageParts(parts []MessagePartEntity) error {
	imageCount := 0
	imageBytes := 0

	for index := range parts {
		part := &parts[index]
		if err := validateMessagePartEntity(part); err != nil {
			return fmt.Errorf("message.parts[%d]: %w", index, err)
		}

		if part.Type == MessagePartImage {
			imageCount++
			imageBytes += len(part.Data)
		}
	}

	if imageCount > maxMessageImages {
		return fmt.Errorf("message has %d images; maximum is %d", imageCount, maxMessageImages)
	}

	if imageBytes > maxMessageImageTotal {
		return errors.New("message image data exceeds the 20 MiB limit")
	}

	return nil
}

func validateMessagePartEntity(part *MessagePartEntity) error {
	if part == nil {
		return errors.New("message part is required")
	}

	switch part.Type {
	case MessagePartText:
		return validateTextMessagePart(part)
	case MessagePartImage:
		return validateImageMessagePart(part)
	default:
		return fmt.Errorf("unsupported message part type %q", part.Type)
	}
}

func validateTextMessagePart(part *MessagePartEntity) error {
	if strings.TrimSpace(part.Text) == "" {
		return errors.New("text part must have text")
	}

	if len(part.Data) != 0 {
		return errors.New("text part must not have binary data")
	}

	return nil
}

func validateImageMessagePart(part *MessagePartEntity) error {
	if len(part.Data) == 0 {
		return errors.New("image part must have binary data")
	}

	if part.Text != "" {
		return errors.New("image part must not have text")
	}

	if !validImageMIMEType(part.MIMEType) {
		return errors.New("image part must have a normalized image MIME type")
	}

	if len(part.Data) > maxMessageImageBytes {
		return errors.New("image part exceeds the 5 MiB limit")
	}

	if part.Width <= 0 || part.Height <= 0 || part.Width > maxMessageImagePixels/part.Height {
		return errors.New("image part dimensions must be positive and at most 40 megapixels")
	}

	return nil
}

func validImageMIMEType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)

	return err == nil && mediaType == value && value == strings.ToLower(value) &&
		strings.HasPrefix(value, "image/") && len(value) > len("image/") &&
		!strings.Contains(value, "*")
}

func validateTaskEntity(entity *TaskEntity) error {
	if entity == nil {
		return errors.New("task is required")
	}

	if err := validateUUIDv7("task.id", entity.ID); err != nil {
		return err
	}

	if entity.ParentTaskID != "" {
		if err := validateUUIDv7("task.parent_task_id", entity.ParentTaskID); err != nil {
			return err
		}
	}

	if err := validateUUIDv7("task.owner_session_id", entity.OwnerSessionID); err != nil {
		return err
	}

	if err := validateRequiredText("task.kind", entity.Kind); err != nil {
		return err
	}

	if err := validateRequiredText("task.state", string(entity.State)); err != nil {
		return err
	}

	if err := validateRequiredTime("task.created_at", entity.CreatedAt); err != nil {
		return err
	}

	return validateRequiredTime("task.updated_at", entity.UpdatedAt)
}

func validateWorkflowRunEntity(entity *WorkflowRunEntity) error {
	if entity == nil {
		return errors.New("workflow run is required")
	}

	if err := validateTaskEntity(&entity.Task); err != nil {
		return err
	}

	if entity.Task.Kind != TaskKindWorkflow {
		return errors.New("workflow_run.task.kind must be workflow")
	}

	if err := validateRequiredText("workflow_run.source", entity.Source); err != nil {
		return err
	}

	if err := validateRequiredText("workflow_run.source_hash", entity.SourceHash); err != nil {
		return err
	}

	var arguments map[string]any
	if err := json.Unmarshal([]byte(entity.ArgumentsJSON), &arguments); err != nil || arguments == nil {
		return errors.New("workflow_run.arguments_json must be a JSON object")
	}

	return nil
}

func validateAgentTaskEntity(entity *AgentTaskEntity) error {
	if entity == nil {
		return errors.New("agent task is required")
	}

	if err := validateTaskEntity(&entity.Task); err != nil {
		return err
	}

	if entity.Task.Kind != "agent" {
		return errors.New("agent_task.task.kind must be agent")
	}

	if err := validateUUIDv7("agent_task.child_session_id", entity.ChildSessionID); err != nil {
		return err
	}

	if err := validateRequiredText("agent_task.agent_name", entity.AgentName); err != nil {
		return err
	}

	if err := validateRequiredText("agent_task.prompt", entity.Prompt); err != nil {
		return err
	}

	if entity.Depth < 1 {
		return errors.New("agent_task.depth must be positive")
	}

	if !json.Valid([]byte(entity.PolicyJSON)) {
		return errors.New("agent_task.policy_json must be valid JSON")
	}

	if err := validateUsageJSON(entity.UsageJSON); err != nil {
		return err
	}

	return validateAgentOutputSchema(entity)
}

func validateUsageJSON(value string) error {
	var usage struct {
		Reported *bool `json:"reported"`
	}
	if err := json.Unmarshal([]byte(value), &usage); err != nil {
		return errors.New("agent_task.usage_json must be valid JSON")
	}

	if usage.Reported == nil {
		return errors.New("agent_task.usage_json reported field is required")
	}

	return nil
}

func validateAgentOutputSchema(entity *AgentTaskEntity) error {
	if (entity.OutputSchemaJSON == "") != (entity.OutputSchemaDigest == "") {
		return errors.New("agent_task output schema and digest must both be set")
	}

	return nil
}

func validateDocumentEntity(entity *DocumentEntity) error {
	if entity == nil {
		return errors.New("document is required")
	}

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

const uuidV7Version = 7

func validateUUIDv7(name, value string) error {
	trimmed := strings.TrimSpace(value)

	parsed, err := uuid.FromString(trimmed)
	if trimmed == "" {
		return fmt.Errorf("%s is required", name)
	}

	if err != nil {
		return fmt.Errorf("%s must be a UUIDv7", name)
	}

	if parsed.Version() != uuidV7Version {
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
