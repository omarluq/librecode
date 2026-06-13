package tui_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRectHelpers(t *testing.T) {
	t.Parallel()

	require.True(t, testRect(0, 0, 0, 1).Empty())
	require.False(t, testRect(0, 0, 1, 1).Empty())
	require.Equal(t, testRect(3, 4, 6, 2), testRect(1, 2, 10, 6).Inner(2))
	require.Equal(t, testRect(1, 2, 10, 6), testRect(1, 2, 10, 6).Inner(0))
}
