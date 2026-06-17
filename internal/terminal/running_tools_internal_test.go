package terminal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
)

func TestRunningToolBlocksAppendRenderAndRemove(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	call := testToolCallEvent(testToolBash, `{"command":"go test ./..."}`)
	call.Arguments = map[string]any{"command": "go test ./..."}

	app.applyStreamedToolStart(&call, "")

	require.Len(t, app.runningToolBlocks, 1)
	lines := app.renderRunningToolBlock(80, app.runningToolBlocks[0].Call)
	assert.NotEqual(t, -1, lineIndexContaining(lines, "◌ $ go test ./..."))

	app.applyStreamedToolEvent(&assistant.ToolEvent{
		Name:          testToolBash,
		ArgumentsJSON: `{"command":"go test ./..."}`,
		DetailsJSON:   "",
		Result:        "ok",
		Error:         "",
		IsError:       false,
	})

	assert.Empty(t, app.runningToolBlocks)
}

func TestRemoveRunningToolBlockFallsBackToName(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.runningToolBlocks = []runningToolBlock{
		testRunningToolBlock(testToolRead, `{"path":"a.go"}`),
		testRunningToolBlock(testToolBash, `{"command":"go test"}`),
	}

	app.removeRunningToolBlock(&assistant.ToolEvent{
		Name:          testToolBash,
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Result:        "",
		Error:         "",
		IsError:       false,
	})

	require.Len(t, app.runningToolBlocks, 1)
	assert.Equal(t, testToolRead, app.runningToolBlocks[0].Call.Name)
}

func TestResetStreamingBlocksClearsRunningTools(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.runningToolBlocks = []runningToolBlock{testRunningToolBlock(testToolBash, "")}

	app.resetStreamingBlocks()

	assert.Empty(t, app.runningToolBlocks)
}

func testRunningToolBlock(name, argumentsJSON string) runningToolBlock {
	return runningToolBlock{
		StartedAt: time.Time{},
		Call:      testToolCallEvent(name, argumentsJSON),
	}
}

func testToolCallEvent(name, argumentsJSON string) assistant.ToolCallEvent {
	return assistant.ToolCallEvent{
		Arguments:     nil,
		ID:            "",
		Name:          name,
		ArgumentsJSON: argumentsJSON,
	}
}
