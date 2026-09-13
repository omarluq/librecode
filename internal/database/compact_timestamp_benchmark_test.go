package database_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/database"
)

const (
	retainedHeapElements = 1_000_000
	amd64Architecture    = "amd64"
)

type compactTimestampLayoutPair struct {
	Name         string  `json:"name"`
	CurrentBytes uintptr `json:"current_bytes"`
	CompactBytes uintptr `json:"compact_bytes"`
	SavedBytes   uintptr `json:"saved_bytes"`
}

// BenchmarkSessionRepositoryLargeTranscript scans production repository pages,
// including message-part hydration, against the deterministic indexed fixture.
// Fixture construction is outside the timer. Run with -count=10 for the Phase 0
// sample set.
func BenchmarkSessionRepositoryLargeTranscript(b *testing.B) {
	fixtureRows := compactTimestampRowCount(b)
	path := filepath.Join(b.TempDir(), "compact-timestamp-transcript.db")
	connection := generateCompactTimestampFixture(b, path, fixtureRows)
	b.Cleanup(func() {
		if err := connection.Close(); err != nil {
			b.Errorf("close transcript benchmark fixture: %v", err)
		}
	})

	repositories, err := database.NewRepositories(connection)
	if err != nil {
		b.Fatalf("construct benchmark repositories: %v", err)
	}

	for _, pageSize := range []int{256, 4096} {
		b.Run(fmt.Sprintf("tail_%d", pageSize), func(b *testing.B) {
			benchmarkTranscriptPage(b, func(ctx context.Context) ([]database.SessionMessageEntity, error) {
				return repositories.Sessions.TranscriptMessageTail(ctx, compactTimestampSessionID, pageSize)
			}, pageSize)
		})

		cursorIndex := fixtureRows / 2
		cursor := time.Date(2025, 1, 1, 0, 0, 0, 1, time.UTC).
			Add(time.Duration(cursorIndex) * 100 * time.Millisecond)
		cursorID := fmt.Sprintf("01910000-0000-7000-8000-%012x", cursorIndex+1)

		b.Run(fmt.Sprintf("cursor_%d", pageSize), func(b *testing.B) {
			benchmarkTranscriptPage(b, func(ctx context.Context) ([]database.SessionMessageEntity, error) {
				return repositories.Sessions.TranscriptMessagesBefore(
					ctx, compactTimestampSessionID, cursor, cursorID, pageSize,
				)
			}, pageSize)
		})
	}
}

func benchmarkTranscriptPage(
	b *testing.B,
	load func(context.Context) ([]database.SessionMessageEntity, error),
	pageSize int,
) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	var messageSink []database.SessionMessageEntity

	for b.Loop() {
		messages, err := load(context.Background())
		if err != nil {
			b.Fatalf("scan transcript page: %v", err)
		}

		if len(messages) != pageSize {
			b.Fatalf("transcript page length = %d, want %d", len(messages), pageSize)
		}

		messageSink = messages
	}

	runtime.KeepAlive(messageSink)
	b.ReportMetric(float64(pageSize), "messages/op")
}

// BenchmarkCompactTimestampLayouts reports raw amd64 struct bytes. The compact
// layouts are test-only field-for-field proposals; they do not alter production.
func BenchmarkCompactTimestampLayouts(b *testing.B) {
	if runtime.GOARCH != amd64Architecture {
		b.Skip("raw layout baseline is specified for amd64")
	}

	var layoutSink compactTimestampLayoutPair

	for _, layout := range compactTimestampLayouts() {
		b.Run(layout.Name, func(b *testing.B) {
			for b.Loop() {
				layoutSink = layout
			}

			runtime.KeepAlive(layoutSink)
			b.ReportMetric(float64(layout.CurrentBytes), "current-B/entity")
			b.ReportMetric(float64(layout.CompactBytes), "compact-B/entity")
			b.ReportMetric(float64(layout.SavedBytes), "saved-B/entity")
		})
	}
}

func compactTimestampLayouts() []compactTimestampLayoutPair {
	if runtime.GOARCH != amd64Architecture {
		return nil
	}

	return []compactTimestampLayoutPair{
		newLayoutPair("MessageEntity", reflect.TypeFor[database.MessageEntity](), compactMessageType()),
		newLayoutPair(
			"SessionMessageEntity",
			reflect.TypeFor[database.SessionMessageEntity](),
			compactSessionMessageType(),
		),
		newLayoutPair("SessionEntity", reflect.TypeFor[database.SessionEntity](), compactSessionType()),
		newLayoutPair("EntryEntity", reflect.TypeFor[database.EntryEntity](), compactEntryType()),
		newLayoutPair("TaskEntity", reflect.TypeFor[database.TaskEntity](), compactTaskType()),
	}
}

func newLayoutPair(name string, currentType, compactType reflect.Type) compactTimestampLayoutPair {
	currentBytes := currentType.Size()
	compactBytes := compactType.Size()

	return compactTimestampLayoutPair{
		Name:         name,
		CurrentBytes: currentBytes,
		CompactBytes: compactBytes,
		SavedBytes:   currentBytes - compactBytes,
	}
}

func compactMessageType() reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		layoutField[uint32]("Timestamp"),
		layoutField[database.Role]("Role"),
		layoutField[string]("Content"),
		layoutField[string]("Provider"),
		layoutField[string]("Model"),
		layoutField[[]database.MessagePartEntity]("Parts"),
	})
}

func compactSessionMessageType() reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		layoutField[uint32]("CreatedAt"),
		layoutField[string]("SessionID"),
		layoutField[string]("EntryID"),
		layoutField[string]("Sender"),
		layoutField[database.Role]("Role"),
		layoutField[string]("Content"),
		layoutField[string]("Provider"),
		layoutField[string]("Model"),
		layoutField[[]database.MessagePartEntity]("Parts"),
	})
}

func compactSessionType() reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		layoutField[uint32]("CreatedAt"),
		layoutField[uint32]("UpdatedAt"),
		layoutField[string]("ID"),
		layoutField[string]("CWD"),
		layoutField[string]("Name"),
		layoutField[string]("ParentSession"),
	})
}

func compactEntryType() reflect.Type {
	return reflect.StructOf([]reflect.StructField{
		layoutField[uint32]("CreatedAt"),
		layoutField[*string]("ParentID"),
		layoutField[string]("ToolStatus"),
		layoutField[string]("SessionID"),
		layoutField[string]("ToolArgsJSON"),
		layoutField[string]("CustomType"),
		layoutField[string]("DataJSON"),
		layoutField[string]("ID"),
		layoutField[string]("Summary"),
		layoutField[string]("ToolName"),
		layoutField[database.EntryType]("Type"),
		layoutField[string]("BranchFromEntryID"),
		layoutField[string]("CompactionFirstKeptEntryID"),
		layoutFieldOf("Message", compactMessageType()),
		layoutField[int]("CompactionTokensBefore"),
		layoutField[int]("TokenEstimate"),
		layoutField[bool]("Display"),
		layoutField[bool]("ModelFacing"),
	})
}

func compactTaskType() reflect.Type {
	optionalTimestampType := reflect.StructOf([]reflect.StructField{
		layoutField[uint32]("Value"),
		layoutField[bool]("Valid"),
	})

	return reflect.StructOf([]reflect.StructField{
		layoutField[uint32]("CreatedAt"),
		layoutFieldOf("StartedAt", optionalTimestampType),
		layoutFieldOf("FinishedAt", optionalTimestampType),
		layoutField[uint32]("UpdatedAt"),
		layoutFieldOf("LeaseExpiresAt", optionalTimestampType),
		layoutField[string]("ID"),
		layoutField[string]("Kind"),
		layoutField[string]("ParentTaskID"),
		layoutField[string]("OwnerSessionID"),
		layoutField[string]("ConcurrencyKey"),
		layoutField[string]("LeaseOwner"),
		layoutField[database.TaskState]("State"),
		layoutField[string]("Result"),
		layoutField[string]("ErrorCode"),
		layoutField[string]("ErrorMessage"),
	})
}

func layoutField[T any](name string) reflect.StructField {
	return layoutFieldOf(name, reflect.TypeFor[T]())
}

func layoutFieldOf(name string, fieldType reflect.Type) reflect.StructField {
	return reflect.StructField{Name: name, Type: fieldType}
}

// BenchmarkCompactTimestampRetainedHeap follows Hatchet's million-element
// method: force GC before and after allocation, read retained heap, and keep the
// backing array live with runtime.KeepAlive. Run this benchmark in isolation.
func BenchmarkCompactTimestampRetainedHeap(b *testing.B) {
	if runtime.GOARCH != amd64Architecture {
		b.Skip("retained heap baseline accompanies the amd64 raw layout baseline")
	}

	b.Run("EntryEntity_current", func(b *testing.B) {
		entryType := reflect.TypeFor[database.EntryEntity]()
		measureRetainedHeap(b, retainedHeapElements, entryType.Size(), func() any {
			values := make([]database.EntryEntity, retainedHeapElements)
			stamp := time.Unix(1_735_689_600, 123_000_000).UTC()

			for index := range values {
				values[index].CreatedAt = stamp
				values[index].Message.Timestamp = stamp
			}

			return values
		})
	})

	b.Run("EntryEntity_compact", func(b *testing.B) {
		entryType := compactEntryType()
		measureRetainedHeap(b, retainedHeapElements, entryType.Size(), func() any {
			return reflect.MakeSlice(reflect.SliceOf(entryType), retainedHeapElements, retainedHeapElements).Interface()
		})
	})
}

func measureRetainedHeap(b *testing.B, count int, rawBytes uintptr, allocate func() any) {
	b.Helper()
	b.StopTimer()
	runtime.GC()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	values := allocate()

	runtime.GC()
	runtime.ReadMemStats(&after)

	retained := uint64(0)
	if after.HeapAlloc > before.HeapAlloc {
		retained = after.HeapAlloc - before.HeapAlloc
	}

	b.StartTimer()

	for b.Loop() {
		runtime.KeepAlive(values)
	}

	b.StopTimer()
	runtime.KeepAlive(values)
	b.ReportMetric(float64(rawBytes), "raw-B/entity")
	b.ReportMetric(float64(retained)/float64(count), "retained-B/entity")
	b.ReportMetric(float64(retained)/(1024*1024), "retained-MiB")
}
