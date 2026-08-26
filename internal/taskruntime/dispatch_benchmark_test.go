package taskruntime_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // Register the SQLite driver used by sql.Open.

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/testutil"
)

const benchmarkTaskKind = "benchmark"

// BenchmarkTaskTableDispatch measures the selected transport boundary: durable
// acceptance through an authoritative task-table claim. Handler execution is
// intentionally excluded so alternative wakeup transports can be compared.
func BenchmarkTaskTableDispatch(b *testing.B) {
	connection := newBenchmarkDatabase(b, "dispatch.db")

	sessions := testutil.SessionRepository(b, connection)

	owner, err := sessions.CreateSession(context.Background(), b.TempDir(), "benchmark", "")
	if err != nil {
		b.Fatal(err)
	}

	tasks := testutil.TaskRepository(b, connection)

	b.ResetTimer()

	for range b.N {
		created, createErr := tasks.Create(context.Background(), &database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
			ID: "", Kind: benchmarkTaskKind, OwnerSessionID: owner.ID, ParentTaskID: "",
			ConcurrencyKey: "", LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		})
		if createErr != nil {
			b.Fatal(createErr)
		}

		changed, claimErr := tasks.ClaimQueued(context.Background(), &database.TaskClaim{
			TaskID: created.ID, LeaseOwner: "benchmark", EventKind: "task_started",
			LeaseExpiresAt: time.Now().Add(time.Minute),
		})
		if claimErr != nil || !changed {
			b.Fatalf("claim: changed=%v err=%v", changed, claimErr)
		}
	}
}

func newBenchmarkDatabase(b *testing.B, name string) *sql.DB {
	b.Helper()

	options := database.SQLiteOptions{BusyTimeout: time.Second}

	connection, err := sql.Open("sqlite", database.SQLiteDSN(filepath.Join(b.TempDir(), name), options))
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		if err := connection.Close(); err != nil {
			b.Error(err)
		}
	})
	connection.SetMaxOpenConns(1)

	if err := database.ConfigureSQLite(context.Background(), connection, options); err != nil {
		b.Fatal(err)
	}

	if err := database.Migrate(context.Background(), connection); err != nil {
		b.Fatal(err)
	}

	return connection
}
