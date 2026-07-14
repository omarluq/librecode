package database

import (
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	invalidValue = "bad"
	validCase    = "valid"
)

func TestCoreEntityValidationBehavior(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	entityID := mustTestUUIDv7(t)
	tests := coreEntityValidationCases(entityID, now)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.validate()
			if test.wantError == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorContains(t, err, test.wantError)
		})
	}
}

type entityValidationCase struct {
	name      string
	validate  func() error
	wantError string
}

func coreEntityValidationCases(entityID string, now time.Time) []entityValidationCase {
	cases := make([]entityValidationCase, 0, 17)
	cases = append(cases,
		entityValidationCase{name: "valid session", validate: func() error {
			entity := validSessionEntity(entityID, now)

			return validateSessionEntity(&entity)
		}, wantError: ""},
		entityValidationCase{name: "session id", validate: func() error {
			entity := validSessionEntity(entityID, now)
			entity.ID = ""

			return validateSessionEntity(&entity)
		}, wantError: "session.id is required"},
		entityValidationCase{name: "session created", validate: func() error {
			entity := validSessionEntity(entityID, now)
			entity.CreatedAt = time.Time{}

			return validateSessionEntity(&entity)
		}, wantError: "session.created_at is required"},
		entityValidationCase{name: "session updated", validate: func() error {
			entity := validSessionEntity(entityID, now)
			entity.UpdatedAt = time.Time{}

			return validateSessionEntity(&entity)
		}, wantError: "session.updated_at is required"},
		entityValidationCase{name: "valid entry", validate: func() error {
			entity := validEntryEntity(entityID, now)

			return validateEntryEntity(&entity)
		}, wantError: ""},
		entityValidationCase{name: "entry id", validate: func() error {
			entity := validEntryEntity(entityID, now)
			entity.ID = ""

			return validateEntryEntity(&entity)
		}, wantError: "entry.id is required"},
		entityValidationCase{name: "entry session", validate: func() error {
			entity := validEntryEntity(entityID, now)
			entity.SessionID = ""

			return validateEntryEntity(&entity)
		}, wantError: "entry.session_id is required"},
		entityValidationCase{name: "entry parent", validate: func() error {
			entity := validEntryEntity(entityID, now)
			parent := invalidValue
			entity.ParentID = &parent

			return validateEntryEntity(&entity)
		}, wantError: "entry.parent_id must be a UUIDv7"},
		entityValidationCase{name: "entry type", validate: func() error {
			entity := validEntryEntity(entityID, now)
			entity.Type = ""

			return validateEntryEntity(&entity)
		}, wantError: "entry.type is required"},
		entityValidationCase{name: "entry created", validate: func() error {
			entity := validEntryEntity(entityID, now)
			entity.CreatedAt = time.Time{}

			return validateEntryEntity(&entity)
		}, wantError: "entry.created_at is required"},
		entityValidationCase{name: "entry data", validate: func() error {
			entity := validEntryEntity(entityID, now)
			entity.DataJSON = `{`

			return validateEntryEntity(&entity)
		}, wantError: "entry.data_json must be valid JSON"},
	)

	return append(cases, messageValidationCases(entityID, now)...)
}

func messageValidationCases(entityID string, now time.Time) []entityValidationCase {
	return []entityValidationCase{
		{name: "valid message", validate: func() error {
			entity := validMessageEntity(entityID, now)

			return validateSessionMessageEntity(&entity)
		}, wantError: ""},
		{name: "message id", validate: func() error {
			entity := validMessageEntity(entityID, now)
			entity.ID = ""

			return validateSessionMessageEntity(&entity)
		}, wantError: "message.id is required"},
		{name: "message session", validate: func() error {
			entity := validMessageEntity(entityID, now)
			entity.SessionID = ""

			return validateSessionMessageEntity(&entity)
		}, wantError: "message.session_id is required"},
		{name: "message entry", validate: func() error {
			entity := validMessageEntity(entityID, now)
			entity.EntryID = ""

			return validateSessionMessageEntity(&entity)
		}, wantError: "message.entry_id is required"},
		{name: "message role", validate: func() error {
			entity := validMessageEntity(entityID, now)
			entity.Role = ""

			return validateSessionMessageEntity(&entity)
		}, wantError: "message.role is required"},
		{name: "message created", validate: func() error {
			entity := validMessageEntity(entityID, now)
			entity.CreatedAt = time.Time{}

			return validateSessionMessageEntity(&entity)
		}, wantError: "message.created_at is required"},
	}
}

func TestTaskValidationBehavior(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	entityID := mustTestUUIDv7(t)

	tests := []struct {
		name      string
		mutate    func(*TaskEntity)
		wantError string
	}{
		{name: validCase, mutate: nil, wantError: ""},
		{name: "missing id", mutate: func(task *TaskEntity) { task.ID = "" }, wantError: "task.id is required"},
		{name: "non-v7 id", mutate: func(task *TaskEntity) { task.ID = uuid.Must(uuid.NewV4()).String() },
			wantError: "task.id must be a UUIDv7"},
		{name: "invalid parent", mutate: func(task *TaskEntity) { task.ParentTaskID = "invalid" },
			wantError: "task.parent_task_id must be a UUIDv7"},
		{name: "invalid owner", mutate: func(task *TaskEntity) { task.OwnerSessionID = "invalid" },
			wantError: "task.owner_session_id must be a UUIDv7"},
		{name: "missing kind", mutate: func(task *TaskEntity) { task.Kind = " " }, wantError: "task.kind is required"},
		{name: "missing state", mutate: func(task *TaskEntity) { task.State = "" },
			wantError: "task.state is required"},
		{name: "missing created time", mutate: func(task *TaskEntity) { task.CreatedAt = time.Time{} },
			wantError: "task.created_at is required"},
		{name: "missing updated time", mutate: func(task *TaskEntity) { task.UpdatedAt = time.Time{} },
			wantError: "task.updated_at is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			entity := validTaskEntity(entityID, now)
			if test.mutate != nil {
				test.mutate(&entity)
			}

			err := validateTaskEntity(&entity)
			if test.wantError == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestAgentTaskValidationBehavior(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	entityID := mustTestUUIDv7(t)

	tests := []struct {
		name      string
		mutate    func(*AgentTaskEntity)
		wantError string
	}{
		{name: validCase, mutate: nil, wantError: ""},
		{name: "invalid generic task", mutate: func(task *AgentTaskEntity) { task.Task.ID = invalidValue },
			wantError: "task.id must be a UUIDv7"},
		{name: "wrong kind", mutate: func(task *AgentTaskEntity) { task.Task.Kind = "other" },
			wantError: "task.kind must be agent"},
		{name: "invalid child", mutate: func(task *AgentTaskEntity) { task.ChildSessionID = invalidValue },
			wantError: "child_session_id must be a UUIDv7"},
		{name: "missing agent", mutate: func(task *AgentTaskEntity) { task.AgentName = " " },
			wantError: "agent_name is required"},
		{name: "missing prompt", mutate: func(task *AgentTaskEntity) { task.Prompt = " " },
			wantError: "prompt is required"},
		{name: "invalid depth", mutate: func(task *AgentTaskEntity) { task.Depth = 0 },
			wantError: "depth must be positive"},
		{name: "invalid policy", mutate: func(task *AgentTaskEntity) { task.PolicyJSON = "{" },
			wantError: "policy_json must be valid JSON"},
		{name: "invalid usage", mutate: func(task *AgentTaskEntity) { task.UsageJSON = "{" },
			wantError: "usage_json must be valid JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			entity := validAgentTaskEntity(entityID, now)
			if test.mutate != nil {
				test.mutate(&entity)
			}

			err := validateAgentTaskEntity(&entity)
			if test.wantError == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestTaskRowDecodingBehavior(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	entityID := mustTestUUIDv7(t)
	valid := validTaskRow(entityID, now)

	tests := []struct {
		mutate func(*taskRow)
		name   string
	}{
		{mutate: nil, name: validCase},
		{mutate: func(row *taskRow) { row.CreatedAt = invalidValue }, name: "invalid created"},
		{mutate: func(row *taskRow) { row.UpdatedAt = invalidValue }, name: "invalid updated"},
		{mutate: func(row *taskRow) { value := invalidValue; row.StartedAt = &value }, name: "invalid started"},
		{mutate: func(row *taskRow) { value := invalidValue; row.FinishedAt = &value }, name: "invalid finished"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			row := valid
			if test.mutate != nil {
				test.mutate(&row)
			}

			entity, err := taskFromRow(&row)
			if test.mutate == nil {
				require.NoError(t, err)
				assert.Equal(t, entityID, entity.ID)

				return
			}

			require.Error(t, err)
			assert.Nil(t, entity)
		})
	}

	eventRow := taskEventRow{TaskID: entityID, ID: entityID, Kind: "progress", PayloadJSON: `{}`,
		CreatedAt: now, Sequence: 2}
	event, err := taskEventFromRow(&eventRow)
	require.NoError(t, err)
	assert.Equal(t, int64(2), event.Sequence)
	invalidEventRow := taskEventRow{TaskID: "", ID: "", Kind: "", PayloadJSON: "", CreatedAt: invalidValue, Sequence: 0}
	event, err = taskEventFromRow(&invalidEventRow)
	require.Error(t, err)
	assert.Nil(t, event)
}

func TestTaskEventInsertionRejectsInvalidEvent(t *testing.T) {
	t.Parallel()
	_, err := insertTaskEvent(t.Context(), nil, "task", 1, " ", `{}`, time.Now())
	require.ErrorContains(t, err, "event.kind is required")
}

func TestAgentTaskRowDecodingBehavior(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	entityID := mustTestUUIDv7(t)
	row := validAgentTaskRow(entityID, now)
	entity, err := agentTaskFromRow(&row)
	require.NoError(t, err)
	assert.Equal(t, "general", entity.AgentName)

	row.CreatedAt = invalidValue
	entity, err = agentTaskFromRow(&row)
	require.Error(t, err)
	assert.Nil(t, entity)
}

func validSessionEntity(entityID string, now time.Time) SessionEntity {
	return SessionEntity{CreatedAt: now, UpdatedAt: now, ID: entityID, CWD: "/work", Name: "", ParentSession: ""}
}

func validEntryEntity(entityID string, now time.Time) EntryEntity {
	return EntryEntity{CreatedAt: now, ParentID: nil, Message: MessageEntity{Timestamp: time.Time{}, Role: "",
		Content: "", Provider: "", Model: ""}, Summary: "", ToolStatus: "",
		Type: EntryTypeMessage, CustomType: "", DataJSON: `{}`, ID: entityID, ToolName: "", SessionID: entityID,
		ToolArgsJSON: "", BranchFromEntryID: "", CompactionFirstKeptEntryID: "", CompactionTokensBefore: 0,
		TokenEstimate: 0, Display: false, ModelFacing: false}
}

func validMessageEntity(entityID string, now time.Time) SessionMessageEntity {
	return SessionMessageEntity{CreatedAt: now, ID: entityID, SessionID: entityID, EntryID: entityID,
		Sender: string(RoleUser), Role: RoleUser, Content: "", Provider: "", Model: ""}
}

func validTaskEntity(entityID string, now time.Time) TaskEntity {
	return TaskEntity{CreatedAt: now, StartedAt: nil, FinishedAt: nil, UpdatedAt: now, LeaseExpiresAt: nil,
		ID: entityID, Kind: TaskKindAgent, ParentTaskID: "", OwnerSessionID: entityID, ConcurrencyKey: "",
		LeaseOwner: "", State: TaskQueued, Result: "", ErrorCode: "", ErrorMessage: ""}
}

func validAgentTaskEntity(entityID string, now time.Time) AgentTaskEntity {
	return AgentTaskEntity{Task: validTaskEntity(entityID, now), ChildSessionID: entityID, AgentName: "general",
		Prompt: "work", Model: "", Provider: "", PolicyJSON: `{}`, UsageJSON: `{}`, Depth: 1}
}

func validTaskRow(entityID, now string) taskRow {
	return taskRow{StartedAt: nil, LeaseExpiresAt: nil, ParentTaskID: nil, LeaseOwner: nil, FinishedAt: nil,
		Result: "", ID: entityID, ErrorCode: "", ErrorMessage: "", CreatedAt: now, State: string(TaskQueued),
		ConcurrencyKey: "", UpdatedAt: now, OwnerSessionID: entityID, Kind: TaskKindAgent}
}

func validAgentTaskRow(entityID, now string) agentTaskRow {
	return agentTaskRow{ID: entityID, Kind: TaskKindAgent, ParentTaskID: nil, OwnerSessionID: entityID,
		ConcurrencyKey: "", State: string(TaskQueued), Result: "", ErrorCode: "", ErrorMessage: "",
		CreatedAt: now, StartedAt: nil, FinishedAt: nil, UpdatedAt: now, LeaseOwner: nil, LeaseExpiresAt: nil,
		ChildSessionID: entityID, AgentName: "general", Prompt: "work", Model: "", Provider: "",
		PolicyJSON: `{}`, UsageJSON: `{}`, Depth: 1}
}

func mustTestUUIDv7(t *testing.T) string {
	t.Helper()

	entityID, err := uuid.NewV7()
	require.NoError(t, err)

	return entityID.String()
}
