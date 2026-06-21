package extension

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

const luaFieldWorkingForTest = "working"

func TestLuaToolResultFromScalarUsesStringContent(t *testing.T) {
	t.Parallel()

	result := luaToolResult(lua.LString("ok"))

	assert.Equal(t, "ok", result.Content)
	assert.Empty(t, result.Details)
}

func TestJSONRawToLuaValue(t *testing.T) {
	t.Parallel()

	tests := []struct { //nolint:govet // Table-driven tests prefer readable field order over fieldalignment.
		raw  json.RawMessage
		name string
		want lua.LValue
	}{
		{name: "valid string", raw: json.RawMessage(`"ok"`), want: lua.LString("ok")},
		{name: "invalid JSON", raw: json.RawMessage(`{`), want: lua.LNil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			state := lua.NewState()
			t.Cleanup(state.Close)

			got := jsonRawToLuaValue(state, testCase.raw)

			assert.Equal(t, testCase.want, got.value)
		})
	}
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

	assert.False(t, luaContextBool(map[string]any{luaFieldWorkingForTest: "y"}, luaFieldWorkingForTest))
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

func TestNewLuaValueCoversCollectionAndFallbackTypes(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	stringMap := newLuaValue(state, map[string]string{"one": "1"}).value
	stringMapTable, matched := stringMap.(*lua.LTable)
	require.True(t, matched)
	assert.Equal(t, "1", stringMapTable.RawGetString("one").String())

	stringSlice := newLuaValue(state, []string{"a", "b"}).value
	stringSliceTable, matched := stringSlice.(*lua.LTable)
	require.True(t, matched)
	assert.Equal(t, "b", stringSliceTable.RawGetInt(2).String())

	assert.Equal(t, lua.LString("7"), newLuaValue(state, uint(7)).value)

	int64Value, ok := scalarLuaValue(int64(9))
	require.True(t, ok)

	numberValue, matched := int64Value.value.(lua.LNumber)
	require.True(t, matched)
	assert.InDelta(t, 9, float64(numberValue), 0)
}

func TestLuaValueFallbackBranches(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	table := state.NewTable()
	state.SetField(table, "text", lua.LString("x"))
	assert.Equal(t, 3, firstLuaTableIntValue(table, "missing", 3))
	assert.Nil(t, luaTableFunction(table, "missing"))
	assert.False(t, luaContextBool(map[string]any{}, luaFieldWorkingForTest))
	assert.True(t, luaContextBool(map[string]any{luaFieldWorkingForTest: true}, luaFieldWorkingForTest))

	text, ok := luaBufferText(state.NewTable())
	assert.False(t, ok)
	assert.Empty(t, text)

	buffer := luaBufferState("empty", state.NewTable())
	assert.Empty(t, buffer.Text)
	assert.Equal(t, 0, buffer.Cursor)
}

func TestLuaUIDrawOpReadsSpans(t *testing.T) {
	t.Parallel()

	state := lua.NewState()
	t.Cleanup(state.Close)

	table := state.NewTable()
	spans := state.NewTable()
	span := state.NewTable()
	state.SetField(span, luaFieldText, lua.LString("hello"))
	state.RawSetInt(spans, 1, span)
	state.SetField(table, "spans", spans)

	op := luaUIDrawOp(table)

	require.NotNil(t, op)
	require.Len(t, op.Spans, 1)
	assert.Equal(t, "hello", op.Spans[0].Text)
}

func firstLuaTableIntValue(table *lua.LTable, key string, fallback int) int {
	value, _ := luaTableInt(table, key, fallback)

	return value
}
