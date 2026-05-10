//nolint:testpackage // Benchmarks exercise unexported terminal rendering helpers.
package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
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
		app.addMessage(database.RoleUser, fmt.Sprintf("message %d %s", i, strings.Repeat("hello world ", 20)))
		app.addMessage(
			database.RoleAssistant,
			fmt.Sprintf("answer %d %s", i, strings.Repeat("lorem ipsum dolor sit amet ", 30)),
		)
	}
	app.frame = newCellBuffer(120, 50, tcell.StyleDefault)
	app.drawMessages(120, 50, 0)

	b.ReportAllocs()
	for b.Loop() {
		app.frame = newCellBuffer(120, 50, tcell.StyleDefault)
		app.drawMessages(120, 50, 0)
	}
}
