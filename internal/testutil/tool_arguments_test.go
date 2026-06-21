package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/testutil"
)

func TestToolArgumentHelpers(t *testing.T) {
	t.Parallel()

	t.Run("map round trip", func(t *testing.T) {
		t.Parallel()

		arguments := testutil.ToolArguments(map[string]any{
			"path":  "README.md",
			"limit": float64(5),
		})

		fields := testutil.ToolArgumentFields(arguments)
		assert.Equal(t, "README.md", fields["path"])
		assert.InDelta(t, 5, fields["limit"], 0)
	})

	t.Run("JSON round trip", func(t *testing.T) {
		t.Parallel()

		arguments := testutil.ToolArgumentsJSON(`{"path":"README.md"}`)

		fields := testutil.ToolArgumentFields(arguments)
		assert.Equal(t, "README.md", fields["path"])
	})

	t.Run("panic on invalid map input", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() {
			testutil.ToolArguments(map[string]any{"bad": func() {}})
		})
	})
}
