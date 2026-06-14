package tui_test

import (
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
)

func TestVirtualList(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*testing.T){
		"visible item after clamped scroll offset": func(t *testing.T) {
			t.Helper()

			result := tui.VirtualList([]int{2, -1, 3, 1}, 3, 1)
			require.Equal(t, tui.VirtualListResult{
				Items:     []tui.VirtualListItem{{Index: 2, RowOffset: 0, Height: 3}},
				Total:     4,
				MaxOffset: 3,
				Offset:    1,
				Start:     2,
				End:       3,
			}, result)
		},
		"empty list": func(t *testing.T) {
			t.Helper()

			result := tui.VirtualList(nil, 3, 0)
			require.Equal(t, tui.VirtualListResult{
				Items:     []tui.VirtualListItem{},
				Total:     0,
				MaxOffset: 0,
				Offset:    0,
				Start:     0,
				End:       0,
			}, result)
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test(t)
		})
	}
}

func TestViewportSliceLines(t *testing.T) {
	t.Parallel()

	lines := []tui.Line{
		tui.NewLine(tcell.StyleDefault, "a"),
		tui.NewLine(tcell.StyleDefault, "b"),
		tui.NewLine(tcell.StyleDefault, "c"),
	}

	tests := map[string]struct {
		want   []tui.Line
		offset int
		height int
	}{
		"offset clamps to last full window": {
			offset: 99,
			height: 2,
			want:   []tui.Line{lines[1], lines[2]},
		},
		"zero height returns empty slice": {
			offset: 0,
			height: 0,
			want:   []tui.Line{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := (tui.Viewport{Offset: tt.offset}).SliceLines(lines, tt.height)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestSliceViewport(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		items  []int
		want   []int
		offset int
		height int
	}{
		"height larger than list returns full slice": {
			items:  []int{1, 2, 3},
			offset: 1,
			height: 9,
			want:   []int{1, 2, 3},
		},
		"zero height returns empty slice": {
			items:  []int{1, 2},
			offset: 0,
			height: 0,
			want:   []int{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tui.SliceViewport(tt.items, tt.offset, tt.height))
		})
	}
}
