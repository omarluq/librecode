package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaSchemaRawHandlesNilTable(t *testing.T) {
	t.Parallel()

	rawSchema, err := luaSchemaRaw(nil)
	require.NoError(t, err)
	assert.Nil(t, rawSchema)
}

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

func TestLuaSchemaJSONEncodesScalarsAndNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value lua.LValue
		name  string
		want  string
	}{
		{name: "true", value: lua.LTrue, want: `true`},
		{name: "false", value: lua.LFalse, want: `false`},
		{name: "number", value: lua.LNumber(1.25), want: `1.25`},
		{name: "string", value: lua.LString(`a "quote"`), want: `"a \"quote\""`},
		{name: "nil", value: lua.LNil, want: `null`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := luaSchemaJSON(testCase.value)
			require.NoError(t, err)
			assert.JSONEq(t, testCase.want, string(encoded))
		})
	}
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

func TestLuaSchemaRawPreservesNonArrayNumericKeysAsObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		script string
		name   string
		want   string
	}{
		{
			name: "fractional key",
			script: `
schema = { "first", "second" }
schema[3.5] = "fractional"
`,
			want: `{"1":"first","2":"second","3.5":"fractional"}`,
		},
		{
			name: "sparse key",
			script: `
schema = {}
schema[2] = "second"
`,
			want: `{"2":"second"}`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			state := lua.NewState()
			t.Cleanup(state.Close)

			require.NoError(t, state.DoString(testCase.script))
			table, ok := state.GetGlobal("schema").(*lua.LTable)
			require.True(t, ok)

			rawSchema, err := luaSchemaRaw(table)
			require.NoError(t, err)
			assert.JSONEq(t, testCase.want, string(rawSchema))
		})
	}
}
