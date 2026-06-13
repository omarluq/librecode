package tui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/tui"
)

func TestTextEdgeCases(t *testing.T) {
	t.Parallel()

	assert.Empty(t, tui.Truncate("hello", 0))
	assert.Equal(t, "hello", tui.Truncate("hello", 10))
	assert.Empty(t, tui.Fit("hello", 0))
	assert.Equal(t, "hello  ", tui.PadRight("hello", 7))
	assert.Equal(t, " ", tui.PadRight("語", 1))

	segments := tui.Segments("alpha beta")
	breakIndex := tui.WrapBreakIndex(segments, 20)
	assert.Equal(t, len(segments), breakIndex)
	assert.Equal(t, []string{"alph", "a"}, tui.Wrap("alpha", 4))
	assert.Equal(t, []string{"alpha", testBeta}, tui.Wrap("alpha  beta", 6))
}

func TestTextWidthFitTruncateAndPad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		fit       string
		truncate  string
		padRight  string
		width     int
		textWidth int
	}{
		{
			name:      "ascii",
			text:      "hello",
			fit:       "hell",
			truncate:  "hel…",
			padRight:  "hell",
			width:     4,
			textWidth: 5,
		},
		{
			name:      "wide rune",
			text:      "語x",
			fit:       "語",
			truncate:  "…",
			padRight:  "語",
			width:     2,
			textWidth: 3,
		},
		{
			name:      "combining mark",
			text:      "e\u0301x",
			fit:       "e\u0301",
			truncate:  "…",
			padRight:  "e\u0301",
			width:     1,
			textWidth: 2,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.textWidth, tui.Width(testCase.text))
			assert.Equal(t, testCase.fit, tui.Fit(testCase.text, testCase.width))
			assert.Equal(t, testCase.truncate, tui.Truncate(testCase.text, testCase.width))
			assert.Equal(t, testCase.padRight, tui.PadRight(testCase.text, testCase.width))
		})
	}
}

func TestWrapModes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"alpha", testBeta}, tui.Wrap("alpha beta", 6))
	assert.Equal(t, []string{"alpha ", testBeta}, tui.WrapPreserveWhitespace("alpha beta", 6))
	assert.Equal(t, []string{""}, tui.Wrap("anything", 0))
	assert.Equal(t, []string{"one", "two"}, tui.Wrap("one\ntwo", 10))
}
