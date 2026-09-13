package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/database"
	_ "modernc.org/sqlite" // Register the production SQLite driver for the baseline harness.
)

const (
	compactTimestampArtifactPathEnv = "LIBRECODE_COMPACT_TIMESTAMP_BASELINE_PATH"
	compactTimestampRowsEnv         = "LIBRECODE_DATABASE_BENCH_ROWS"
	compactTimestampFixtureRows     = 1_000_000
	compactTimestampFixtureBatch    = 10_000
	compactTimestampSessionID       = "01900000-0000-7000-8000-000000000001"
	fixtureStartTimestamp           = "2025-01-01T00:00:00Z"
)

type baselineQueryPlanSpec struct {
	Name string
	SQL  string
	Args []any
}

type baselinePageUsage struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes"`
	Pages        int64  `json:"pages"`
	PayloadBytes int64  `json:"payload_bytes"`
	UnusedBytes  int64  `json:"unused_bytes"`
}

type baselineStorage struct {
	Objects           []baselinePageUsage `json:"objects"`
	FileBytes         int64               `json:"file_bytes"`
	PageSize          int64               `json:"page_size"`
	PageCount         int64               `json:"page_count"`
	FreeListPages     int64               `json:"free_list_pages"`
	ActiveDBStatPages int64               `json:"active_dbstat_pages"`
	HeapAllocBytes    uint64              `json:"process_heap_alloc_bytes"`
	RSSBytes          uint64              `json:"process_rss_bytes"`
}

type baselineCollisionStats struct {
	EqualTimestampGroups int64 `json:"equal_timestamp_groups"`
	RowsInEqualGroups    int64 `json:"rows_in_equal_groups"`
	MaxGroupLength       int64 `json:"max_group_length"`
}

type baselineIndex struct {
	Name      string `json:"name"`
	TableName string `json:"table_name"`
	SQL       string `json:"sql"`
}

type compactTimestampBaselineReport struct {
	QueryPlans       map[string][]string          `json:"query_plans"`
	Indexes          []baselineIndex              `json:"indexes"`
	TimestampIndexes []string                     `json:"timestamp_indexes"`
	Layouts          []compactTimestampLayoutPair `json:"amd64_layouts,omitempty"`
	GeneratedAtUTC   string                       `json:"generated_at_utc"`
	GOARCH           string                       `json:"goarch"`
	GoVersion        string                       `json:"go_version"`
	SessionID        string                       `json:"session_id"`
	TimestampStorage string                       `json:"timestamp_storage"`
	BeforeVacuum     baselineStorage              `json:"before_vacuum"`
	AfterVacuum      baselineStorage              `json:"after_vacuum"`
	Collisions       baselineCollisionStats       `json:"same_second_collisions"`
	FixtureRows      int                          `json:"fixture_rows"`
}

type baselineMeasurements struct {
	Plans            map[string][]string
	Indexes          []baselineIndex
	TimestampIndexes []string
	Storage          baselineStorage
	Collisions       baselineCollisionStats
}

// TestCompactTimestampBaselineContracts cheaply freezes the schema and query-plan
// inputs used by the expensive artifact generator. It never creates the million-row fixture.
func TestCompactTimestampBaselineContracts(t *testing.T) {
	t.Parallel()

	connection := openCompactTimestampDatabase(t, ":memory:")
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close baseline schema: %v", err)
		}
	})

	gotIndexes, err := timestampIndexInventory(t.Context(), connection)
	if err != nil {
		t.Fatalf("inventory timestamp indexes: %v", err)
	}

	wantIndexes := expectedTimestampIndexes()
	if !slices.Equal(gotIndexes, wantIndexes) {
		t.Fatalf("timestamp index inventory changed\n got: %v\nwant: %v", gotIndexes, wantIndexes)
	}

	plans, err := collectBaselineQueryPlans(t.Context(), connection)
	if err != nil {
		t.Fatalf("collect query plans: %v", err)
	}

	for _, name := range expectedBaselinePlanNames() {
		if len(plans[name]) == 0 {
			t.Errorf("query plan %q was not collected", name)
		}
	}

	usage, err := collectDBStat(t.Context(), connection)
	if err != nil {
		t.Fatalf("collect dbstat: %v", err)
	}

	if len(usage) == 0 {
		t.Fatal("dbstat returned no schema objects")
	}

	assertProductionTranscriptIndexes(t, connection)
}

// TestGenerateCompactTimestampBaseline is an explicit, provider-free artifact
// generator. Set LIBRECODE_COMPACT_TIMESTAMP_BASELINE_PATH to the desired .db
// path. It creates the database and a sibling .json report. The row count is one
// million by default; LIBRECODE_DATABASE_BENCH_ROWS exists only for smoke runs.
func TestGenerateCompactTimestampBaseline(t *testing.T) {
	t.Parallel()

	path := cleanArtifactPath(t)
	fixtureRows := compactTimestampRowCount(t)
	connection := generateCompactTimestampFixture(t, path, fixtureRows)

	defer func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close generated baseline: %v", err)
		}
	}()

	before := collectBaselineMeasurements(t, connection, path)

	_, vacuumErr := connection.ExecContext(t.Context(), "VACUUM")
	if vacuumErr != nil {
		t.Fatalf("vacuum baseline fixture: %v", vacuumErr)
	}

	after, err := measureBaselineStorage(t.Context(), connection, path)
	if err != nil {
		t.Fatalf("measure storage after VACUUM: %v", err)
	}

	report := compactTimestampBaselineReport{
		QueryPlans:       before.Plans,
		GeneratedAtUTC:   time.Now().UTC().Format(time.RFC3339),
		GOARCH:           runtime.GOARCH,
		GoVersion:        runtime.Version(),
		SessionID:        compactTimestampSessionID,
		TimestampStorage: "RFC3339Nano TEXT",
		BeforeVacuum:     before.Storage,
		AfterVacuum:      after,
		Indexes:          before.Indexes,
		TimestampIndexes: before.TimestampIndexes,
		Layouts:          compactTimestampLayouts(),
		Collisions:       before.Collisions,
		FixtureRows:      fixtureRows,
	}

	writeBaselineReport(t, path, &report)
	logBaselineReport(t, path, &report)
}

func cleanArtifactPath(tb testing.TB) string {
	tb.Helper()

	path := strings.TrimSpace(os.Getenv(compactTimestampArtifactPathEnv))
	if path == "" {
		tb.Skip("set " + compactTimestampArtifactPathEnv + " to generate the expensive baseline fixture")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		tb.Fatalf("resolve baseline artifact path: %v", err)
	}

	return absolutePath
}

func collectBaselineMeasurements(tb testing.TB, connection *sql.DB, path string) baselineMeasurements {
	tb.Helper()

	storage, err := measureBaselineStorage(tb.Context(), connection, path)
	if err != nil {
		tb.Fatalf("measure storage before VACUUM: %v", err)
	}

	collisions, err := measureSameSecondCollisions(tb.Context(), connection)
	if err != nil {
		tb.Fatalf("measure timestamp collisions: %v", err)
	}

	plans, err := collectBaselineQueryPlans(tb.Context(), connection)
	if err != nil {
		tb.Fatalf("collect query plans: %v", err)
	}

	indexes, err := indexInventory(tb.Context(), connection)
	if err != nil {
		tb.Fatalf("inventory indexes: %v", err)
	}

	timestampIndexes, err := timestampIndexInventory(tb.Context(), connection)
	if err != nil {
		tb.Fatalf("inventory timestamp indexes: %v", err)
	}

	return baselineMeasurements{
		Plans:            plans,
		Indexes:          indexes,
		TimestampIndexes: timestampIndexes,
		Storage:          storage,
		Collisions:       collisions,
	}
}

func writeBaselineReport(tb testing.TB, path string, report *compactTimestampBaselineReport) {
	tb.Helper()

	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		tb.Fatalf("encode baseline report: %v", err)
	}

	reportPath := filepath.Clean(path + ".json")
	if err := os.WriteFile(reportPath, append(reportBytes, '\n'), 0o600); err != nil {
		tb.Fatalf("write baseline report: %v", err)
	}
}

func logBaselineReport(tb testing.TB, path string, report *compactTimestampBaselineReport) {
	tb.Helper()

	tb.Logf("generated %d rows: database=%s report=%s", report.FixtureRows, path, path+".json")
	tb.Logf(
		"file bytes before=%d after=%d; active pages before=%d after=%d",
		report.BeforeVacuum.FileBytes,
		report.AfterVacuum.FileBytes,
		report.BeforeVacuum.ActiveDBStatPages,
		report.AfterVacuum.ActiveDBStatPages,
	)
	tb.Logf(
		"same-second groups=%d rows-in-groups=%d max-group=%d",
		report.Collisions.EqualTimestampGroups,
		report.Collisions.RowsInEqualGroups,
		report.Collisions.MaxGroupLength,
	)
}

func compactTimestampRowCount(tb testing.TB) int {
	tb.Helper()

	value := strings.TrimSpace(os.Getenv(compactTimestampRowsEnv))
	if value == "" {
		return compactTimestampFixtureRows
	}

	fixtureRows, err := strconv.Atoi(value)
	if err != nil || fixtureRows <= 0 {
		tb.Fatalf("%s must be a positive integer, got %q", compactTimestampRowsEnv, value)
	}

	return fixtureRows
}

func openCompactTimestampDatabase(tb testing.TB, path string) *sql.DB {
	tb.Helper()

	connection, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatalf("open baseline SQLite database: %v", err)
	}

	connection.SetMaxOpenConns(1)

	if err := database.Migrate(context.Background(), connection); err != nil {
		closeDatabaseAfterFailure(tb, connection)
		tb.Fatalf("migrate baseline SQLite database: %v", err)
	}

	return connection
}

func generateCompactTimestampFixture(tb testing.TB, path string, rowCount int) *sql.DB {
	tb.Helper()

	cleanPath, err := filepath.Abs(path)
	if err != nil {
		tb.Fatalf("resolve fixture path: %v", err)
	}

	prepareCompactTimestampFixturePath(tb, cleanPath)

	connection := openCompactTimestampDatabase(tb, cleanPath)
	applyCompactTimestampFixtureSettings(tb, connection)
	insertCompactTimestampFixtureSession(tb, connection)

	for start := 0; start < rowCount; start += compactTimestampFixtureBatch {
		end := min(start+compactTimestampFixtureBatch, rowCount)
		insertCompactTimestampFixtureBatch(tb, connection, start, end)
	}

	_, analyzeErr := connection.ExecContext(context.Background(), "ANALYZE")
	if analyzeErr != nil {
		closeDatabaseAfterFailure(tb, connection)
		tb.Fatalf("analyze fixture: %v", analyzeErr)
	}

	return connection
}

func prepareCompactTimestampFixturePath(tb testing.TB, path string) {
	tb.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		tb.Fatalf("create fixture directory: %v", err)
	}

	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			tb.Fatalf("remove old fixture %s: %v", candidate, err)
		}
	}
}

func applyCompactTimestampFixtureSettings(tb testing.TB, connection *sql.DB) {
	tb.Helper()

	pragmas := []string{
		"PRAGMA journal_mode=DELETE",
		"PRAGMA synchronous=OFF",
		"PRAGMA foreign_keys=OFF",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA cache_size=-65536",
	}

	for _, pragma := range pragmas {
		if _, err := connection.ExecContext(context.Background(), pragma); err != nil {
			closeDatabaseAfterFailure(tb, connection)
			tb.Fatalf("apply fixture setting %q: %v", pragma, err)
		}
	}
}

func insertCompactTimestampFixtureSession(tb testing.TB, connection *sql.DB) {
	tb.Helper()

	const insertSession = `INSERT INTO sessions
(id,cwd,name,parent_session_id,created_at,updated_at) VALUES(?,?,?,NULL,?,?)`

	_, err := connection.ExecContext(
		context.Background(),
		insertSession,
		compactTimestampSessionID,
		"/fixture",
		"compact timestamp baseline",
		fixtureStartTimestamp,
		"2025-01-02T00:00:00Z",
	)
	if err != nil {
		closeDatabaseAfterFailure(tb, connection)
		tb.Fatalf("insert fixture session: %v", err)
	}
}

func closeDatabaseAfterFailure(tb testing.TB, connection *sql.DB) {
	tb.Helper()

	if err := connection.Close(); err != nil {
		tb.Errorf("close database after fixture failure: %v", err)
	}
}

func insertCompactTimestampFixtureBatch(tb testing.TB, connection *sql.DB, start, end int) {
	tb.Helper()

	transaction, err := connection.BeginTx(context.Background(), nil)
	if err != nil {
		tb.Fatalf("begin fixture batch %d:%d: %v", start, end, err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}

		rollbackErr := transaction.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			tb.Errorf("roll back fixture batch %d:%d: %v", start, end, rollbackErr)
		}
	}()

	insertCompactTimestampEntries(tb, transaction, start, end)
	insertCompactTimestampMessages(tb, transaction, start, end)

	if err := transaction.Commit(); err != nil {
		tb.Fatalf("commit fixture batch %d:%d: %v", start, end, err)
	}

	committed = true
}

func insertCompactTimestampEntries(tb testing.TB, transaction *sql.Tx, start, end int) {
	tb.Helper()

	const insertEntries = `WITH digits(n) AS (VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)),
seq(n) AS (
 SELECT ? + a.n + 10*b.n + 100*c.n + 1000*d.n
 FROM digits a CROSS JOIN digits b CROSS JOIN digits c CROSS JOIN digits d
)
INSERT INTO session_entries (
 id,session_id,parent_id,entry_type,custom_type,data_json,summary,created_at,
 tool_name,tool_status,tool_args_json,token_estimate,model_facing,display,
 compaction_first_kept_entry_id,compaction_tokens_before,branch_from_entry_id,operation_id)
SELECT printf('01910000-0000-7000-8000-%012x',n+1), ?, NULL, 'message', '', '{}', '',
 strftime('%Y-%m-%dT%H:%M:%S','2025-01-01T00:00:00Z',printf('+%.1f seconds',n/10.0)) ||
 '.'||printf('%09d',(n%10)*100000000)||'Z',
 '', '', '', 32, 1, 1, '', 0, '', '' FROM seq WHERE n < ?`

	_, err := transaction.ExecContext(
		context.Background(),
		insertEntries,
		start,
		compactTimestampSessionID,
		end,
	)
	if err != nil {
		tb.Fatalf("insert fixture entries %d:%d: %v", start, end, err)
	}
}

func insertCompactTimestampMessages(tb testing.TB, transaction *sql.Tx, start, end int) {
	tb.Helper()

	firstID := fmt.Sprintf("01910000-0000-7000-8000-%012x", start+1)
	lastID := fmt.Sprintf("01910000-0000-7000-8000-%012x", end)

	const insertMessages = `INSERT INTO session_messages(entry_id,role,provider,model)
SELECT id,CASE WHEN substr(id,-1,1) IN ('0','2','4','6','8','a','c','e') THEN 'user' ELSE 'assistant' END,
CASE WHEN substr(id,-1,1) IN ('0','2','4','6','8','a','c','e') THEN '' ELSE 'fixture' END,
CASE WHEN substr(id,-1,1) IN ('0','2','4','6','8','a','c','e') THEN '' ELSE 'fixture-model' END
FROM session_entries WHERE id BETWEEN ? AND ? ORDER BY id`

	if _, err := transaction.ExecContext(context.Background(), insertMessages, firstID, lastID); err != nil {
		tb.Fatalf("insert fixture messages %d:%d: %v", start, end, err)
	}

	const insertParts = `INSERT INTO session_message_parts
(entry_id,sequence,type,text,mime_type,name,width,height,data)
SELECT id,0,'text','deterministic provider-free transcript payload for entry ' || id ||
 ' used to measure normal repository hydration allocations','','',0,0,NULL
FROM session_entries WHERE id BETWEEN ? AND ? ORDER BY id`

	if _, err := transaction.ExecContext(context.Background(), insertParts, firstID, lastID); err != nil {
		tb.Fatalf("insert fixture message parts %d:%d: %v", start, end, err)
	}
}

func measureBaselineStorage(ctx context.Context, connection *sql.DB, path string) (baselineStorage, error) {
	objects, err := collectDBStat(ctx, connection)
	if err != nil {
		return baselineStorage{}, err
	}

	pageSize, err := queryPragmaInt64(ctx, connection, "PRAGMA page_size")
	if err != nil {
		return baselineStorage{}, err
	}

	pageCount, err := queryPragmaInt64(ctx, connection, "PRAGMA page_count")
	if err != nil {
		return baselineStorage{}, err
	}

	freeList, err := queryPragmaInt64(ctx, connection, "PRAGMA freelist_count")
	if err != nil {
		return baselineStorage{}, err
	}

	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		return baselineStorage{}, fmt.Errorf("stat database: %w", err)
	}

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	activePages := int64(0)
	for _, object := range objects {
		activePages += object.Pages
	}

	return baselineStorage{
		Objects:           objects,
		FileBytes:         info.Size(),
		PageSize:          pageSize,
		PageCount:         pageCount,
		FreeListPages:     freeList,
		ActiveDBStatPages: activePages,
		HeapAllocBytes:    memory.HeapAlloc,
		RSSBytes:          processRSSBytes(),
	}, nil
}

func queryPragmaInt64(ctx context.Context, connection *sql.DB, query string) (int64, error) {
	var value int64

	if err := connection.QueryRowContext(ctx, query).Scan(&value); err != nil {
		return 0, fmt.Errorf("query %s: %w", query, err)
	}

	return value, nil
}

func collectDBStat(ctx context.Context, connection *sql.DB) (_ []baselinePageUsage, returnErr error) {
	const query = `SELECT d.name,COALESCE(s.type,CASE WHEN d.name='sqlite_schema' THEN 'table' ELSE 'internal' END),
 sum(d.pgsize),count(*),sum(d.payload),sum(d.unused)
FROM dbstat AS d LEFT JOIN sqlite_schema AS s ON s.name=d.name
GROUP BY d.name,COALESCE(s.type,CASE WHEN d.name='sqlite_schema' THEN 'table' ELSE 'internal' END)
ORDER BY d.name`

	rows, err := connection.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query SQLite dbstat (driver must include SQLITE_ENABLE_DBSTAT_VTAB): %w", err)
	}

	defer func() { returnErr = closeRows(rows, "close dbstat rows", returnErr) }()

	usage := make([]baselinePageUsage, 0)

	for rows.Next() {
		var item baselinePageUsage

		err := rows.Scan(
			&item.Name,
			&item.Kind,
			&item.Bytes,
			&item.Pages,
			&item.PayloadBytes,
			&item.UnusedBytes,
		)
		if err != nil {
			return nil, fmt.Errorf("scan dbstat: %w", err)
		}

		usage = append(usage, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dbstat: %w", err)
	}

	return usage, nil
}

func measureSameSecondCollisions(
	ctx context.Context,
	connection *sql.DB,
) (baselineCollisionStats, error) {
	const query = `WITH second_groups AS (
 SELECT substr(created_at,1,19) AS unix_second,count(*) AS group_length
 FROM session_entries GROUP BY substr(created_at,1,19)
)
SELECT COALESCE(sum(CASE WHEN group_length>1 THEN 1 ELSE 0 END),0),
 COALESCE(sum(CASE WHEN group_length>1 THEN group_length ELSE 0 END),0),
 COALESCE(max(group_length),0) FROM second_groups`

	var result baselineCollisionStats

	err := connection.QueryRowContext(ctx, query).Scan(
		&result.EqualTimestampGroups,
		&result.RowsInEqualGroups,
		&result.MaxGroupLength,
	)
	if err != nil {
		return baselineCollisionStats{}, fmt.Errorf("query same-second collisions: %w", err)
	}

	return result, nil
}

func collectBaselineQueryPlans(ctx context.Context, connection *sql.DB) (map[string][]string, error) {
	specifications := append(baselineQueryPlans(), executionBaselineQueryPlans()...)
	plans := make(map[string][]string, len(specifications))

	for _, specification := range specifications {
		plan, err := collectBaselineQueryPlan(ctx, connection, specification)
		if err != nil {
			return nil, err
		}

		plans[specification.Name] = plan
	}

	return plans, nil
}

func collectBaselineQueryPlan(
	ctx context.Context,
	connection *sql.DB,
	specification baselineQueryPlanSpec,
) (_ []string, returnErr error) {
	rows, err := connection.QueryContext(
		ctx,
		"EXPLAIN QUERY PLAN "+specification.SQL,
		specification.Args...,
	)
	if err != nil {
		return nil, fmt.Errorf("explain %s: %w", specification.Name, err)
	}

	defer func() { returnErr = closeRows(rows, "close explain "+specification.Name, returnErr) }()

	plan := make([]string, 0)

	for rows.Next() {
		var (
			selectID int
			parentID int
			unusedID int
			detail   string
		)

		if err := rows.Scan(&selectID, &parentID, &unusedID, &detail); err != nil {
			return nil, fmt.Errorf("scan explain %s: %w", specification.Name, err)
		}

		plan = append(plan, fmt.Sprintf("%d|%d|%d|%s", selectID, parentID, unusedID, detail))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate explain %s: %w", specification.Name, err)
	}

	return plan, nil
}

func indexInventory(ctx context.Context, connection *sql.DB) (_ []baselineIndex, returnErr error) {
	const query = `SELECT name,tbl_name,COALESCE(sql,'')
FROM sqlite_schema WHERE type='index' ORDER BY name`

	rows, err := connection.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query index inventory: %w", err)
	}

	defer func() { returnErr = closeRows(rows, "close index inventory", returnErr) }()

	indexes := make([]baselineIndex, 0)

	for rows.Next() {
		var index baselineIndex

		if err := rows.Scan(&index.Name, &index.TableName, &index.SQL); err != nil {
			return nil, fmt.Errorf("scan index inventory: %w", err)
		}

		indexes = append(indexes, index)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate index inventory: %w", err)
	}

	return indexes, nil
}

func timestampIndexInventory(ctx context.Context, connection *sql.DB) (_ []string, returnErr error) {
	const query = `SELECT name FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL AND (
 lower(sql) LIKE '%created_at%' OR lower(sql) LIKE '%updated_at%' OR
 lower(sql) LIKE '%finished_at%' OR lower(sql) LIKE '%lease_expires_at%' OR
 lower(sql) LIKE '%consumed_at%') ORDER BY name`

	rows, err := connection.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query timestamp index inventory: %w", err)
	}

	defer func() { returnErr = closeRows(rows, "close timestamp index inventory", returnErr) }()

	indexes := make([]string, 0)

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan timestamp index inventory: %w", err)
		}

		indexes = append(indexes, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate timestamp index inventory: %w", err)
	}

	return indexes, nil
}

func closeRows(rows *sql.Rows, message string, returnErr error) error {
	closeErr := rows.Close()
	if closeErr != nil {
		return errors.Join(returnErr, fmt.Errorf("%s: %w", message, closeErr))
	}

	return returnErr
}

func assertProductionTranscriptIndexes(t *testing.T, connection *sql.DB) {
	t.Helper()

	for name, wantColumns := range expectedTranscriptIndexes() {
		columns, err := indexColumns(t.Context(), connection, name)
		if err != nil {
			t.Fatalf("inspect transcript index %s: %v", name, err)
		}

		if got := strings.Join(columns, ","); got != wantColumns {
			t.Errorf("transcript index %s columns = %q, want %q", name, got, wantColumns)
		}
	}

	const cursorIndexSQL = `SELECT sql FROM sqlite_schema
WHERE type='index' AND name='idx_session_entries_transcript_cursor'`

	var cursorSQL string

	if err := connection.QueryRowContext(t.Context(), cursorIndexSQL).Scan(&cursorSQL); err != nil {
		t.Fatalf("read transcript cursor index SQL: %v", err)
	}

	if !strings.Contains(strings.ToLower(cursorSQL), "where display=1") {
		t.Errorf("transcript cursor index is not the production partial index: %s", cursorSQL)
	}
}

func indexColumns(ctx context.Context, connection *sql.DB, name string) (_ []string, returnErr error) {
	rows, err := connection.QueryContext(
		ctx,
		`SELECT name FROM pragma_index_info(?) ORDER BY seqno`,
		name,
	)
	if err != nil {
		return nil, fmt.Errorf("query index columns: %w", err)
	}

	defer func() { returnErr = closeRows(rows, "close index columns", returnErr) }()

	columns := make([]string, 0)

	for rows.Next() {
		var column string

		if err := rows.Scan(&column); err != nil {
			return nil, fmt.Errorf("scan index columns: %w", err)
		}

		columns = append(columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate index columns: %w", err)
	}

	return columns, nil
}

func processRSSBytes() uint64 {
	contents, err := os.ReadFile("/proc/self/statm")
	if err == nil {
		fields := strings.Fields(string(contents))
		if len(fields) >= 2 {
			residentPages, parseErr := strconv.ParseUint(fields[1], 10, 64)
			pageSize := os.Getpagesize()

			if parseErr == nil && pageSize >= 0 {
				return residentPages * uint64(pageSize)
			}
		}
	}

	var usage syscall.Rusage

	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil || usage.Maxrss < 0 {
		return 0
	}

	if runtime.GOOS == "darwin" {
		return uint64(usage.Maxrss)
	}

	if usage.Maxrss > math.MaxInt64/1024 {
		return math.MaxUint64
	}

	return uint64(usage.Maxrss * 1024)
}

func expectedTimestampIndexes() []string {
	return []string{
		"idx_events_completion_repair",
		"idx_session_completion_global_pending",
		"idx_session_entries_model_facing",
		"idx_session_entries_session_created_id",
		"idx_session_entries_tool_name",
		"idx_session_entries_transcript_cursor",
		"idx_sessions_cwd_parent_updated",
		"idx_sessions_parent_updated",
		"idx_tasks_completion_repair",
		"idx_tasks_kind_state_created",
		"idx_tasks_owner_state_updated",
		"idx_tasks_owner_updated",
		"idx_tasks_parent_updated",
		"idx_tasks_recoverable_leases",
		"idx_tasks_state_created",
	}
}

func expectedBaselinePlanNames() []string {
	return []string{
		"newest_session",
		"transcript_tail",
		"transcript_cursor_pagination",
		"current_leaf",
		"queued_task_claim",
		"lease_recovery",
		"completion_repair",
	}
}

func expectedTranscriptIndexes() map[string]string {
	return map[string]string{
		"idx_session_entries_session_created_id": "session_id,created_at,id",
		"idx_session_entries_transcript_cursor":  "session_id,created_at,id",
	}
}

func baselineQueryPlans() []baselineQueryPlanSpec {
	plans := baselineSessionQueryPlans()

	return append(plans, baselineTaskQueryPlans()...)
}

func baselineSessionQueryPlans() []baselineQueryPlanSpec {
	return []baselineQueryPlanSpec{
		{
			Name: "newest_session",
			SQL: `SELECT id, cwd, name, parent_session_id, created_at, updated_at
FROM sessions
WHERE cwd = ? AND parent_session_id IS NULL
ORDER BY updated_at DESC, id DESC
LIMIT 1`,
			Args: []any{"/fixture"},
		},
		{
			Name: "transcript_tail",
			SQL: `SELECT entry_id, session_id, custom_type, role, provider, model, created_at FROM (
SELECT e.id AS entry_id, e.session_id, e.custom_type, m.role, m.provider, m.model, e.created_at
FROM session_entries AS e INDEXED BY idx_session_entries_transcript_cursor
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 ORDER BY e.created_at DESC, e.id DESC LIMIT ?)
ORDER BY created_at ASC, entry_id ASC`,
			Args: []any{compactTimestampSessionID, 256},
		},
		{
			Name: "transcript_cursor_pagination",
			SQL: `SELECT entry_id, session_id, custom_type, role, provider, model, created_at FROM (
SELECT e.id AS entry_id, e.session_id, e.custom_type, m.role, m.provider, m.model, e.created_at
FROM session_entries AS e INDEXED BY idx_session_entries_transcript_cursor
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 AND (e.created_at < ? OR (e.created_at = ? AND e.id < ?))
ORDER BY e.created_at DESC, e.id DESC LIMIT ?) ORDER BY created_at ASC, entry_id ASC`,
			Args: []any{
				compactTimestampSessionID,
				"2025-01-01T01:00:00.000Z",
				"2025-01-01T01:00:00.000Z",
				"01910000-0000-7000-8000-000000008ca0",
				256,
			},
		},
	}
}

func executionBaselineQueryPlans() []baselineQueryPlanSpec {
	return []baselineQueryPlanSpec{
		{
			Name: "current_leaf",
			SQL: `SELECT id, session_id, parent_id, entry_type, custom_type, data_json, summary, created_at,
tool_name, tool_status, tool_args_json, token_estimate, model_facing, display,
compaction_first_kept_entry_id, compaction_tokens_before, branch_from_entry_id
FROM session_entries
WHERE session_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 1`,
			Args: []any{compactTimestampSessionID},
		},
	}
}

func baselineTaskQueryPlans() []baselineQueryPlanSpec {
	return []baselineQueryPlanSpec{
		{
			Name: "queued_task_claim",
			SQL: `UPDATE tasks SET state = ?, started_at = COALESCE(started_at, ?), finished_at = NULL,
updated_at = ?, lease_owner = ?, lease_expires_at = ?, result = '', error_code = '', error_message = ''
WHERE id = ? AND state = ?`,
			Args: []any{
				database.TaskRunning,
				fixtureStartTimestamp,
				fixtureStartTimestamp,
				"worker",
				"2025-01-01T00:01:00Z",
				"01920000-0000-7000-8000-000000000001",
				database.TaskQueued,
			},
		},
		{
			Name: "lease_recovery",
			SQL: `SELECT id FROM tasks WHERE kind = ? AND state IN (?, ?)
AND (lease_expires_at IS NULL OR lease_expires_at <= ?) ORDER BY created_at, id LIMIT ?`,
			Args: []any{
				database.TaskKindAgent,
				database.TaskRunning,
				database.TaskCanceling,
				fixtureStartTimestamp,
				100,
			},
		},
		{
			Name: "completion_repair",
			SQL: `SELECT e.id AS event_id,t.id AS task_id,e.kind AS event_kind,t.kind AS task_kind,
 t.owner_session_id AS owner,t.state,t.result,t.error_code,t.error_message,e.created_at
FROM tasks t INDEXED BY idx_tasks_completion_repair
JOIN task_events te ON te.task_id=t.id
 AND te.event_id=(SELECT candidate.event_id FROM task_events candidate
  JOIN events candidate_event ON candidate_event.id=candidate.event_id
  WHERE candidate.task_id=t.id
   AND candidate_event.kind=CASE WHEN t.kind='agent' THEN 'task_'||t.state ELSE 'workflow_'||t.state END
  ORDER BY candidate.sequence DESC LIMIT 1)
JOIN events e ON e.id=te.event_id
WHERE t.kind IN ('agent','workflow')
 AND t.state IN ('succeeded','failed','canceled','interrupted')
 AND (t.kind!='agent' OR NOT EXISTS (SELECT 1 FROM workflow_agent_tasks wa WHERE wa.agent_task_id=t.id))
 AND NOT EXISTS (SELECT 1 FROM session_completion_deliveries d
  WHERE d.owner_session_id=t.owner_session_id AND d.event_id=e.id AND d.mapping_version=?)
ORDER BY t.finished_at,t.id LIMIT ?`,
			Args: []any{database.CompletionMappingV1, 256},
		},
	}
}
