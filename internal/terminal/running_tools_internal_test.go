package terminal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

type runningToolBlockTestCase struct {
	run  func(t *testing.T, app *App)
	name string
	want []string
}

func TestRunningToolBlocks(t *testing.T) {
	t.Parallel()

	for _, tt := range runningToolBlockTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runRunningToolBlockCase(t, tt)
		})
	}
}

func runningToolBlockTestCases() []runningToolBlockTestCase {
	return []runningToolBlockTestCase{
		runningToolAppendRenderRemoveCase(),
		runningToolFallbackNameCase(),
		runningToolRemoveByNameAndArgumentsCase(),
		runningToolRemoveByNameFallbackCase(),
		runningToolResetCase(),
	}
}

func runningToolAppendRenderRemoveCase() runningToolBlockTestCase {
	bashTestArguments := `{"command":"go test ./..."}`

	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			call := testToolCallEvent(testToolBash, bashTestArguments)
			call.Arguments = testutil.ToolArguments(map[string]any{testToolCommandKey: "go test ./..."})

			app.applyStreamedToolStart(&call, "")
			require.NotEmpty(t, app.runningToolBlocks)

			lines := app.renderRunningToolBlock(80, app.runningToolBlocks[0].Call)
			assert.NotEqual(t, -1, lineIndexContaining(lines, "◌ $ go test ./..."))

			app.applyStreamedToolEvent(&assistant.ToolEvent{
				Name:          testToolBash,
				ArgumentsJSON: bashTestArguments,
				DetailsJSON:   "",
				Result:        "ok",
				Error:         "",
				IsError:       false,
			})
		},
		name: "append render and remove completed tool",
		want: []string{},
	}
}

func runningToolFallbackNameCase() runningToolBlockTestCase {
	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			call := testToolCallEvent("", `{"command":"go test"}`)
			app.applyStreamedToolStart(&call, testToolBash)
		},
		name: "use fallback for blank streamed tool name",
		want: []string{testToolBash},
	}
}

func runningToolRemoveByNameAndArgumentsCase() runningToolBlockTestCase {
	sharedArguments := `{"path":"same.go"}`

	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			app.runningToolBlocks = []runningToolBlock{
				testRunningToolBlock(testToolRead, sharedArguments),
				testRunningToolBlock(testToolWrite, sharedArguments),
			}
			app.removeRunningToolBlock(&assistant.ToolEvent{
				Name:          testToolWrite,
				ArgumentsJSON: sharedArguments,
				DetailsJSON:   "",
				Result:        "",
				Error:         "",
				IsError:       false,
			})
		},
		name: "remove by matching name and arguments",
		want: []string{testToolRead},
	}
}

func runningToolRemoveByNameFallbackCase() runningToolBlockTestCase {
	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

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
		},
		name: "remove falls back to name when arguments are missing",
		want: []string{testToolRead},
	}
}

func runningToolResetCase() runningToolBlockTestCase {
	return runningToolBlockTestCase{
		run: func(t *testing.T, app *App) {
			t.Helper()

			app.runningToolBlocks = []runningToolBlock{testRunningToolBlock(testToolBash, "")}
			app.resetStreamingBlocks()
		},
		name: "reset clears running tools",
		want: []string{},
	}
}

func runRunningToolBlockCase(t *testing.T, testCase runningToolBlockTestCase) {
	t.Helper()

	app := newRenderTestApp(t)
	testCase.run(t, app)

	assert.Equal(t, testCase.want, runningToolBlockNames(app.runningToolBlocks))
}

func testRunningToolBlock(name, argumentsJSON string) runningToolBlock {
	return runningToolBlock{
		StartedAt: time.Time{},
		Call:      testToolCallEvent(name, argumentsJSON),
	}
}

func testToolCallEvent(name, argumentsJSON string) assistant.ToolCallEvent {
	return assistant.ToolCallEvent{
		Arguments:     tool.EmptyArguments(),
		ID:            "",
		Name:          name,
		ArgumentsJSON: argumentsJSON,
	}
}

func runningToolBlockNames(blocks []runningToolBlock) []string {
	names := make([]string, 0, len(blocks))
	for _, block := range blocks {
		names = append(names, block.Call.Name)
	}

	return names
}
