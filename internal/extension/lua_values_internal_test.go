package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaToolResultFromScalarUsesStringContent(t *testing.T) {
	t.Parallel()

	result := luaToolResult(lua.LString("ok"))

	assert.Equal(t, "ok", result.Content)
	assert.Empty(t, result.Details)
}

func TestStringMapToLuaTable(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	table := stringMapToLuaTable(state, map[string]string{"a": "one", "b": "two"})

	assert.Equal(t, "one", table.RawGetString("a").String())
	assert.Equal(t, "two", table.RawGetString("b").String())
}

func TestLuaScalarAndContextFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input any
		want  lua.LValue
		name  string
	}{
		{name: "bool", input: true, want: lua.LBool(true)},
		{name: "number", input: float64(2.5), want: lua.LNumber(2.5)},
		{name: "string", input: "x", want: lua.LString("x")},
		{name: "nil", input: nil, want: lua.LNil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, ok := scalarLuaValue(testCase.input)

			require.True(t, ok)
			assert.Equal(t, testCase.want, value.value)
		})
	}

	assert.False(t, luaContextBool(map[string]any{"working": "yes"}, "working"))
}

func TestLuaBufferStateVariants(t *testing.T) {
	t.Parallel()

	t.Run("plain value", func(t *testing.T) {
		t.Parallel()

		buffer := luaBufferState("main", lua.LString("hello"))

		assert.Equal(t, "main", buffer.Name)
		assert.Equal(t, "hello", buffer.Text)
		assert.Equal(t, 5, buffer.Cursor)
	})

	t.Run("chars and blocks", func(t *testing.T) {
		t.Parallel()

		state := lua.NewState()
		t.Cleanup(state.Close)

		table := state.NewTable()
		chars := state.NewTable()
		state.RawSetInt(chars, 1, lua.LString("h"))
		state.RawSetInt(chars, 2, lua.LString("i"))
		state.SetField(table, "chars", chars)
		state.SetField(table, luaFieldName, lua.LString("buf"))
		state.SetField(table, "label", lua.LString("Buffer"))
		state.SetField(table, "cursor", lua.LNumber(1))

		blocks := state.NewTable()
		block := state.NewTable()
		state.SetField(block, luaFieldKind, lua.LString("text"))
		state.SetField(block, "role", lua.LString("assistant"))
		state.SetField(block, luaFieldText, lua.LString("hello"))
		state.SetField(block, "index", lua.LNumber(7))
		state.SetField(block, "streaming", lua.LBool(true))
		state.RawSetInt(blocks, 1, block)
		state.SetField(table, "blocks", blocks)

		buffer := luaBufferState("fallback", table)

		assert.Equal(t, "buf", buffer.Name)
		assert.Equal(t, "hi", buffer.Text)
		assert.Equal(t, 1, buffer.Cursor)
		assert.Equal(t, "Buffer", buffer.Label)
		require.Len(t, buffer.Blocks, 1)
		assert.Equal(t, "assistant", buffer.Blocks[0].Role)
		assert.True(t, buffer.Blocks[0].Streaming)
	})
}

func TestLuaWindowDrawCursorLayoutAndActionFallbacks(t *testing.T) {
	t.Parallel()

	window := luaWindowState("main", lua.LString("buffer-name"))
	assert.Equal(t, "buffer-name", window.Buffer)
	assert.True(t, window.Visible)

	assert.Nil(t, luaUIDrawOp(lua.LString("not-table")))
	assert.Nil(t, luaUICursor(lua.LString("not-table")))
	assert.Nil(t, luaLayoutState(lua.LString("not-table")))
	assert.Equal(t, ActionCall{Name: "do-it"}, luaActionCall(lua.LString("do-it")))
}

func TestLuaTableBoolValue(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)
	table := state.NewTable()
	state.SetField(table, "flag", lua.LBool(true))
	state.SetField(table, "text", lua.LString("yes"))

	tests := []struct {
		name      string
		field     string
		wantValue bool
		wantFound bool
	}{
		{name: "bool field", field: "flag", wantValue: true, wantFound: true},
		{name: "non-bool field", field: "text", wantValue: false, wantFound: false},
		{name: "missing field", field: "absent", wantValue: false, wantFound: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, found := luaTableBoolValue(table, testCase.field)

			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantValue, value)
		})
	}
}
