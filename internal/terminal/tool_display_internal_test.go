package terminal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/testutil"
)

func TestToolSummaryHumanizesKnownTools(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tool string
		args string
		want string
	}{
		{
			name: testToolBash,
			tool: testToolBash,
			args: `{"command":"mise exec -- go test ./...","timeout":30}`,
			want: "$ mise exec -- go test ./... (timeout 30s)",
		},
		{
			name: "read range",
			tool: testToolRead,
			args: `{"path":"internal/terminal/tool_blocks.go","offset":10,"limit":5}`,
			want: "read internal/terminal/tool_blocks.go:10-14",
		},
		{
			name: "edit count",
			tool: testToolEdit,
			args: `{"path":"main.go","edits":[{},{}]}`,
			want: "edit main.go (2 edits)",
		},
		{name: testToolWrite, tool: testToolWrite, args: `{"path":"out.txt"}`, want: "write out.txt"},
		{
			name: "grep",
			tool: "grep",
			args: strings.Join([]string{
				`{"pattern":"StreamEventToolStart"`,
				`"path":"internal"`,
				`"glob":"**/*.go"`,
				`"literal":true`,
				`"ignore_case":true}`,
			}, ","),
			want: `grep "StreamEventToolStart" in internal (glob **/*.go, literal, ignore case)`,
		},
		{
			name: testToolFind,
			tool: testToolFind,
			args: `{"pattern":"**/*.go","path":"internal/terminal"}`,
			want: "find **/*.go under internal/terminal",
		},
		{name: "ls default", tool: "ls", args: `{}`, want: "ls ."},
		{
			name: "fetch default markdown",
			tool: testToolFetch,
			args: `{"url":"https://example.com/docs"}`,
			want: "fetch https://example.com/docs",
		},
		{
			name: "fetch explicit format",
			tool: testToolFetch,
			args: `{"url":"https://example.com/docs","format":"text"}`,
			want: "fetch text https://example.com/docs",
		},
		{
			name: "ast",
			tool: "ast",
			args: `{"mode":"query","path":"main.go","line":12,"depth":2,"query":"(function_declaration)"}`,
			want: `ast query main.go:12 depth 2 query "(function_declaration)"`,
		},
		{name: "skill", tool: "load skill: golang-testing", args: `{}`, want: "loaded skill golang-testing"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, toolSummary(testCase.tool, testCase.args, nil))
		})
	}
}

func TestToolSummaryFallsBackForUnknownAndBadJSON(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `custom {bad`, toolSummary("custom", `{bad`, nil))
	assert.Equal(t, `custom alpha="one" beta=2`, toolSummary("custom", `{"beta":2,"alpha":"one"}`, nil))
	assert.Equal(t, `custom array=["a"] bool=true nil=null`, toolSummary(
		"custom",
		`{"nil":null,"object":{"nested":true},"array":["a"],"bool":true}`,
		nil,
	))
	assert.Equal(t, "tool", toolSummary("", "", nil))
}

func TestToolSummaryArgumentTypeFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		args string
		want string
	}{
		{name: "find defaults path", tool: testToolFind, args: `{"pattern":"**/*.go"}`, want: "find **/*.go under ."},
		{name: "find missing pattern", tool: testToolFind, args: `{"path":"internal"}`, want: "find"},
		{
			name: "bash fractional timeout",
			tool: testToolBash,
			args: `{"command":"sleep","timeout":1.5}`,
			want: "$ sleep (timeout 1.5s)",
		},
		{name: "read offset", tool: testToolRead, args: `{"path":"file.go","offset":7}`, want: "read file.go:7"},
		{
			name: "read zero limit",
			tool: testToolRead,
			args: `{"path":"file.go","offset":7,"limit":0}`,
			want: "read file.go:7",
		},
		{name: "read invalid path", tool: testToolRead, args: `{"path":42}`, want: testToolRead},
		{
			name: "read invalid range",
			tool: testToolRead,
			args: `{"path":"file.go","offset":"7","limit":"2"}`,
			want: "read file.go",
		},
		{name: "bash invalid timeout", tool: testToolBash, args: `{"command":"echo","timeout":"soon"}`, want: "$ echo"},
		{name: "fetch missing url", tool: testToolFetch, args: `{"format":"text"}`, want: testToolFetch},
		{
			name: "fetch markdown omitted",
			tool: testToolFetch,
			args: `{"url":"https://example.com","format":"markdown"}`,
			want: "fetch https://example.com",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, toolSummary(testCase.tool, testCase.args, nil))
		})
	}
}

func TestToolDisplayAdditionalFallbackBranches(t *testing.T) {
	t.Parallel()

	errorDisplay := toolDisplayFromParsedEvent(&parsedToolEvent{
		Name:          testToolBash,
		ArgumentsJSON: `{"command":"false"}`,
		DetailsJSON:   "",
		Output:        "",
		Error:         "boom",
	})
	assert.Equal(t, toolDisplayError, errorDisplay.Status)
	assert.Equal(t, "$ false", errorDisplay.Title)

	assert.Equal(t, testToolBash, bashToolSummary(nil, testToolBash))
	assert.Equal(t, "grep", grepToolSummary(map[string]any{}, "grep"))
	assert.Equal(t, "ast", astToolSummary(map[string]any{"mode": "query"}, "ast"))
	assert.Equal(t, "ast outline main.go", astToolSummary(map[string]any{"path": "main.go"}, "ast"))
	assert.Contains(t, unknownToolSummary("custom", map[string]any{"nested": make(chan int)}, ""), "custom nested=")
	assert.Equal(t, "7.25", formatNumber(7.25))
	assert.False(t, boolArg(map[string]any{}, "literal"))
	assert.Zero(t, arrayLenArg(map[string]any{"items": "nope"}, "items"))
}

func TestToolSummaryNumericArgumentTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "$ sleep (timeout 2.5s)", bashSummaryForTest(float32(2.5)))
	assert.Equal(t, "$ sleep (timeout 2s)", bashSummaryForTest(2))
	assert.Equal(t, "$ sleep (timeout 3s)", bashSummaryForTest(int64(3)))
	assert.Equal(t, "$ sleep (timeout 4s)", bashSummaryForTest(int32(4)))
	assert.Equal(t, "$ sleep (timeout 5s)", bashSummaryForTest(json.Number("5")))
	assert.Equal(t, "$ sleep", bashSummaryForTest(json.Number("NaN-ish")))
}

func TestToolDisplayFromCallUsesStructuredArguments(t *testing.T) {
	t.Parallel()

	display := toolDisplayFromCall(&assistant.ToolCallEvent{
		Arguments:     testutil.ToolArguments(map[string]any{testToolCommandKey: "go test ./..."}),
		ID:            "call_1",
		ParentCallID:  "",
		Name:          testToolBash,
		ArgumentsJSON: `{"command":"stale"}`,
		Sequence:      0,
	})

	assert.Equal(t, "$ go test ./...", display.Title)
	assert.Equal(t, toolDisplayPending, display.Status)
}

func TestToolDisplayTitleUsesPendingMarkerOnly(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	pending := testToolDisplay("$ go test", toolDisplayPending)
	success := testToolDisplay("read file.go", toolDisplaySuccess)
	errorDisplay := testToolDisplay("$ false", toolDisplayError)

	assert.Equal(t, "◌ $ go test", app.toolDisplayTitle(&pending))
	assert.Equal(t, "read file.go", app.toolDisplayTitle(&success))
	assert.Equal(t, "$ false", app.toolDisplayTitle(&errorDisplay))
}

func TestRenderToolDisplayPadsLikeUserMessageBlocks(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	display := testToolDisplay("$ go test ./...", toolDisplaySuccess)
	display.Output = "ok"

	lines := app.renderToolDisplayBlock(24, &display)

	assert.Contains(t, lineTexts(lines), "  $ go test ./...       ")
	assert.Contains(t, lineTexts(lines), "  ok                    ")
}

func TestRenderToolDisplayShowsDiffOnlyWhenExpanded(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	display := testToolDisplay("edit main.go", toolDisplaySuccess)
	display.DetailsJSON = `{"diff":"--- before\n+++ after\n@@ -1 +1 @@\n-old\n+new"}`

	collapsedLines := app.renderToolDisplayBlock(80, &display)
	assert.Equal(t, -1, lineIndexContaining(collapsedLines, "diff:"))
	assert.Equal(t, -1, lineIndexContaining(collapsedLines, "-old"))
	assert.Equal(t, -1, lineIndexContaining(collapsedLines, "+new"))

	app.toolsExpanded = true
	expandedLines := app.renderToolDisplayBlock(80, &display)
	assert.NotEqual(t, -1, lineIndexContaining(expandedLines, "diff:"))
	assert.NotEqual(t, -1, lineIndexContaining(expandedLines, "-old"))
	assert.NotEqual(t, -1, lineIndexContaining(expandedLines, "+new"))
}

func TestRenderEditToolDisplayFallsBackToArgumentDiffWhenExpanded(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	display := testToolDisplay("edit main.go", toolDisplaySuccess)
	display.Name = testToolEdit
	display.ArgumentsJSON = strings.Join([]string{
		`{"path":"main.go"`,
		`"edits":[{"old_text":"old\nline"`,
		`"new_text":"new\nline"}]}`,
	}, ",")

	lines := app.renderToolDisplayBlock(80, &display)

	assert.NotEqual(t, -1, lineIndexContaining(lines, "diff:"))
	assert.NotEqual(t, -1, lineIndexContaining(lines, "-old"))
	assert.NotEqual(t, -1, lineIndexContaining(lines, "+new"))
}

func TestTailExpandedToolLinesIncludesErrorWithOutput(t *testing.T) {
	t.Parallel()

	display := testToolDisplay("$ command", toolDisplayError)
	display.Output = "stdout"
	display.Error = "stderr"

	lines, hidden := tailExpandedToolLines(80, &display, tcell.StyleDefault, 10)

	assert.False(t, hidden)
	assert.NotEqual(t, -1, lineIndexContaining(lines, "output:"))
	assert.NotEqual(t, -1, lineIndexContaining(lines, "stderr"))
	assert.NotEqual(t, -1, lineIndexContaining(lines, "stdout"))
}

func TestRenderExpandedToolDisplayPrettyPrintsArguments(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	display := testToolDisplay("$ go test", toolDisplaySuccess)
	display.Name = testToolBash
	display.ArgumentsJSON = `{"command":"go test ./...","timeout":30}`

	lines := app.renderToolDisplayBlock(80, &display)

	assert.Equal(t, -1, lineIndexContaining(lines, `{"command":"go test ./...","timeout":30}`))
	assert.NotEqual(t, -1, lineIndexContaining(lines, `{`))
	assert.NotEqual(t, -1, lineIndexContaining(lines, `"command": "go test ./...",`))
	assert.NotEqual(t, -1, lineIndexContaining(lines, `"timeout": 30`))
	assert.NotEqual(t, -1, lineIndexContaining(lines, `}`))
}

func TestPrettyJSONFallsBackToTrimmedInput(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `{bad`, prettyJSON("\n {bad \n"))
}

func TestRenderToolDisplayWrapsLongTitleWithinPadding(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	display := testToolDisplay("$ "+strings.Repeat("word ", 8), toolDisplayPending)

	lines := app.renderToolDisplayBlock(18, &display)
	texts := lineTexts(lines)

	assert.Contains(t, texts, "  ◌ $ word word   ")
	assert.Contains(t, texts, "  word word       ")
}

func TestHiddenToolLinesTextIncludesExpandHint(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "… 1 earlier output line hidden  ctrl+o expand", hiddenToolLinesText(1, "ctrl+o"))
	assert.Equal(t, "… 2 earlier output lines hidden", hiddenToolLinesText(2, ""))
}

func testToolDisplay(title string, status toolDisplayStatus) toolDisplay {
	return toolDisplay{
		Title:         title,
		Name:          "",
		ArgumentsJSON: "",
		DetailsJSON:   "",
		Output:        "",
		Error:         "",
		Status:        status,
	}
}

func bashSummaryForTest(timeout any) string {
	return bashToolSummary(map[string]any{testToolCommandKey: "sleep", "timeout": timeout}, testToolBash)
}
