//nolint:testpackage // These tests exercise unexported terminal rendering helpers.
package terminal

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
)

func TestRenderParityComposerFrame(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("first line\nsecond line")

	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	app.drawComposerWindow(&layout)

	frame := frameText(app.frame)
	assertFrameContainsAll(t, frame,
		"╭",
		"first line",
		"second line",
		"╰",
	)
	if layout.Composer.Y+layout.Composer.Height != layout.Status.Y {
		t.Fatalf("composer bottom = %d, status y = %d", layout.Composer.Y+layout.Composer.Height, layout.Status.Y)
	}
}

func TestRenderParityStatuslineFrame(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.cwd = "/work/librecode"
	app.sessionID = "session-123"

	layout := app.defaultRuntimeLayout(60, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)
	app.drawStatusWindow(&layout)

	statusY := layout.Status.Y
	first := frameRowText(app.frame, statusY)
	second := frameRowText(app.frame, statusY+1)
	if !strings.Contains(first, "/work/librecode • session-123") {
		t.Fatalf("status first row = %q", first)
	}
	if !strings.Contains(second, modelLabel(app.currentProvider(), app.currentModel())) {
		t.Fatalf("status second row = %q", second)
	}
}

func TestRenderParityToolBlockFrame(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	event := newTestToolEvent("read", "func main() {\n    fmt.Println(\"hi\")\n}")
	lines := app.renderToolMessage(50, newChatMessage(database.RoleToolResult, formatToolEventForUI(event)))
	app.frame = newCellBuffer(50, len(lines), tcell.StyleDefault)
	for row, line := range lines {
		app.writeStyledLine(row, 50, line)
	}

	frame := frameText(app.frame)
	assertFrameContainsAll(t, frame,
		"✓ read",
		"output:",
		"func main() {",
		"      fmt.Println",
	)
}

func TestRenderParitySkillLoadBlockFrame(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	event := newTestToolEvent("load skill: golang-testing", "# golang-testing\nUse table-driven tests.")
	lines := app.renderToolMessage(60, newChatMessage(database.RoleToolResult, formatToolEventForUI(event)))
	app.frame = newCellBuffer(60, len(lines), tcell.StyleDefault)
	for row, line := range lines {
		app.writeStyledLine(row, 60, line)
	}

	frame := frameText(app.frame)
	assertFrameContainsAll(t, frame,
		"loaded skill golang-testing",
		"# golang-testing",
		"Use table-driven tests.",
	)
}

func TestRenderParityThinkingBlockStyle(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderThinkingMessage(50, newChatMessage(database.RoleThinking, "reasoning\ncontinues"))
	app.frame = newCellBuffer(50, len(lines), tcell.StyleDefault)
	for row, line := range lines {
		app.writeStyledLine(row, 50, line)
	}

	thinkingRow := lineIndexContaining(lines, settingThinking)
	contentRow := lineIndexContaining(lines, "reasoning")
	if thinkingRow == -1 || contentRow == -1 {
		t.Fatalf("thinking lines missing: %#v", lineTexts(lines))
	}
	for _, row := range []int{thinkingRow, contentRow} {
		cell := firstNonSpaceCell(app.frame, row)
		if got, want := cell.Style.GetForeground(), app.theme.colors[colorDim]; got != want {
			t.Fatalf("thinking row %d foreground = %v, want %v", row, got, want)
		}
		if !cell.Style.HasItalic() {
			t.Fatalf("thinking row %d should be italic", row)
		}
	}
}

func TestRenderParityWrappedBulletsFrame(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	lines := app.renderMarkdown("- alpha beta gamma delta epsilon zeta eta theta", 18)
	app.frame = newCellBuffer(18, len(lines), tcell.StyleDefault)
	for row, line := range lines {
		app.writeStyledLine(row, 18, line)
	}

	frame := frameText(app.frame)
	if got := strings.Count(frame, markdownBullet); got != 1 {
		t.Fatalf("bullet count = %d, want 1; frame = %q", got, frame)
	}
}

func TestRenderParityResumedHistoryViewport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtime, sessionID := newRenderParityRuntimeWithSession(ctx, t)
	app := newApp(nil, &RunOptions{
		Extensions: nil,
		Resources:  nil,
		Runtime:    runtime,
		Settings:   nil,
		Models:     nil,
		Auth:       nil,
		Config:     renderParityConfig(),
		CWD:        "/work",
		SessionID:  sessionID,
	})
	app.resetMessages()
	requireNoError(t, app.loadInitialMessages(ctx))

	bottom := app.messageLines(60, 8)
	if lineIndexContaining(bottom, "assistant history") == -1 {
		t.Fatalf("expected latest assistant history, got %#v", lineTexts(bottom))
	}

	app.warmMessageLineCache()
	app.scrollTranscript(100)
	scrolled := app.messageLines(60, 8)
	if lineIndexContaining(scrolled, "oldest user history") == -1 {
		t.Fatalf("expected oldest resumed history after scroll, got %#v", lineTexts(scrolled))
	}
}

func renderParityConfig() *config.Config {
	return &config.Config{
		Assistant: config.AssistantConfig{
			Provider:      "openai-codex",
			Model:         "gpt-5.5",
			ThinkingLevel: "off",
			Retry: config.RetryConfig{
				BaseDelay:   0,
				MaxDelay:    0,
				MaxAttempts: 0,
				Enabled:     false,
			},
		},
		App: config.AppConfig{
			Name:          "librecode",
			Env:           "test",
			WorkingLoader: config.LoaderUI{Text: "Shenaniganing..."},
		},
		Logging:    config.LoggingConfig{Level: "info", Format: "pretty"},
		Extensions: config.ExtensionsConfig{Paths: nil, Enabled: false},
		KSQL: config.KSQLConfig{
			Endpoint: "",
			Timeout:  0,
		},
		Database: config.DatabaseConfig{
			Path:            "",
			ApplyMigrations: false,
			MaxOpenConns:    0,
			MaxIdleConns:    0,
			ConnMaxLifetime: 0,
		},
		Cache: config.CacheConfig{
			Enabled:  false,
			Capacity: 0,
			TTL:      0,
		},
	}
}

func newRenderParityRuntimeWithSession(
	ctx context.Context,
	t *testing.T,
) (runtime *assistant.Runtime, sessionID string) {
	t.Helper()

	connection, err := sql.Open("sqlite", ":memory:")
	requireNoError(t, err)
	t.Cleanup(func() { requireNoError(t, connection.Close()) })
	requireNoError(t, database.Migrate(ctx, connection))
	repository := database.NewSessionRepository(connection)
	session, err := repository.CreateSession(ctx, "/work", "render parity", "")
	requireNoError(t, err)

	first := appendRenderParityMessage(ctx, t, repository, session.ID, nil, database.RoleUser, "oldest user history")
	second := appendRenderParityMessage(
		ctx,
		t,
		repository,
		session.ID,
		&first.ID,
		database.RoleThinking,
		"thinking history",
	)
	appendRenderParityMessage(ctx, t, repository, session.ID, &second.ID, database.RoleAssistant, "assistant history")

	runtime = assistant.NewRuntime(
		renderParityConfig(),
		repository,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	return runtime, session.ID
}

func appendRenderParityMessage(
	ctx context.Context,
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	t.Helper()

	entry, err := repository.AppendMessage(ctx, sessionID, parentID, &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	})
	requireNoError(t, err)

	return entry
}

func frameRowText(frame *cellBuffer, row int) string {
	if frame == nil || row < 0 || row >= frame.height {
		return ""
	}
	var builder strings.Builder
	for column := range frame.width {
		builder.WriteRune(frame.cell(column, row).Rune)
	}

	return builder.String()
}

func firstNonSpaceCell(frame *cellBuffer, row int) screenCell {
	if frame == nil || row < 0 || row >= frame.height {
		return emptyScreenCell()
	}
	for column := range frame.width {
		cell := frame.cell(column, row)
		if cell.Rune != ' ' {
			return cell
		}
	}

	return emptyScreenCell()
}

func emptyScreenCell() screenCell {
	return screenCell{Style: tcell.StyleDefault, Rune: 0}
}

func assertFrameContainsAll(t *testing.T, frame string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(frame, value) {
			t.Fatalf("frame missing %q: %q", value, frame)
		}
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
