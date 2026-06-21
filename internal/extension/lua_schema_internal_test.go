package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaSchemaRawEncodesJSON(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	require.NoError(t, state.DoString(`
schema = {
  type = "object",
  properties = {
    foo = { type = "string" },
    count = { type = "number" },
    enabled = { type = "boolean" },
  },
  required = { "foo" },
}
`))

	table, ok := state.GetGlobal("schema").(*lua.LTable)
	require.True(t, ok)

	rawSchema, err := luaSchemaRaw(table)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "object",
		"properties": {
			"foo": { "type": "string" },
			"count": { "type": "number" },
			"enabled": { "type": "boolean" }
		},
		"required": ["foo"]
	}`, string(rawSchema))
}

func TestLuaSchemaRawRejectsNonFiniteNumbers(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	require.NoError(t, state.DoString(`schema = { invalid = 0 / 0 }`))
	table, ok := state.GetGlobal("schema").(*lua.LTable)
	require.True(t, ok)

	_, err := luaSchemaRaw(table)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-finite number")
}

func TestLuaSchemaRawRejectsCyclicTables(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	require.NoError(t, state.DoString(`
schema = { type = "object" }
schema.properties = { self = schema }
`))
	table, ok := state.GetGlobal("schema").(*lua.LTable)
	require.True(t, ok)

	_, err := luaSchemaRaw(table)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic table reference")
}

func TestLuaSchemaRawPreservesNonArrayNumericKeys(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	require.NoError(t, state.DoString(`
schema = { "first", "second" }
schema[3.5] = "fractional"
`))
	table, ok := state.GetGlobal("schema").(*lua.LTable)
	require.True(t, ok)

	rawSchema, err := luaSchemaRaw(table)
	require.NoError(t, err)
	assert.JSONEq(t, `{"1":"first","2":"second","3.5":"fractional"}`, string(rawSchema))
}
