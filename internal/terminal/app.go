// Package terminal implements an interactive chat interface.
package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/omarluq/librecode/internal/assistant"
)

type chatMessage struct {
	role    string
	content string
}

// App is the terminal chat UI.
type App struct {
	screen    tcell.Screen
	runtime   *assistant.Runtime
	cwd       string
	sessionID string
	messages  []chatMessage
	input     []rune
}

// Run starts an interactive tcell chat loop.
func Run(ctx context.Context, runtime *assistant.Runtime, cwd, sessionID string) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("tui: create screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("tui: init screen: %w", err)
	}
	defer screen.Fini()

	app := &App{
		screen:    screen,
		runtime:   runtime,
		cwd:       cwd,
		sessionID: sessionID,
		messages:  []chatMessage{{role: "system", content: "librecode-go chat. Enter /quit to exit."}},
		input:     []rune{},
	}
	app.loop(ctx)

	return nil
}

func (app *App) loop(ctx context.Context) {
	for {
		app.draw()
		event := app.screen.PollEvent()
		if event == nil {
			return
		}

		shouldQuit, err := app.handleEvent(ctx, event)
		if err != nil {
			app.messages = append(app.messages, chatMessage{role: "error", content: err.Error()})
		}
		if shouldQuit {
			return
		}
	}
}

func (app *App) handleEvent(ctx context.Context, event tcell.Event) (bool, error) {
	switch typedEvent := event.(type) {
	case *tcell.EventResize:
		app.screen.Sync()
		return false, nil
	case *tcell.EventKey:
		return app.handleKey(ctx, typedEvent)
	default:
		return false, nil
	}
}

func (app *App) handleKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	key := event.Key()
	if key == tcell.KeyCtrlC || key == tcell.KeyEscape {
		return true, nil
	}
	if key == tcell.KeyEnter {
		return app.submit(ctx)
	}
	if key == tcell.KeyBackspace || key == tcell.KeyBackspace2 {
		if len(app.input) > 0 {
			app.input = app.input[:len(app.input)-1]
		}
		return false, nil
	}
	if key == tcell.KeyRune {
		app.input = append(app.input, event.Rune())
		return false, nil
	}

	return false, nil
}

func (app *App) submit(ctx context.Context) (bool, error) {
	text := strings.TrimSpace(string(app.input))
	app.input = []rune{}
	if text == "" {
		return false, nil
	}
	if text == "/quit" {
		return true, nil
	}

	app.messages = append(app.messages, chatMessage{role: "user", content: text})
	response, err := app.runtime.Prompt(ctx, assistant.PromptRequest{
		SessionID: app.sessionID,
		CWD:       app.cwd,
		Text:      text,
		Name:      "",
	})
	if err != nil {
		return false, err
	}
	app.sessionID = response.SessionID
	app.messages = append(app.messages, chatMessage{role: "assistant", content: response.Text})

	return false, nil
}

func (app *App) draw() {
	app.screen.Clear()
	width, height := app.screen.Size()
	headerStyle := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorGreen)
	mutedStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	userStyle := tcell.StyleDefault.Foreground(tcell.ColorLightCyan)
	assistantStyle := tcell.StyleDefault.Foreground(tcell.ColorLightGreen)

	writeLine(app.screen, 0, 0, width, " librecode-go ", headerStyle)
	row := 2
	for _, message := range visibleMessages(app.messages, height-5) {
		style := styleForRole(message.role, userStyle, assistantStyle, mutedStyle)
		for _, line := range strings.Split(message.content, "\n") {
			if row >= height-2 {
				break
			}
			writeLine(app.screen, 0, row, width, message.role+": "+line, style)
			row++
		}
	}

	prompt := "> " + string(app.input)
	writeLine(app.screen, 0, height-1, width, prompt, tcell.StyleDefault)
	app.screen.ShowCursor(len([]rune(prompt)), height-1)
	app.screen.Show()
}

func visibleMessages(messages []chatMessage, maxRows int) []chatMessage {
	if maxRows < 1 || len(messages) <= maxRows {
		return messages
	}

	return messages[len(messages)-maxRows:]
}

func styleForRole(role string, userStyle, assistantStyle, mutedStyle tcell.Style) tcell.Style {
	switch role {
	case "user":
		return userStyle
	case "assistant":
		return assistantStyle
	default:
		return mutedStyle
	}
}

func writeLine(screen tcell.Screen, column, row, width int, text string, style tcell.Style) {
	line := []rune(text)
	if len(line) > width {
		line = line[:width]
	}

	for index, value := range line {
		screen.SetContent(column+index, row, value, nil, style)
	}
}
