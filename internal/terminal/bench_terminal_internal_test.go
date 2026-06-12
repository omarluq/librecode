package terminal

import (
	"fmt"
	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/transcript"
)

func BenchmarkDrawMessagesSameWidth(b *testing.B) {
	app := newApp(nil, &RunOptions{
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
	app.resetMessages()
	for i := range 200 {
		app.addMessage(transcript.RoleUser, fmt.Sprintf("message %d %s", i, strings.Repeat("hello world ", 20)))
		app.addMessage(
			transcript.RoleAssistant,
			fmt.Sprintf("answer %d %s", i, strings.Repeat("lorem ipsum dolor sit amet ", 30)),
		)
	}
	app.frame = rendertext.NewBuffer(120, 50, tcell.StyleDefault)
	app.drawMessages(120, 50, 0)

	b.ReportAllocs()
	for b.Loop() {
		app.frame = rendertext.NewBuffer(120, 50, tcell.StyleDefault)
		app.drawMessages(120, 50, 0)
	}
}
