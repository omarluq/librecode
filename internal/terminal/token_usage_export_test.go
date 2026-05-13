package terminal

import (
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func MergeTerminalUsageForTest(current, next model.TokenUsage) model.TokenUsage {
	return mergeTerminalUsage(current, next)
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
