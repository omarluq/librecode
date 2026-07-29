package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/transcript"
	"github.com/omarluq/librecode/internal/tui"
)

const benchmarkDisplayedSession = "displayed-session"

func BenchmarkWithSessionViewMissing(b *testing.B) {
	app := newApp(nil, &RunOptions{
		Extensions: nil, Resources: nil, Runtime: nil, Workflows: nil, Settings: nil,
		Models: nil, Auth: nil, Config: nil, CWD: "", SessionID: "",
	})
	app.sessionID = benchmarkDisplayedSession
	app.composerBuffer.Metadata = map[string]any{"source": "benchmark"}
	app.composerBuffer.Chars = []string{"a", "b", "c"}
	app.scopedEnabled = map[string]bool{"benchmark-tool": true}
	app.scopedOrder = []string{"benchmark-tool"}

	b.ReportAllocs()

	for b.Loop() {
		app.withSessionView("missing-session", func() {})
	}
}

func BenchmarkDrawMessagesSameWidth(b *testing.B) {
	app := newApp(nil, &RunOptions{
		Extensions: nil,
		Resources:  nil,
		Runtime:    nil,
		Workflows:  nil,
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

	app.frame = tui.NewCellBuffer(120, 50, tcell.StyleDefault)
	app.drawMessages(120, 50, 0)

	b.ReportAllocs()

	for b.Loop() {
		app.frame = tui.NewCellBuffer(120, 50, tcell.StyleDefault)
		app.drawMessages(120, 50, 0)
	}
}
