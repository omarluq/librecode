package database

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
)

func nilProviderError() error {
	return oops.In("database").Code("nil_sql_provider").Errorf("sql provider is required")
}

func isNilProvider(provider ksql.Provider) bool {
	if provider == nil {
		return true
	}

	value := reflect.ValueOf(provider)
	kind := value.Kind()
	nilable := kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice

	return nilable && value.IsNil()
}

func sameSQLProvider(left, right ksql.Provider) bool {
	if isNilProvider(left) || isNilProvider(right) {
		return false
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)

	if leftValue.Type() != rightValue.Type() || !leftValue.Comparable() || !rightValue.Comparable() {
		return false
	}

	return leftValue.Interface() == rightValue.Interface()
}

// Repositories is a repository graph backed by one shared transaction provider.
type Repositories struct {
	Sessions   *SessionRepository
	Documents  *DocumentRepository
	Tasks      *TaskRepository
	AgentTasks *AgentTaskRepository
	Workflows  *WorkflowRepository
	ToolTasks  *ToolTaskRepository
}

// NewRepositories constructs the complete repository graph for a SQL connection.
func NewRepositories(connection *sql.DB) (*Repositories, error) {
	provider, err := newSQLProviderFromOpenConnection(connection)
	if err != nil {
		return nil, err
	}

	sessions, err := NewSessionRepositoryWithProvider(provider)
	if err != nil {
		return nil, wrapRepositoryConstruction(err, "session")
	}

	documents, err := NewDocumentRepositoryWithProvider(provider)
	if err != nil {
		return nil, wrapRepositoryConstruction(err, "document")
	}

	tasks, err := NewTaskRepositoryWithProvider(provider)
	if err != nil {
		return nil, wrapRepositoryConstruction(err, "task")
	}

	agentTasks, err := NewAgentTaskRepositoryWithProvider(provider, tasks)
	if err != nil {
		return nil, wrapRepositoryConstruction(err, "agent task")
	}

	toolTasks, err := NewToolTaskRepositoryWithProvider(provider, tasks)
	if err != nil {
		return nil, wrapRepositoryConstruction(err, "tool task")
	}

	workflows, err := NewWorkflowRepositoryWithProvider(provider, tasks, agentTasks)
	if err != nil {
		return nil, wrapRepositoryConstruction(err, "workflow")
	}

	return &Repositories{
		Sessions: sessions, Documents: documents, Tasks: tasks,
		AgentTasks: agentTasks, Workflows: workflows, ToolTasks: toolTasks,
	}, nil
}

func wrapRepositoryConstruction(err error, name string) error {
	return oops.In("database").Code("repository_construction").Wrapf(err, "construct %s repository", name)
}

func newSQLProvider(connection *sql.DB) (*transactionProvider, error) {
	if connection == nil {
		return nil, oops.In("database").Code("nil_sql_connection").Errorf("sql connection is required")
	}

	if err := connection.PingContext(context.Background()); err != nil {
		return nil, oops.In("database").Code("ping_sql_connection").Wrapf(err, "ping sql connection")
	}

	return newSQLProviderFromOpenConnection(connection)
}

// newSQLProviderFromOpenConnection adapts a connection already verified by its
// lifecycle owner. Standalone repository constructors use newSQLProvider.
func newSQLProviderFromOpenConnection(connection *sql.DB) (*transactionProvider, error) {
	if connection == nil {
		return nil, oops.In("database").Code("nil_sql_connection").Errorf("sql connection is required")
	}

	provider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		return nil, oops.In("database").Code("sql_provider").Wrapf(err, "create sql provider")
	}

	return &transactionProvider{
		Provider: provider, connection: connection,
		executeAttempt: nil, waitRetry: nil, restoreReadWrite: nil, diagnostic: nil,
	}, nil
}
