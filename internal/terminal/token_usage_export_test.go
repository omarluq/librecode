package terminal

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func MergeTerminalUsageForTest(current, next model.TokenUsage) model.TokenUsage {
	return mergeTerminalUsage(current, next)
}

func FormatContextUsageForTest(usage model.TokenUsage) string {
	return formatContextUsage(usage)
}

func FormatTokenStatusForTest(usage model.TokenUsage) string {
	return formatTokenStatus(usage)
}

func ContextBreakdownLinesForTest(breakdown map[string]int) []string {
	return contextBreakdownLines(breakdown)
}

func ContextContributorLinesForTest(contributors []model.TokenContributor) []string {
	return contextContributorLines(contributors)
}

func NewAppForTest() *App {
	return newApp(nil, &RunOptions{
		Extensions: nil,
		Resources:  nil,
		Runtime:    nil,
		Settings:   nil,
		Models:     nil,
		Auth:       nil,
		Config:     nil,
		CWD:        "",
		SessionID:  "",
	})
}

func (app *App) SetTokenUsageForTest(usage model.TokenUsage) {
	app.tokenUsage = usage
}

func (app *App) ApplyTokenUsageForTest(usage *model.TokenUsage) {
	app.applyTokenUsage(usage)
}

func (app *App) TokenStatusTextForTest() string {
	return app.tokenStatusText()
}

func (app *App) TokenUsageForTest() model.TokenUsage {
	return app.tokenUsage
}

func (app *App) ResetMessagesForTest() {
	app.resetMessages()
}

func (app *App) TruncateMessagesForTest(length int) {
	app.truncateMessages(length)
}

func (app *App) AddMessageForTest(role, content string) {
	app.addMessage(database.Role(role), content)
}

func (app *App) ShowContextInfoForTest(original string) error {
	return app.showContextInfo(context.Background(), original)
}

func (app *App) MessageContentsForTest() []string {
	contents := make([]string, 0, len(app.transcript.History))
	for _, message := range app.transcript.History {
		contents = append(contents, message.Content)
	}

	return contents
}
