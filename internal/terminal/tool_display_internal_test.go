package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/assistant"
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
			name: "find",
			tool: "find",
			args: `{"pattern":"**/*.go","path":"internal/terminal"}`,
			want: "find **/*.go under internal/terminal",
		},
		{name: "ls default", tool: "ls", args: `{}`, want: "ls ."},
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
	assert.Equal(t, "tool", toolSummary("", "", nil))
}

func TestToolDisplayFromCallUsesStructuredArguments(t *testing.T) {
	t.Parallel()

	display := toolDisplayFromCall(assistant.ToolCallEvent{
		Arguments:     map[string]any{"command": "go test ./..."},
		ID:            "call_1",
		Name:          testToolBash,
		ArgumentsJSON: `{"command":"stale"}`,
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

func TestRenderExpandedToolDisplayPrettyPrintsArguments(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.toolsExpanded = true
	display := testToolDisplay("$ go test", toolDisplaySuccess)
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
