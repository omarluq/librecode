package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaToolResultFromScalarUsesStringContent(t *testing.T) {
	t.Parallel()

	result := luaToolResult(lua.LString("ok"))

	assert.Equal(t, "ok", result.Content)
	assert.Empty(t, result.Details)
}
