package extension_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerEmit(t *testing.T) {
	t.Parallel()

	t.Run("sends payload to handlers", func(t *testing.T) {
		t.Parallel()

		manager := loadTestExtension(t, `
local lc = require("librecode")
local seen = ""
lc.on("custom", function(name, payload)
  seen = name .. ":" .. payload.message
end)
lc.register_command("seen", "Seen", function()
  return seen
end)
`)

		require.NoError(t, manager.Emit(context.Background(), "custom", map[string]any{"message": "hello"}))
		seen, err := manager.ExecuteCommand(context.Background(), "seen", "")

		require.NoError(t, err)
		assert.Equal(t, "custom:hello", seen)
	})

	t.Run("returns handler errors", func(t *testing.T) {
		t.Parallel()

		manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("custom", function()
  error("boom")
end)
`)

		err := manager.Emit(context.Background(), "custom", map[string]any{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), `event "custom" failed`)
	})

	t.Run("respects canceled context", func(t *testing.T) {
		t.Parallel()

		manager := loadTestExtension(t, `
local lc = require("librecode")
lc.on("custom", function() end)
`)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := manager.Emit(ctx, "custom", map[string]any{})

		require.ErrorIs(t, err, context.Canceled)
	})
}
